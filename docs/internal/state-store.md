# State store

Brain, the calendar and Telegram keep their durable state in SQLite
(`modernc.org/sqlite`, already a dependency of memory search). A commit writes
the rows it changed and nothing else; no state is rewritten as a whole file.

| Database | Holds |
| --- | --- |
| `~/.mewla/brain/state/brain.db` | lifecycle Works and Events, Brain presentation, the chat timeline |
| `~/.mewla/calendar/calendar.db` | calendar items and runs |
| `~/.mewla/telegram/state.db` | Telegram's durable state |

They are three files because three owners open them: the calendar and Telegram
do not know where Brain's state lives, and their schemas change on their own
schedules. They share one package, `daemon/sqlitedb`, for opening, transactions,
schema versions and migration.

## Rules every database follows

- **One daemon owns it.** The daemon holds the state-directory lock; WAL mode
  lets other processes read. Brain's presentation commit carries a generation
  number and fails (once, then resyncs) if another process changed the database
  since this one read it, instead of overwriting it.
- **Every commit is durable.** `synchronous=FULL`: when a command returns, its
  transaction is on disk, as the old `fsync` + rename was.
- **Rows are the same JSON the code already used.** Each row is the JSON of the
  struct the old file held an array of, so readers and writers did not change
  shape. After a commit the in-memory copy is the decode of what was stored, so
  a restart never changes a value's representation.
- **Schema changes are numbered steps** per component (`schema_version`
  table), each in one transaction. A database from a newer build is refused.

## Brain (`brain.db`)

| Table | Row | Key |
| --- | --- | --- |
| `lc_work` | a Work's reduced state, without its source index | `id` |
| `lc_event` | one lifecycle Event | `seq`; indexed `(work_id, kind, turn_token)`; unique `(work_id, source_id)` |
| `lc_meta` | `next_seq` | |
| `pres_work`, `pres_event`, `pres_turn`, `pres_admission` | the presentation rows | their ids |
| `pres_meta` | host foreground turn, `next_event_sequence`, commit `generation` | |
| `timeline` | one chat or Work-card row | `seq` (order), unique `id`, indexed by thread |
| `timeline_archive` | chat rows earlier repairs removed from the live timeline | `seq`, unique `(origin, id)` |

- A lifecycle command commits its Events, the Work row they reduce into and
  `next_seq` together. The Event log is never rewritten or compacted; lookups
  go through the indexes, so it can grow without slowing reads (it holds
  113 thousand Events on the author's machine and one query touches a handful).
- Exactly-once admission is a database constraint: `UNIQUE(work_id, source_id)`
  is what `State.SeenSources` always was (the set of a Work's Event source
  ids), so a duplicate source cannot be committed. The in-memory index is
  rebuilt from it at start.
- Lifecycle and presentation commit separately, as they always did: the
  lifecycle engine is the authority, the presentation rows are a projection,
  and startup repair rebuilds the projection from the engine.
- Works and presentation rows stay resident (about a thousand Works), so a read
  is a memory copy. The Event log and the timeline's history are read by index.
- The timeline appends with `INSERT` (the unique `id` replaces the old linear
  scan for duplicates). Rewriting it (moving a Work card, marking read) deletes,
  updates and inserts only the rows that differ.
- Memory search (`memory.sqlite`, derived) reads `timeline_archive` then
  `timeline` from a per-source position and rebuilds from the database when its
  own schema changes.

## Calendar (`calendar.db`)

`cal_item` holds an item without its runs; `cal_run` holds each run, keyed
`(item_id, seq)`. Claiming or finishing a run writes that run and the item, not
the run history (a recurring action's history was most of the old 2.8 MB file).

## Telegram (`state.db`)

`tg_core` holds the scalars; `tg_entry(field, key, body)` holds every map entry
and list element, found by reflection on `durableState`, so a new field is
stored without touching the database code. Map keys are the entry's key, list
keys the index. Telegram's polling projections are unbounded (about 22 thousand
entries on the author's machine); a mutation writes the entries it changes, so
the size no longer costs anything per update. Nothing is pruned: whether a
projection entry can be dropped is Telegram's decision, not the store's.

## Migrating from the JSON files

The first start of a daemon that finds the old files moves them in, once
(`sqlitedb.Migration`; Brain's part is `brain/legacy_import.go`):

1. Take an exclusive lock (`.migrate.lock`) on the directory.
2. Build the database under a temporary name (`brain.db.migrating`) and fill it
   from the decoded old files.
3. Re-read it and compare, per table, the row counts and a SHA-256 over every
   row's canonical JSON with the same figures computed from the old files. Any
   mismatch aborts; the old files are untouched and the temporary database is
   removed.
4. Write a `migration` row (what was checked, which files, where they went),
   checkpoint, and rename the database into place. This is the commit point.
5. Move the old files into one dated backup directory, keeping their relative
   paths, and write a tombstone.

Backups sit beside the migrated directory: `~/.mewla/brain/legacy-state-<UTC
time>/state/…`, `~/.mewla/calendar`'s sibling `~/.mewla/legacy-calendar-<UTC
time>/calendar/calendar.json…`, and `~/.mewla/telegram`'s sibling
`~/.mewla/legacy-telegram-<UTC time>/telegram/state.json…`. Brain's backup also
takes the repair copies the old store had accumulated (`*.pre-digest-repair-*`,
`*.schema1.bak`, `messages-<n>.jsonl`, crash-left `.tmp-*`). Nothing is deleted;
remove a backup yourself once you are satisfied. `mewla` never reads one.

A crash at any point reruns safely: before step 4 the next start rebuilds from
the untouched files; after it the database carries its migration record and the
next start only finishes step 5, skipping files already moved. A database next
to old files **without** a migration record is refused rather than guessed at.

Rows that do not map one to one are reported in the record, not dropped
silently: a repeated timeline id keeps its first row (the one every old reader
returned), and a torn final line of `messages.jsonl` (a crash mid-append) is
skipped. Any other malformed line fails the migration.

### Rolling back

The tombstone (`lifecycle/state.json` with schema 3, `calendar.json` with
`schema_version` 3, `state.json` with `schema` 99) makes an older binary stop
at start with an unsupported-schema error instead of opening an empty store.
To go back: stop the daemon, move the backup's files back to where they were,
delete the tombstone and the `.db` files, and start the older binary. Anything
written after the migration is in the database only and is lost by doing so.
