# The cat's house

Brain's cat has a small house. Tap the house mark at the end of the cat's row
on the Brain screen and you are there. It needs no setup and has no menus: you
tap things in the picture, and the one back button leaves.

The cat's life is Brain's real Work, nothing invented:

| Brain | The cat |
| --- | --- |
| Idle | At home. At night (8 pm to 6 am on the phone's clock) the lamp is on and it sleeps, and the bag still hangs on the wall |
| A Worker starts a Work | It leaves, and the bag is gone from the wall |
| The Work runs | It sends a postcard shortly after it leaves, and one more if the Work runs long |
| The Work is accepted | It comes home with a souvenir, and the shelf gains it |
| Something waits on you | A small sign hangs by the cat, or on its empty hook while it is away. Tap it to go back to Brain |

A Work that is cancelled brings the cat home with nothing.

## What you can tap

- **The mailbox** shows a new postcard and keeps it in the **album**; with none waiting it opens the album.
- **The shelf** opens every souvenir the cat can bring. Tap one to see the Work it came from.
- **The window** is the yard. A few treats grow each day: pick them up, and tap one in the tray to pack it in the bag before the cat leaves. The treat only changes the postcard's mood. Days you miss cost nothing.
- **The cat** answers a poke.

## Postcards

A postcard is a picture and one short line. The place comes from the Work's
title by fixed keywords (studio, harbour, library, workshop, garden,
observatory, market, kitchen, post office, station, night desk, and a trail
for anything else), so the choice is deterministic, works offline and cannot
leak: only the place leaves the title, never its words. At most one postcard
comes when a Work starts and one when it runs long (45 minutes), and at least
10 minutes pass between any two, so many Works starting at once never flood the
album. A Work that is already running when the daemon first learns of the cat
sends nothing: history is not backfilled.

The line is in the phone's language, chosen the way the rest of the App does:
the device locale, Chinese when it starts with `zh` and English otherwise. The
page's screen-reader names for the mailbox, shelf, window, bag, sign, cat and
back button follow it too. A new language is a new set of lines in
`daemon/catpark/web/lines.js`; a test fails if a name or a kind lacks a language.

The souvenir keeps the title of your own Work, on your own shelf only.

## Where it lives

The daemon keeps the state per computer (`catpark/state.json` in the state
directory) and derives it from Brain's snapshots and the Work record. Every
postcard and souvenir is keyed by Work and written to disk before it is
announced, so a restart repeats nothing. App and web UI show the same state
because both read it from the daemon over the connection they already have
(`cat_get`, `cat_state`, `cat_collect`, `cat_pack`, `cat_seen`).

The game is one web page the daemon carries (`/cat/`). The phone cannot open a
daemon URL in a WebView (its connection is a pinned tunnel on a port that
changes per launch), so the app downloads the page with signed requests into a
cache keyed by its version, checks each file against the manifest's digest, and
opens it from disk. The page holds no credentials and has no network access; it
can ask the app only for the few actions above. After the first visit it opens
offline with the last state it knew. The web UI frames the same page from
`/cat/`: the web UI's content security policy allows `frame-src 'self'`, and
only `/cat/` allows being framed, by the web UI itself.

The handwritten line uses a subset of LXGW WenKai (SIL OFL 1.1); its notice is in the
repository's third-party assets list.
