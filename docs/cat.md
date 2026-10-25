# The cat's house

Brain's cat has a small house. It is a tab of its own, **Cat**, beside Brain and
Sessions at the top of the App: tap it and you are there. It needs no setup and
has no menus: you tap things in the picture. Swipe to Brain or Sessions, or tap
their tabs, to leave.

The cat's life is Brain's real Work, nothing invented:

| Brain | The cat |
| --- | --- |
| Idle | At home. The light follows the phone's clock (dusk from about 6 pm, night by 8:30 pm, dawn from about 5:30 am). At night the lamp is on and it sleeps, and the bag still hangs on the wall |
| A Worker starts a Work | It leaves, and the bag is gone from the wall |
| The Work runs | It sends a postcard shortly after it leaves, and one more if the Work runs long |
| The Work is accepted | It comes home with a souvenir, and the shelf gains it |
| Something waits on you | A small sign hangs by the cat, or on its empty hook while it is away. Tap it to go back to Brain |

A Work that is cancelled brings the cat home with nothing.

## What you can tap

- **The mailbox** shows a new postcard and keeps it in the **album**; with none waiting it opens the album.
- **The shelf** opens every souvenir the cat can bring. Tap one to see the Work it came from.
- **The window** is the yard. A few treats grow each day: pick them up, and tap one in the tray to pack it in the bag before the cat leaves. The treat only changes the postcard's mood. Days you miss cost nothing.
- **The cat** answers a tap: a purr, a happy look and a little hop. Tap the little gift a visitor left to keep it.
- **The picture frame** on the wall opens the pictures: six ways to draw the same
  house (crayon, watercolour, gouache, paper cut-out, pixel, clay). Tap one and
  the whole room changes in place, so you compare it where you live. The choice
  is remembered per user on the daemon, so every device shows the same house.
  Gouache storybook is the default. Only gouache is the full living house
  described below; the other styles keep one picture per state, with the cat
  drawn into it, so a tap on the cat there is a small nudge of the room, and the
  album, postcards and souvenirs stay crayon.

## The living house (gouache)

The room is two empty pictures, day and night, that cross-fade with the phone's
clock, plus a dusk glow. The cat is drawn on its own, one round silhouette, and
only changes pose.

- **At home** it does one of eight things, picked by the time of day and chance, never the same one twice in a row: reads, plays with yarn, writes, has tea (steam rises), eats, naps on its bed; at night it sleeps on its cushion or watches the stars at the window. After a while it hops over to the next one and the pictures cross-fade; now and then it looks up.
- **Leaving** is a moment: the bag flies from the hook to the cat, which hops to the window and slips out. **Coming home** reverses it, the bag goes back on the hook, and the souvenir appears on the shelf after that.
- **Small life**: clouds, a butterfly, a lit lamp that flickers at night with crickets, and now and then a bird lands on the window sill and leaves a small gift. The gift is only for fun and is not saved.
- **Sound** is made in the page, with no audio files: a quiet room tone, paper for postcards, a purr. It starts with the first tap and stops whenever the tab is not on screen. There is no switch.
- Weather is not shown: the page has no network by design.

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
(`cat_get`, `cat_state`, `cat_collect`, `cat_pack`, `cat_seen`, `cat_style`).

The game is one web page the daemon carries (`/cat/`). The phone cannot open a
daemon URL in a WebView (its connection is a pinned tunnel on a port that
changes per launch), so the app downloads the page with signed requests into a
cache keyed by its version, checks each file against the manifest's digest, and
opens it from disk. The page holds no credentials and has no network access; it
can ask the app only for the few actions above. After the first visit it opens
offline with the last state it knew. The web UI frames the same page from
`/cat/`: the web UI's content security policy allows `frame-src 'self'`, and
only `/cat/` allows being framed, by the web UI itself.

The state also carries the chosen `style`; an unknown name is ignored and a
state saved before styles existed reads as gouache; a style someone already chose is kept. The pictures are generated
art kept in the page's `a/` folder, one folder per style under `a/s/`, and a
test fails if a style lacks its pictures, and for gouache every postcard, souvenir, treat and cat pose.

The handwritten line uses a subset of LXGW WenKai (SIL OFL 1.1); its notice is in the
repository's third-party assets list.
