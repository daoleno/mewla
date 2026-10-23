// Package memorysearch indexes Brain's visible chat (the timeline in
// brain.db and its archive) into a SQLite FTS5 file so Brain can look up what
// the user said or decided without guessing. The chat log stays the source of
// truth: the index is rebuilt from it whenever the schema or redaction rules
// change.
//
// Only visible text is indexed: the user's messages, Brain's replies and Work
// result cards. Tool output and thinking never enter the log, so they never
// enter the index. Text is redacted before it is stored.
package memorysearch

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// schemaVersion is stored in PRAGMA user_version. Bump it when the schema or
// Redact changes so the next open discards the index and rebuilds it.
const schemaVersion = 3

// refreshBatch is how many chat rows one Refresh transaction reads.
const refreshBatch = 500

// maxCandidates bounds the rows one search tier reads before deduplication.
const maxCandidates = 5000

var ErrNotFound = errors.New("message not found")

// Who labels of indexed messages.
const (
	WhoUser  = "user"
	WhoBrain = "brain"
	WhoWork  = "work"
	// WhoContext marks user-role rows Brain's Claude transcript fed back into
	// the chat that the user did not type: compaction summaries, forwarded
	// Mewla events and notifications. They are searchable but never "user".
	WhoContext = "context"
)

// ChatRow is one stored chat row: its position in its source and its JSON.
type ChatRow struct {
	Seq  int64
	Body []byte
}

// Source is where the chat lives. Sources are read in the order ChatSources
// names them (archives before the live timeline), each from a position that
// only grows: a row appended later has a larger Seq.
type Source interface {
	ChatSources() []string
	ChatRows(source string, after int64, limit int) ([]ChatRow, error)
}

// Index is one open index file. It is not safe for concurrent use; callers
// serialize Refresh and queries.
type Index struct {
	db *sql.DB
}

// Open opens (creating, mode 0600) the index at path. An index written by a
// different schemaVersion is deleted and recreated empty.
func Open(path string) (*Index, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	ix, err := openFile(path)
	if err != nil {
		return nil, err
	}
	var version int
	if err := ix.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		_ = ix.Close()
		return nil, err
	}
	if version != 0 && version != schemaVersion {
		_ = ix.Close()
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if err := os.Remove(path + suffix); err != nil && !os.IsNotExist(err) {
				return nil, err
			}
		}
		if ix, err = openFile(path); err != nil {
			return nil, err
		}
		version = 0
	}
	if version == 0 {
		if err := ix.createSchema(); err != nil {
			_ = ix.Close()
			return nil, err
		}
	}
	return ix, nil
}

func openFile(path string) (*Index, error) {
	// Create the file 0600 first: SQLite gives -wal and -shm the same mode.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	_ = file.Close()
	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Index{db: db}, nil
}

func (ix *Index) createSchema() error {
	stmts := []string{
		`CREATE TABLE cursors(
			source TEXT PRIMARY KEY, seq INTEGER NOT NULL)`,
		`CREATE TABLE msg(
			seq INTEGER PRIMARY KEY, id TEXT NOT NULL UNIQUE, at INTEGER NOT NULL,
			who TEXT NOT NULL, work_id TEXT NOT NULL DEFAULT '', body TEXT NOT NULL,
			dedupe TEXT NOT NULL, backfilled INTEGER NOT NULL DEFAULT 0)`,
		`CREATE INDEX msg_at ON msg(at, seq)`,
		`CREATE VIRTUAL TABLE msg_fts USING fts5(
			body, content='msg', content_rowid='seq', tokenize='trigram')`,
		fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion),
	}
	for _, stmt := range stmts {
		if _, err := ix.db.Exec(stmt); err != nil {
			return fmt.Errorf("create memory index: %w", err)
		}
	}
	return nil
}

func (ix *Index) Close() error {
	if ix == nil || ix.db == nil {
		return nil
	}
	return ix.db.Close()
}

// RefreshStats reports what one Refresh did.
type RefreshStats struct {
	Messages int
}

// Refresh reads whatever src gained since the last call. Archives are read
// before the live timeline; ids are unique, so a message that appears in both
// is stored once, and a row the source rewrote under a new position is read
// again and ignored.
func (ix *Index) Refresh(ctx context.Context, src Source) (RefreshStats, error) {
	var stats RefreshStats
	for _, name := range src.ChatSources() {
		for {
			added, more, err := ix.refreshSource(ctx, src, name)
			stats.Messages += added
			if err != nil {
				return stats, fmt.Errorf("index %s: %w", name, err)
			}
			if !more {
				break
			}
		}
	}
	return stats, nil
}

type timelineRow struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Kind      string `json:"kind"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	WorkID    string `json:"work_id"`
	// BrainAdmission marks rows written when the user sent a message through
	// Mewla, as opposed to rows backfilled from Brain's provider transcript.
	BrainAdmission bool `json:"brain_admission"`
}

// refreshSource indexes one batch of src's rows after the stored cursor and
// moves the cursor past it in the same transaction. more reports a full batch.
func (ix *Index) refreshSource(ctx context.Context, src Source, name string) (added int, more bool, err error) {
	var cursor int64
	switch err := ix.db.QueryRowContext(ctx, `SELECT seq FROM cursors WHERE source = ?`, name).Scan(&cursor); {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return 0, false, err
	}
	rows, err := src.ChatRows(name, cursor, refreshBatch)
	if err != nil || len(rows) == 0 {
		return 0, false, err
	}

	conn, err := ix.db.Conn(ctx)
	if err != nil {
		return 0, false, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return 0, false, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()

	for _, row := range rows {
		var item timelineRow
		if json.Unmarshal(row.Body, &item) != nil {
			continue
		}
		who, ok := whoOf(item)
		if !ok {
			continue
		}
		backfilled := isBackfilled(item)
		if backfilled && who == WhoUser && isContextBody(item.Body) {
			who = WhoContext
		}
		at, perr := time.Parse(time.RFC3339Nano, item.CreatedAt)
		body := strings.TrimSpace(Redact(item.Body))
		if perr != nil || body == "" || strings.TrimSpace(item.ID) == "" {
			continue
		}
		res, err := conn.ExecContext(ctx,
			`INSERT INTO msg(id, at, who, work_id, body, dedupe, backfilled) VALUES(?,?,?,?,?,?,?)
			 ON CONFLICT(id) DO NOTHING`,
			item.ID, at.UnixNano(), who, strings.TrimSpace(item.WorkID), body, dedupeKey(who, body), backfilled)
		if err != nil {
			return added, false, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		seq, err := res.LastInsertId()
		if err != nil {
			return added, false, err
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO msg_fts(rowid, body) VALUES(?,?)`, seq, body); err != nil {
			return added, false, err
		}
		added++
	}
	last := rows[len(rows)-1].Seq
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO cursors(source, seq) VALUES(?,?)
		 ON CONFLICT(source) DO UPDATE SET seq=excluded.seq`, name, last); err != nil {
		return added, false, err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return added, false, err
	}
	committed = true
	return added, len(rows) == refreshBatch, nil
}

// isBackfilled reports a user row Brain's provider transcript wrote into the
// chat (id "<session>:<turn>:user"), as opposed to one Mewla admitted when the
// user sent it.
func isBackfilled(item timelineRow) bool {
	return item.Role == "user" && !item.BrainAdmission && strings.HasSuffix(item.ID, ":user") && strings.Count(item.ID, ":") >= 2
}

var (
	pastedWrapper = regexp.MustCompile(`^<pasted_content[^>]*>\s*`)
	markupStart   = regexp.MustCompile(`^<[A-Za-z][\w:-]*[\s>]`)
)

// isContextBody reports text the user did not type: Claude's compaction
// summary, an interruption marker, or a message that is only markup (task
// notifications, forwarded Mewla events). A paste wrapper is looked through;
// the user's own paste (a log, a document) stays theirs. Typed text followed by
// markup does not start with it and also stays theirs.
func isContextBody(body string) bool {
	body = strings.TrimSpace(body)
	for _, prefix := range []string{"This session is being continued", "[Request interrupted", "Caveat:"} {
		if strings.HasPrefix(body, prefix) {
			return true
		}
	}
	if loc := pastedWrapper.FindStringIndex(body); loc != nil {
		body = body[loc[1]:]
	}
	return markupStart.MatchString(body)
}

func whoOf(item timelineRow) (string, bool) {
	switch strings.TrimSpace(item.Kind) {
	case "user_message":
		return WhoUser, true
	case "assistant_message":
		return WhoBrain, true
	case "work_card":
		return WhoWork, true
	case "":
		switch item.Role {
		case "user":
			return WhoUser, true
		case "assistant":
			return WhoBrain, true
		}
	}
	return "", false
}

// dedupeKey identifies a message by side and normalized text. User and context
// rows share a side: a backfilled copy of what the user typed collapses into
// the real message.
func dedupeKey(who, body string) string {
	if who == WhoContext {
		who = WhoUser
	}
	sum := sha256.Sum256([]byte(who + "\x00" + strings.Join(strings.Fields(body), " ")))
	return hex.EncodeToString(sum[:12])
}

// cursorHashes fingerprints the start of the file and the bytes just before
// off. A file replaced or rewritten in place almost surely changes one.
