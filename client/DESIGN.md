# Mewla design language (app)

The app follows the landing page (`site/index.html`, `site/styles.css`,
`site/pet.js`): warm paper, warm ink and one vermilion seal with Brain's pet
asleep in it. This file maps that language onto the app's theme system
(`app/theme/`, `app/constants/tokens.ts`). It does not define a second one.
The product is Mewla, and every brand asset is `assets/branding/mewla-*`.

## Principles

The chosen direction is **Seal & Slip (quiet)**: neutral paper and ink chrome,
Work arriving as plain slips, and the red cat in exactly one meaningful place.

1. **Paper and ink.** Content sits on warm white paper with warm-ink text. Use
   hairlines and tint for structure, not colour. Buttons, links, switches and
   selection are ink (`accent` is ink).
2. **Vermilion has three jobs only.** The seal (the cat and the logo), Send,
   and "Needs you" (its pill, its dot). It is never the chrome and never a
   failure. This is the YouTube/Netflix/Airbnb pattern; in Chinese reading a
   vermilion seal is a sign-off (盖章), so "Needs you" reads as "this needs
   your mark".
3. **One focal point per screen.** On Brain it is the slip that needs you.
4. **One cat.** The cat is Brain. It appears only where Brain is, at most once
   per screen, and every pose reflects a real Brain state. (The Brain icon's
   ink ears are a name, not a cat; see Iconography.)
5. **Every state has its own glyph** (`components/ui/StatusMark.tsx`), so the
   six Work states survive greyscale; colour only reinforces them.
6. **Quiet motion.** Motion eases out on the landing's curve, stops when the
   screen is hidden, and is off under reduced motion.

## Colour

All colours are `AppColors` fields built in `theme/primitives.ts`.
`theme/contrast.test.ts` enforces WCAG AA in both schemes (4.5:1 for text,
including every status word, 3:1 for affordances), OKLab ΔE > 0.06 between
the six Work states, and ΔE > 0.15 between the seal and failure.

| Landing token | Role | `AppColors` | Light | Dark |
| --- | --- | --- | --- | --- |
| `--bg` paper | canvas | `bgPrimary`, chat background | `#FBFAF7` | `#141210` (`--dark`) |
| `--card` | slips, cards, composer | `bgSurface`, `inputBackground` | `#FFFFFF` | `#201D19` |
| `--tint` | raised fill, quiet wells | `bgElevated`, `surfaceSubtle`, chat `surfaceMuted` | `#F5F3EE` | `#2C2823` |
| – | pressed well | `surfacePressed` | `#ECE8DF` | `#3A3530` |
| `--ink` | text, the chrome accent | `textPrimary`, `accent`, `accentStrong` | `#161412` | `#F4F0EA` |
| – | text on ink buttons | `textOnAccent` | `#FBFAF7` | `#141210` |
| – | selected row | `accentSoft`, `surfaceActive` | `#EFEBE3` | `#322D28` |
| `--soft` | secondary text, icons | `textSecondary` / `textTertiary` | `#57514A` / `#6C665D` | `#C9C1B6` / `#A9A196` |
| `--line` | hairline | `borderSubtle` / `border` | `#ECE7DE` / `#DDD6C9` | `#2A2622` / `#3F3933` |
| `--faint` | decoration only | `borderStrong` (3:1) | `#857E73` | `#8A8378` |
| `--red` | the seal | `seal` (fill) / `sealText` (words) / `onSeal` | `#C8372B` / `#BC3328` / white | `#D2412F` / `#FF9466` / white |
| `--red-soft` | seal wash | `sealSoft` | `#FBEBE6` | `#3A1F1B` |

`materials.tint` is a neutral ink wash (6% light, 8% dark), not a colour.
Focus rings and text selection use the Running blue.

**Chat.** Your messages sit on a quiet paper tint (`#F0ECE4`, dark
`#2E2A25`) in ink; Brain's replies sit on the page. Links are ink and
underlined. The conversation is capped at a reading width of 820 pt
(`ChatCanvas`).

Brain and a Session draw the same rows (`ui/chat`, soft-final SPEC §1.7):

- **Links** open: a web page in the browser, a file path in the file preview
  pushed over the chat (`ui/chat/links.ts`). A page without its own handler
  is Brain's, so its files are read through Brain's host Session; the Work
  file page uses the same function.
- **Code blocks** carry NSH's built-in Copy (`codeBlockActions`), which shows
  a check for a moment once copied.
- **Edited files** list each file once, from its project down, with +/− in
  `add` and `del` (a Codex patch's counts, or a Claude Edit's or Write's that
  the daemon reads from its recorded hunks); a rename shows and opens its new
  path, and a folded run lists its steps' files
  (`core/timeline/activityFiles.ts`).
- **Pictures**: sent ones at 168 × 126 above the bubble, a tool call's as
  thumbnails under its row (96 × 170 tall, 200 × 125 wide; radius 16 on the
  canvas, no fill or padding behind), a reply's at the
  text's width. A failed one keeps its frame with a refresh glyph and loads
  again on a tap. A tap opens the viewer (`PictureViewer`): black in both
  themes, ×, "2 of 3", previous and next, arrow keys and swipe, double tap
  and pinch to zoom, through every picture of the thread in order
  (`core/timeline/threadPictures.ts`).
- **Composer**: a pasted picture or file becomes a chip (`onFiles`); Esc
  leaves an open message edit on every hardware keyboard; text written into
  a draft from elsewhere (Ask Brain about this) joins what is typed, below
  it (`writeIntoDraft`).

**Work states.** Each state is a 13 pt glyph plus a word in soft type.

| State | Light | Dark | Glyph | Slip |
| --- | --- | --- | --- | --- |
| Ready | `#2F6B4F` | `#74C79B` | filled circle with a check | plain |
| Running | `#2C55C0` | `#9AB6FF` | spinning arc | plain |
| Needs you | seal | seal | the seal pill | ink outline, the cat perched on it |
| Warning | `#9A6212` | `#EDBA5A` | open triangle | plain |
| Failed | `#7D1F35` | `#F2A0B1` | crossed box, plus the cause | plain |
| Blocked | `#736C61` | `#9C9589` | dashed ring | dashed, unfilled |

`StatusPill` renders these marks: `success` Ready, `accent` Running, `warning`,
`danger` Failed, `blocked` the dashed ring, `needs` the seal pill, and
`neutral` a plain paper tag.
Brain lifecycles map onto the six states in `brainWorkLifecycleStatus`
(Reviewing runs, Waiting and Cancelled are inert, Needs review warns, Done
and Ready share the check).

Ink is the only chrome accent; there is no accent picker or stored accent
preference.

## Navigation

- **Phone.** The app bar has the menu (☰), the Brain · Sessions switch (text
  tabs with an ink underline; a seal dot on Brain when its Work needs you,
  on Sessions when a Session needs you) and one ⋮ page action. The menu
  slides over the page. Nothing else is pinned under the app bar: no status
  strip, no goal line, no connection banner.
- **Two panes from 1024 pt, or 760 on its side.** A window with at least 1024 pt
  of usable width, or 760 when it is wider than tall and at least 480 high (an
  open foldable held like a book: the iPhone Duo's inner screen on its side, a
  Android 7.6" Fold-in's), on any device, gets the wide layout (see The wide layout): the
  tray as a left pane beside the one stack. It follows the window's shape
  alone, never the device, and crossing the gate only adds or removes the
  pane: the page keeps its state.
- **Side insets.** Every page sits inside the left and right safe areas (a
  foldable's rail, a landscape notch): one frame around each screen in the root
  Stack (`HorizontalSafeAreaFrame`), and floating layers (toasts, sheets, the
  phone float) add the insets themselves.
- **Brain and Sessions have one home per layout.** On phone it is the app
  bar's switch, so the menu does not list them. On wide the left pane holds
  the Brain row and the tray's Sessions (see The wide layout). Never both on
  one screen.
- **Menu.** One ordered list without captions or cards: Calendar, Plugins,
  Skills, Stats, Resources. Settings sits apart at the
  bottom, and under it one quiet line names the current server and its
  state. Rows are a soft-ink glyph and a label, no tiles.
- **Connection state lives in the header** (SPEC §1.3): the cat and the state
  control on home (`core/tray/model.ts`), the pane's state line on wide. On
  every pushed page, while the computer is offline, connecting or must be
  paired again, Back carries the mark: an 8 pt dot at its top-right (grey; red
  for Pair again) and the label "Back · Offline" (`backMark`, `ui/shell/NavPage.tsx`;
  on iOS and Android the page's own Back stands in for the system's while the
  mark shows). No page, chat or list carries a connection banner. A page that
  can't work at all says so in its empty state.
- **The capsule header** (SPEC §11; `ui/shell/Capsule.tsx`, every value and the
  collapse rule in `core/shell/capsule.ts`). On a phone no page has a bar: one
  opaque shape floats 6 below the status bar and 8 from the sides, over a canvas
  strip. Brain: the cat, "Brain" (the computer's name under it with two or more),
  the state control without its pill, ⋯. A Session: Back, the agent's mark (a red
  dot while it needs you), the name, the view button, ⋯. Both collapse while the
  person reads (moved 24 from where the content last rested, away from the newest
  end) and expand at the newest end, on a tap, or when something needs the person;
  Brain's collapsed keeps the cat and a short state ("● 3"), a Session's Back, the
  mark and the name. The terminal starts collapsed. Every other pushed page
  (`NavPage`) has the compact capsule, as wide as what it holds and never a
  hollow middle: Back, the title once the page's own title has scrolled under
  it, then the page's actions right after; Back alone (a 44 circle) on a page
  with no actions while its title shows. It never collapses. Every capsule
  screen stands on the app's canvas (`CapsulePage`): the screen view behind a
  page is painted in the system's theme. Screen keeps its black system header,
  wide keeps its own header.
- **Home's tray** (SPEC §1.2, §3.2, §11; `ui/tray/Tray.tsx`, `core/tray/model.ts`)
  rises from the bottom, its top hanging just under home's header and filling
  to the screen's bottom whatever it lists; the header and the page dim under
  it. Its title row ends in the options glyph (Sort, Services), then +. "N
  running" counts the Running rows that are not quiet (`runningCount`): the
  state control, the tray's title and the cat read that one number, so quiet
  queued or waiting Work leaves "All quiet" and the napping cat. A row's line
  says what the thing does: never its section's word ("Working", "Idle",
  "Queued"), never the terminal's footer (`isTerminalFooter`), never a
  Session's "(mh-…)" id; with nothing reported, the agent and project
  ("Claude Code · mewla"), and a queued Work with no Worker "Not started
  yet". Live rows end in "● 12m". Work detail's Workers are the same rows
  (`workerRow`).
- **Failures show at the action.** Send stays pressable while offline: the
  composer says "Not sent: Mewla can't reach your computer. Your message is
  still here." and keeps the draft; the note clears on reconnect. Drafts also survive a reload
  and an app restart (`services/draftStore.ts`, 14 days, cleared on send). A sent
  message waits in the queue and goes out by itself; one that could not be
  delivered shows a "!" you tap to send it again.
  Other actions (New chat, a Work action) fail with a toast.
- Settings holds Servers, Channels (Telegram), Agents (Model Providers),
  Appearance and About. Browser stays hidden from the menu.
- **One home per destination.** Every screen you can go to (Calendar,
  Plugins, Skills, Stats, Resources, Settings) has exactly one entry point:
  its menu row. A ⋮ menu, a header title tap or a page button never repeats
  a menu destination. A ⋮ menu holds only actions on the current screen:
  Brain's is Work (phone only: the Work sheet, with the count line as its
  detail), New chat, Switch executor, Open terminal, Browse workspace; a
  Session's is New terminal, Rename, Model, Open Web, Open Brain (its linked
  Work), Terminate. Sessions' ⋮ holds New session and Services. Contextual
  recovery links ("Open Settings" in a no-server empty state) are fine: they
  fix the screen you are on rather than navigate elsewhere.
  So are links to one item, which is not the destination itself: a
  Calendar run's Work and Session name their schedule (calendar glyph,
  "运营：挖稿 · Oct 27") on the Work rail slip, the Work page, the Session
  row and the Session header, and tapping it (the header's title, on a
  Session) opens that item at that run, like Open Brain opens a Session's
  Work.
- **⋮ is a page's menu; ⋯ is an item's.** The header action of a page or
  sheet (Brain, Sessions, a Session, Screen, Browser, a Work, a diff) draws
  `more-vertical`. A menu on one thing in a list or under a reply (a message,
  a post, a provider row) draws `more-horizontal`.

### One control per concept

Screens compose the kit in `components/ui`; they do not restyle it, and a
second copy of a control is a bug. A small exclusive choice is
`SegmentedControl`; a filter with many, variable or multiple options is
`ChipGroup` (`multiple` for toggles, which also draw a check). Buttons are
`Button` (capsule) and `IconButton`; either shows its own busy state with
`loading`. A single-line text field is `MobileSingleLineInput`, with `framed`
for the field box. Status tags are `StatusPill`, centred waits are
`EmptyState busy`, in-line busy marks are `Spinner`, a switch whose change
needs the computer's answer is `PendingSwitch` (a spinner from the tap until
the answer, then the confirmed state), errors beside a form are
`InlineNotice`, bottom sheets are `BottomSheetFrame` and centred cards
`RisingSheet`, and an irreversible confirmation is `confirmDestructive`.
A multi-line field is `MultilineField`, a list row is `ListRow` in a
`ListSection`, and a failure with nothing to decide is a persistent error
`Toast` (stays until tapped or swiped; anything tied to a visible form or
sheet is an `InlineNotice` there, because a Toast sits under a Modal). Only a
decision is an `Alert`.

Terminal and Brain surfaces wear the terminal theme through the same
tokens: wrap them in `TerminalThemeScope chrome={…}` (`theme/provider.tsx`)
and the kit reads `TerminalThemeChrome` as `useAppColors()`. Never add a
palette prop to a component.

### Gestures

- **Peer pages swipe; filters tap.** Pages that are peers under one header
  sit in a native pager (react-native-tab-view on react-native-pager-view),
  and the indicator follows the finger through the swipe rather than jumping
  on release: Brain · Sessions (`app/(primary)/_layout.tsx`), Stats' ranges
  and Plugins · Skills on an agent's page (`SegmentedPager`, which feeds the
  pager position to `SegmentedControl`). A control that filters, picks or
  steps through one list stays tap-only, because a swipe on its content
  already belongs to that content: Friends' All · Following (it narrows one
  feed and looks like tabs), the diff scope, the provider agent selector,
  Appearance, and a Claude question's option stepper.
- **Menu.** On phone the menu opens from a rightward swipe anywhere on
  Brain, not only from the edge: with Android gesture navigation the left
  30 dp is system Back, so an edge-only drawer is unreachable there and the
  swipe leaves the app. On Sessions a rightward swipe pages back to Brain
  instead. An open menu closes on a swipe left anywhere or a scrim tap. The
  drag follows the finger on the UI thread; on release a fling (over
  500 pt/s) wins, otherwise the menu settles the way it was moving once the
  drag passes 60 pt, so a long drag is not undone by a slow drift. It claims
  a drag after 12 pt, within 45° of horizontal (`PrimaryDrawer.tsx`).
- **Nested horizontal scrollers win.** Tables and code blocks in a reply
  claim a horizontal drag at 6 pt, before the menu (12 pt) and the pager
  (Android's 16 dp paging slop), through
  `useNestedHorizontalScrollGesture`. A rightward drag on a table already
  scrolled to its start therefore does nothing rather than open the menu.
  The terminal owns all its touches.
- **Platforms.** The gestures are shared code. One piece is Android only:
  when the menu takes a drag, `modules/touch-cancel` sends the page below
  the cancel that gesture-handler's root does not, so selectable text does
  not long-press into a word selection. iOS recognizers cancel the touches
  they claim, so on iOS and web it is a no-op (not yet checked on an iOS
  device). The other differences are the system's: Android's Back gesture
  keeps the screen edge, and on iOS a pager that is not on its first page
  (Skills, a later Stats range) takes a rightward swipe as paging, so the
  stack's back swipe works from the first page. Claude Code and Codex with
  no Plugins open straight on Skills, so there a rightward swipe shows
  Plugins before it can go back. Web is outside this rule: the web
  turns Brain · Sessions swiping off, and Plugins · Skills stays a
  tap-only switch on web.

## The wide layout

From 1024 pt of usable width, or 760 on its side, the app has two panes
(soft-final SPEC §14). It is the same app, the same routes and the same stack
as on the phone; only the frame changes. `wide` (`ui/shell/layout.ts`, from
`isWide` in `core/web/places.ts`) is the one gate, whatever the device:
- the usable width (the window less what the system draws at its sides: the
  iPhone Duo's 84 pt side bar, a phone's cutout) is at least 1024, or
- it is at least 760, wider than the window is tall, and the window at least
  480 high.

So an open iPhone Duo on its side (867 usable) and the Android SDK's 7.6" Fold-in on its
side (841 x 674 dp, less a side cutout) have the pane; at Material's 840 that
fold would have missed it by its cutout. Upright, folded shut, or a
phone held sideways (under 480 high), they keep the phone layout, as Apple's
split view tiles only on its side. Touch (a tablet, an open foldable) and a
pointer with a keyboard (a browser, a desktop) get the same frame.

On an iPhone (the Duo opened) the right pane's pages keep their phone
headers: Brain's header loses its cat and state control to the pane, and a
pushed page keeps its capsule. A half-open fold is not yet followed: NSH 0.5.0
reports no fold, so nothing snaps to it.

1. **One tree.** `Shell` (`ui/shell/Shell.tsx`) is one row at every width: the
   left pane and its handle come and go beside the right pane, which holds the
   one stack and is never made again. Crossing the gate (a resized window, a
   rotated tablet, folding or unfolding) keeps the stack, the pages, their scroll and what
   is typed. Narrow to wide, an open tray becomes the left pane; wide to
   narrow, the pane goes and Brain's header gets back its cat and state
   control. Anything that differs by width changes a leaf or a style, never
   wraps a page in a conditional.
2. **The left pane is the tray, always open** (`ui/shell/LeftPane.tsx`). The
   pane header (68): the cat (the computer page), the computer's name and its
   state line ("Online", "Connecting…", "Offline · Relay unreachable", "Pair
   again", "Brain is starting", "Brain can't start"); "Search or run" with the
   palette's key cap; the Brain row (the last thing Brain said, a live dot
   while its turn runs); then the tray itself (`TrayBody`, the phone's overlay
   shows the same), its title carrying the counts ("12 running, 3 need you").
   The row whose page is in the right pane has the pressed well. The pure
   parts (state line, title, selected row, Brain's line) are
   `core/tray/pane.ts`.
3. **A left-pane row replaces the page above Brain.** The stack is never
   deeper than Brain › that page › what it pushed, and Back always leads to
   Brain. A link inside the right pane pushes, as on the phone. A request row
   whose card is in Brain's thread returns the right pane to Brain and scrolls
   to the card. The palette's picks follow the same rule.
4. **The handle** between the panes: drag (280–420), double-click or
   double-tap to reset to 320, ← and → in steps of 16 while it has the focus.
   The width is kept on the device (`mewla.wide.paneWidth`).
5. **Brain's header on wide** is "Brain" and ⋯ only: the cat, the computer's
   name and the state control live in the pane.
6. **Reading columns.** On wide a page's content sits in its column
   (`NavPage` `column`, `PAGE_COLUMN_WIDTH`): list pages 680 (`page`), threads
   and their composer 760 (`reading`), and the whole pane (`full`) for the
   terminal, a diff and Screen. A Session switches between `reading` (chat)
   and `full` (terminal) without making its content again. Off wide pages
   take the window, as on a phone.
7. **Sheets become dialogs; menus become popovers.** Both in the
   primitives. `AppSheet` (and so `kit/Sheet`) is a 480 pt dialog on wide,
   centred, radius 28, over the scrim, without the grabber; Esc and the scrim
   close it (iOS shows it as its own form sheet). With the software keyboard
   up (an open foldable, a tablet) the dialog sits in what the keyboard
   leaves (`dialogFrame` in `core/shell/sheet.ts`), and its body's room
   (`SheetRoom`) follows, so New session's Start stays reachable. An action menu is
   `showMenu` (`ui/kit/Menu.tsx`): the platform's action sheet off wide, a
   288 pt popover with 48 pt rows and radius 18 at its button on wide (the
   button hands it `createMenuAnchor().node()`); Esc and a press outside
   close it, and a row that cannot run now stays in place at 40 %. Brain's
   ⋯, a Session's ⋯, the tray's options glyph and Git ⋯ use it; Screen ⋯ is NSH's
   `Menu`, a popup at its button on a pointer device.
8. **List and detail side by side** when the right pane is 880 or wider
   (`listDetail`: a 1280 window with the default pane): Git review › diff,
   Git's files › file, Brain's workspace › file. Below that the item pushes,
   as on the phone. It follows the pane's width live, the handle included.
9. **What else moves.** The floating screen stays in the right pane's
   bottom-right corner (`leftInset`). A page's toast belongs to its page, so
   it is centred in the right pane. First run and pairing have no left pane
   (they are not the shell).
10. **Keys at any width** with a hardware keyboard (`ui/shell/desktop/`):
    Ctrl/⌘ K the palette (from a field too, not from a focused terminal), `?`
    the shortcuts sheet, `N` New session, `/` the message box, `G` then B · S
    · C · M · X · E · T · R · , for Brain · All Sessions · Calendar · the
    computer page · Screen · Extensions · Stats · Resources · Settings, and
    Esc closes the top dialog, popover or palette. The palette is a 620
    dialog 12 % from the top, opening with the focus in its field: Actions
    (New session, New chat, Open Brain's terminal, Browse workspace, Switch
    executor, Keyboard shortcuts), Go to, Work, Sessions. A focused terminal
    owns its keys; one on screen but not focused does not (parity #27).
11. **Pointer.** Rows and icon buttons show the pressed well on hover; a
    right-click on a row in All Sessions starts selecting (parity #28); what a
    key can reach has a 2 pt ring.
12. **URLs and titles (web).** Every place has a URL (parity #47); Back,
    Forward and reload return to it, and the tab reads "{page} · Mewla"
    (`core/web/places.ts`, `ui/shell/webLocation.ts`).

## Brain and its Work

Brain has one Work surface, built from the daemon's `current_work`, then its
`open_work` (unfinished Work outside the current window: no Worker yet, or
waiting on something not live) (`components/brain/brainWorkSurface.ts`, pure
and tested):

- **Wide (≥ 1024 pt).** A 360 pt Work column to the right of the conversation
  (`BrainWorkColumn`): "Work", the goal line when there is one, a count
  line, then slips grouped Needs you ·
  Running · Back · Waiting · Queued. Queued and Waiting slips carry no
  buttons: nothing there needs you. Empty is "No open Work", nothing more.
  The count line is the only count: the group
  captions are bare ("Running", never "Running · 6"). The cat sits on the
  first Needs-you slip there,
  and the conversation drops its between-turn tail row. While Brain's turn
  runs, the cat is in the Working row and the column has none.
- **Phone.** The chat gets the whole screen. The same grouped list is a
  sheet, opened from the hint beside the cat at the end of the conversation
  ("Waiting on 3 Workers", "Brought 2 things back", "2 need you") or from
  ⋮ → Work. The hint is a button, not a line of text: Work that needs you is
  the seal pill with a chevron (the screen's one focal point), counted Work a
  tinted paper pill with a chevron, and every other state ("All quiet") plain
  muted words. It is drawn 32 pt tall with a 46 pt press target. Touching it
  darkens it (a 22% shade on the seal, 12% on a tint) and squeezes it to 96%
  on the UI thread (a gesture's worklets that never activates, so the Pressable keeps the press), so it answers on the frame the
  finger lands even while the JS thread draws the chat; the Pressable still
  owns the press (the list opens on release)
  and the accessibility action. The sheet is not an RN Modal: it is mounted
  once at the root over the tab bar and header (BrainWorkOverlayHost), and it
  stays mounted with its slips drawn while shut (Brain publishes the Work, the
  host draws it at low priority, memoised, and not again for the same Work). A
  shut sheet waits below the screen, so opening is one shared value moving on
  the UI thread, the card rising into view; no React commit of the sheet sits
  between the press and the sheet. What a Modal did for free is done by hand:
  Android's back button closes it (Esc on web), the keyboard is dismissed, the
  rest of the app is hidden from screen readers while it is open and focus
  lands on its title. It takes touches at once; dragging the grabber down
  closes it. The sheet shows
  "Work", the goal line and the count line, and keeps the captions bare. Work that needs you also puts a seal dot on
  "Brain" and perches the cat on its slip in the conversation.
- **Loading is not empty.** "Ready when you are" appears only once the
  conversation has loaded and has nothing in it. Until then (connecting,
  waiting for Brain's first snapshot, loading or syncing history) the chat
  shows the chosen pet in the middle, at the empty state's seal size,
  hopping out of its seal and standing, over one line, "Waking up". No
  skeleton bubbles and no spinner. A load under 250 ms shows nothing;
  once the cat shows it stays at least 600 ms and then fades out over
  whatever loaded, so history or "Ready when you are" fades in where it
  stood (`loadingVeilPhase`, the same timing for App start and "Loading
  sessions"). Under reduced motion the cat is the pet's still portrait.
  The composer is there from the start; it says "Connecting…" until Brain
  can take a message. `interfaceTimelinePhase` and `brainScreenSurface`
  decide this, and both are pure and tested.
  Brain's chat never goes back to that screen over messages it has
  already shown: when it remounts or its thread key flaps (a reconnect after
  a daemon restart) it starts from the messages it last showed, without
  Activity, and the next snapshot replaces them in place.
  The cat's tail row says "Waking up" only while Brain is not hydrated; a
  Brain already known that is reconnecting keeps its state until the
  reconnect stalls, then reads "Reconnecting".
- **Goal line.** When Brain has declared an objective
  (`mewla brain objective set`), it heads the Work column and the Work sheet:
  "Ship atlas-notes v1.4 this week · 2 of 5 back". It never takes header
  space over the chat. The daemon stops sending it once it is no longer
  current (`objectiveCurrent`): every child Work is done, everything has
  been back for 30 minutes, or nothing in it has moved for 12 hours. Brain's
  own context still has it, so Brain can clear or restate it.
- A slip in the column opens its live Session, or a small detail sheet when
  the Session is gone. Calendar occurrences that have not run and Brain's
  resource telemetry are not shown; closed Work leaves as soon as the daemon
  stops listing it.
- The state on every slip comes from `brainCurrentWorkLifecycle`, so a Work
  reads the same in the column and in the conversation. A `user_input` wake
  (Brain parked the Work on you) is Needs you.

### Moving Work forward

Every slip carries its next step (`components/brain/brainWorkActions.ts`,
one table for the slip and its sheet; pure and tested). On the slip:
Brain's choices as compact buttons (ink primary, outlined rest), or the one
primary action. In the detail sheet: every action, plus a reply box with
the vermilion Send. Destructive actions confirm first.

| Slip | Actions |
| --- | --- |
| Needs you, with Brain's question | each choice · Something else… · Snooze · Not needed anymore |
| Needs you | Reply · Snooze · Not needed anymore; waiting over a day, the line starts "Waiting 6 days" and the slip offers Not needed anymore beside Reply |
| Outcome unknown (Session ended without a result) | Ask Brain to check · Close it · Not needed anymore |
| Back, or No decision (back over a day) | Accept · Ask Brain about this · Not needed anymore |
| Failed | Retry with Brain · Dismiss |
| Running | Open Worker · Ask Brain about this · Stop (confirm) |
| With Brain (you replied) | the Worker, or Ask Brain about this; nothing back after a day, it is No decision again |
| Closed, unread | Mark reviewed · Ask Brain about this |

**Decision: the user may close Work directly.** Accept closes the Work as
done and Not needed anymore closes it as cancelled, both through the daemon's
audited close with the user as actor. Brain treats a user close as final. The
alternative, Accept as a message for Brain to record, would leave the slip open
until Brain's next turn, which is the dead end this replaces. "Ask Brain"
never sends by itself: it puts `Re: <title> (work <id>)` and the slip's line in
the composer. A reply is a real Brain message bound to the Work. The toast
says "Brain has your answer", or that it may not have arrived when the
admission is uncertain. The reply box takes files like a chat does: the attach
button, and on the web paste or drop, with the same previews and upload queue.
A reply can be words, files or both; it waits while a file uploads, and a
failed send keeps the words and the uploaded files in the box.

**Stuck and stale.** Outcome unknown and No decision are the Warning state
(triangle) under Needs you; a snoozed slip waits under Waiting with
"Snoozed" for the hour Snooze asked. A running slip leads its line with the
Worker's reported phase ("Verifying · Running go test"). These are app
derivations from `attention_since`, `attention_reason` and `snoozed_until`;
the daemon writes nothing on a timer.

A Work result in the conversation carries that Work's inline actions while
the Work is current; a tap still opens the result itself, so you read what
came back before you accept it.

In the conversation, every Work result is a full slip (`WorkSlip`):
executor · project · time, the state, the title and one line. A Work's slip
moves to when its newest result came back. A turn's finished tool rows fold
into one row that says what they did ("Ran 1 command, edited 1 file") with
its kind's icon and the files in soft, and opens onto the real rows; Brain and
a Session fold alike (`core/timeline/foldToolRuns.ts`). The composer says "Tell Brain", with no ellipsis.

## Type

| Role | Face | Size / line | Use |
| --- | --- | --- | --- |
| `display`, `largeTitle` | Bricolage Grotesque ExtraBold | 34/40, 30/36, −0.025em | hero numbers, screen titles, wordmark |
| `title` | Bricolage Grotesque SemiBold | 20/26, −0.3 | sheet and empty-state titles |
| `heading` | Inter SemiBold | 17/24 | row titles |
| `body`, `compact` | Inter Regular | 15/24, 14/22 | everything read at length |
| `label`, `micro` | Inter Medium | 13/18, 11/15 | controls, chips |
| `caption` | Inter Regular | 12/17 | metadata |
| `mono` | Maple Mono (CN build, CJK removed) | 13/20 | code |
| terminal (phones) | Maple Mono NF (bundled, CJK removed) | 8, line = the face's height | terminal grid |

- **CJK on web.** Each family is a stack: Inter, then Source Han Sans SC,
  Noto Sans CJK SC, PingFang SC, Microsoft YaHei. Only the Latin faces are
  shipped to the browser.
- **CJK on native.** There are no font stacks on native. Inter and Bricolage
  fall back to the OS CJK face (PingFang SC on iOS, Noto Sans CJK on Android,
  which is Source Han's design); no CJK UI face is bundled. Line heights are
  fixed per role, so fallback glyphs never change a row's height. **Native
  CJK rendering is not yet verified on a device.**
- **Numbers are not code.** Costs, counts and percentages use the UI face
  with `fontVariant: ["tabular-nums"]`; mono is for identifiers a person
  might type (model ids, commands, paths) and for code.
- **Mono.** JetBrains Mono is the landing's mono, but the app keeps Maple Mono:
  its 0.6em cell, box drawing, braille and Powerline glyphs set the terminal
  grid. The bundled files are the Maple Mono CN build with every CJK
  codepoint removed by `scripts/subset-maple-mono.py` (about 340 KB per
  weight). CJK comes from the OS face, as in the rest of the UI.
- **The terminal on the phones.** One renderer design on Android and iOS
  (`client/native/terminal`): cells are whole device pixels and the text size
  is snapped so one Maple advance is exactly one cell; every glyph is placed
  at its column. Box drawing, blocks, braille, Powerline and the media marks
  (⏴⏵⏸⏹⏺) are drawn as shapes for the cell (`TerminalGlyphs.cpp`), so lines
  and blocks meet across rows. Anything else not one cell wide in the face
  (CJK, emoji, fallback symbols) is centred in its cells and scaled down if
  it is wider. An emoji the VT gives one cell (❤️, ⚠️, 1️⃣) is drawn at full
  size across the next cell when that one is blank, and fitted otherwise; two
  regional indicators draw one flag across their cells. Italic is Maple's real
  italic where it has the glyph. The size is 8 (about 83 columns on a phone,
  the shipped app's density); no setting.
- **CJK in chat mono.** Highlighted code blocks and inline code are RN
  `Text`, which falls back to the OS CJK face, so Han there takes that face's
  advance instead of exactly two cells. Plain and ASCII-art blocks already
  render in a WebView with the system monospace stack
  (`PreformattedCodeWebView`); neither keeps Chinese columns at 2:1.
- **Web weights.** expo-font declares every face at weight 400, and each
  weight here is its own family. Web therefore sets
  `font-synthesis-weight: none` (in `appFontAssets.web.ts`) so browsers don't
  fake bold.
- **Added fonts.** All five are SIL OFL 1.1; see
  `docs/third-party-assets.md`.

  | File | Size |
  | --- | --- |
  | `Inter-Regular.ttf` | 325 KB |
  | `Inter-Medium.ttf` | 325 KB |
  | `Inter-SemiBold.ttf` | 326 KB |
  | `BricolageGrotesque-SemiBold.ttf` | 82 KB |
  | `BricolageGrotesque-ExtraBold.ttf` | 82 KB |

  Total: about 1.14 MB.

## Shape, borders, shadow

- **Radius.** These are the existing `Radii` steps. Phone cards stay at 20,
  with 24 for hero cards and sheets at 28. Buttons, chips and the composer
  are pills (999), like the landing's `.btn`.
- **Borders first.** On paper, structure comes from a hairline
  (`borderSubtle`) or from tint. Shadows are reserved for things that float,
  like sheets and menus (`shadow('float')`). The landing's warm shadow
  (`0 18px 40px -18px rgba(60,40,20,.18)`) is the reference, and Android
  stays flat for in-flow cards.
- **Materials.** Bars use paper at 86% over a blur, matching the landing's
  sticky header. Dark-mode separators are white at 10% (`--dline`).

## Motion

- **Easing.** The landing's `cubic-bezier(.2,.8,.2,1)` for UI transitions. The
  springs in `constants/motion.ts` still apply to presses and sheets.
- **The cat.** The cat is driven on the UI thread (Reanimated). Each standing
  state is one to six precomputed SVG frames, flipped by opacity, so there
  are no per-frame React renders. The sleeping seal breathes (a 3.6 s scale)
  and lets out three z's. Kneading is six frames at 210 ms (the `sit` pose,
  front paws treading ±0.2 rad in turn, a 14° tail swish). The waking
  ear is one extra layer: two flicks of up to 18° in the first 0.6 s of
  every 3.2 s.
- **Stopping.** All cat motion stops when the screen loses focus or the app
  is backgrounded. Under reduced motion it stops completely: the seal is
  still and the z's are fixed, as on the landing.

## Iconography

One vocabulary, one component: `components/icons/Icon.tsx` with named
glyphs (`IconName`). Every glyph is an original Mewla drawing in
`components/icons/mewlaGlyphs.ts`, in the spirit of Hugeicons' Stroke
Rounded set but never copied or traced from it. The grammar:

- **Grid.** A 24 px artboard. Live shapes sit inside 2–22 (circles r 9.25,
  boxes about 18), and nothing, stroke included, crosses 0.75 from the edge.
- **Stroke.** 1.5 on the grid, so about 1.25 pt at 20 pt. Caps and joins
  are round, and the stroke is `currentColor` (the `color` prop).
- **Corners.** Soft and generous. Boxes take radius 3–5 at full size (about
  a quarter of the side), and every bend in a line is eased (`soft()`), so
  even chevrons and arrowheads have no hard point.
- **Geometry.** Open and friendly: outline only, few strokes, dots for
  detail (r 1), and no hairline inner detail that vanishes at 16 px.
- **Mewla touches.** Brain is a round chat bubble with two soft cat ears,
  and the menu is two lines. Skills is a recipe card: a soft card with three
  steps, the first one ticked. A Skill is a written procedure an agent
  follows, so the glyph shows one, not sparkles, stars or a wand.

Add a glyph by drawing it in `mewlaGlyphs.ts` with the same helpers
(`box`, `circle`, `soft`, `dot`), never by importing an icon font or
pasting another set's paths. `iconVocabulary.test.ts` checks the number
format, that every glyph stays inside the grid, and the cat ears.

- Glyphs are ink (`textSecondary` at rest, `textPrimary` when selected),
  never vermilion; Send and the seal keep their own colours. The Brain
  glyph's ears are the cat's name in the icon set, not a second cat: the
  one-cat rule counts the vermilion seal and its poses.
- `*-fill` variants exist only where a filled shape carries state (notices,
  toasts, selection checks, an expanded calendar, the selected radio, the
  active step dot). Their marks are cut out with the even-odd rule, so they
  work on any ground. The one other fill is `contrast`'s half disc, which is
  what the glyph means. Everything else is outline.
- Glyphs are decorative; the control around them carries the label. Sizes
  and hit targets are the control's, unchanged by the set.
- Real product marks (Claude, Codex, GitHub, Slack, Google and the agent
  logos) keep their own logos, as on the landing; `iconVocabulary.test.ts`
  keeps icon fonts to those two places. The seal is the brand mark.
- Service logos in Plugins (GitHub, Slack, Google, Notion, Linear) are all
  monochrome in the text colour, on the same tint tile. A logo identifies the
  service; whether it is connected is the status pill's job, so no logo takes
  its brand colour, on or off.

### Brand assets

Every brand asset is the default pet (`p05`, Dango) in the vermilion seal. The art
is generated (provenance in `docs/third-party-assets.md`) and committed as
PNGs in `assets/branding/mewla-*`; `mewlaBrandAssets.test.ts` checks their
sizes, that App Store icons carry no alpha, and that the config points at
them in the theme's colours.

| Surface | Drawing |
| --- | --- |
| iOS icon | The pet's face in the seal on paper `#F6F1E8` |
| iOS dark / tinted | The same on warm ink / a white silhouette for the system tint |
| Android adaptive | Paper `#F6F1E8` background colour; the seal and face fitted to the safe circle |
| Android themed | The silhouette, for the system tint |
| Splash | The seal at 128 pt on paper `#FBFAF7` / ink `#141210` (fits Android 12's 192 dp circle mask) |
| Notification | The white silhouette, tinted vermilion |
| Favicon | The seal and face at 48 px |
| Drawer and About | `MewlaMark`, `assets/branding/mewla-mark.webp` |

The mark is the logo, not Brain: it never moves, changes with state or
follows the chosen pet. The drawer, the About row and Onboarding's brand row
all show `MewlaMark`. Small inline waits (composer send, upload chip, sheets)
use the platform activity indicator in ink, never a brand mark.

## Connecting a service

Plugins is one list with one row per service, and connecting is one action
(`connect` in `components/plugins/PluginsFlow.tsx`). The row's Connect, the
service page's Connect and Add another account, Reconnect, and allowing
changes that the service must approve all call it.

- **The target is three steps:** tap Connect on the list, approve on the
  service's page, and land back on the list with the service Connected. Read
  and search is on by default. Making changes is a follow-up, offered once on
  the result card and kept as a switch on the service page, never a gate
  before connecting.
- **A row's trailing control is its one action:** a small tinted Connect, or
  Reconnect when an account needs sign-in again. Otherwise it shows a status
  pill and a chevron, and the row opens the service page. The service page
  lists every account as a card: status, Read and search, Make changes when
  asked, Tools & activity, then Disconnect, which confirms inline on the card.
- **One card at the top of every Plugins page** follows the sign-in in
  progress: Waiting for the service (Open again, Cancel), a GitHub device code
  in large type, Checking, then Connected or Couldn't connect with Start
  again. Nothing waits silently.
- **Progress sits on the control that started it.** `running` names the
  request in flight, so only that button or switch spins; the rest are
  disabled against double taps.
- **Status words** are Connected, Needs sign-in again, Off and Last call
  failed. Recovery copy says what to do: Reconnect, Turn on, Check again.
- **Web and phone share the model.** The web UI's sign-in returns its tab to
  `/plugins` through the daemon's callback; the phone returns through
  `mewla://plugins`. Neither changes the UI.

## Sessions and Worker chat

- **Sessions.** Plain rows on the paper, grouped by directory with a quiet
  caption when there is more than one; no cards and no separators. A row's
  only fill is the selection tint, and in selection mode the check takes its
  own leading column. New session is the page's one ink action: a round
  plus with no words on it, floating at the bottom right
  (`components/workers/SessionsListView.tsx`), never a full-width bar. Its
  name lives in the accessibility label and the tooltip ("New session (N)" with
  a keyboard). Nothing sits
  above the list unless the server is unreachable.
- **Worker chat.** On wide the conversation and its composer sit in the
  760 pt reading column and the terminal takes the whole pane, the same
  content in either (see The wide layout). The header avatar carries the Session's
  StatusMark (Running, Blocked, Failed only).
- **Tool rows.** A bare soft-ink tool glyph, the title, then the StatusMark
  in place of the old mono status word: Running and Waiting spin the arc,
  Succeeded and Passed are the Ready check, Failed is the crossed box with its
  cause ("exit 1"), Blocked is the dashed ring. "Done" and "Finished" only
  say the call returned, so they show nothing. A failed call is an ordinary
  row: it starts collapsed and folds into its run of finished calls, and the
  fold row's detail counts it ("5 steps · 1 failed"). Its output sits in
  the same neutral well as any other, in full ink
  (`splitActivityStatusDetail` in `InterfaceTimelineActivityModel.ts`).
  Agent status rows (Failed, Blocked) follow the same rule: they start
  collapsed with their normal mark; only a running one opens by itself.
- **Git.** Added is green and deleted is the terminal's crimson; modified and
  renamed are ink, because amber means Warning and blue means Running.

## Terminal

The grid sits on the app canvas: paper `#FBFAF7` with ink text in light, the
landing's `#141210` with `#E9E4DB` in dark. The cursor is ink (the accent)
and selection is the Running blue (`constants/terminalThemes.ts`). The 16
ANSI colours are warm-tuned for TUIs and tested in `terminalThemes.test.ts`:

- every normal colour reads at 4.5:1 on its canvas and every bright one at
  3:1 (dark "black" is a background by convention);
- light "white" is a stone grey, as in most light terminal themes, so text a
  program prints in white stays legible on paper;
- red is a crimson at OKLab ΔE > 0.075 from the seal and its text, so a
  deleted line never reads as "Needs you".

Every renderer (libghostty-vt on the phones and, as WebAssembly, on the web) reads the same palette, and
chat code, inline code and git diffs inherit it.

## The pet

Brain is a pet the user picks in Settings › Pet (soft-final SPEC §5, §15 Picker grid): six pets, each in
its own medium — Dango (`p05`, the default), Ash (`p11`), Kami (`p12`), Bit (`p13`), Doodle (`p14`) and
Riso (`p15`) — one per device, stored under `mewla:pet_preference`. A stored pet that is no longer in the
table (the ten of 0.3.x) falls back to Dango for everyone; a friend's avatar on such a pet shows Dango's
portrait.

The table is `client/core/pets/manifest.json` (the pets pack's manifest as delivered, without its file
lists): per pet its name, frame (256 px for Dango, 192 for the others), `hopBleed` and, per clip, the
canvas size, frames, delays, loop and bleed. `core/pets/manifest.ts` types it; `ui/pets/assets.ts`
imports each pet's files from `client/ui/assets/pets/<id>/`: the clips `idle`, `waking`, `going_back`,
`working`, `delegating`, `attention`, `delivered`, `offline`; a still per state (`<state>-still.webp`,
the cat's larger side at 90 % of the frame); `portrait`; and `homeless`, the empty bed drawn once at
each pet's frame size. Adding a pet is its folder, its manifest entry and its imports.

**One drawing path.** Every cat is drawn by `ui/pets/PetSprite.tsx`. The hops, `attention` and
`delivered` sit on a square canvas `1 + 2 × hopBleed` the frame, centred on it, so the sprite draws them
larger than the box with negative offsets (`clipFrame`) and the cat keeps its size; nothing around it
clips them. Moving between the nap and its feet plays the hop first (`petTransition`): `waking` on the
way out, `going_back` on the way in. Modes: `loop` (the state's clip keeps playing: Sessions' empty
state, the computer page, Onboarding), `once` (the header cat: a change of state or the app coming to
the front plays the clip for whole passes of at least 2 s, then the state's still holds; a Work coming
back done plays `delivered` once) and `still` (the picker's poses). Reduced motion, the app in the
background, a scroll, the keyboard up or a page pushed over home hold the still.

### State map (the header cat)

Highest first (`core/pets/cat.ts`): no computer `homeless` (the empty bed) · offline, pair again or
Brain can't start `offline` (the nap, grey, at 72 %) · connecting or Brain starting `waking` · something
needs you `attention` · Brain's turn `working` · Workers running `delegating` · quiet `idle` (the nap).
`delivered` plays once over whatever follows. Tapping the cat opens the computer page.

## Rollout

Stage A (branch `mewla-seal-slip`) shipped the tokens and contrast tests,
StatusMark and StatusPill, list rows, empty states and notices, Send, the
navigation shell (menu, switch, sidebar), the Brain slips and the cat's
perch, TaskNotificationCard, Settings, Plugins, Onboarding and the Session
row marks. Still to get a dedicated pass:

1. ~~Sessions list and Worker chat chrome~~: done (see Sessions and Worker
   chat above).
2. ~~**Calendar, Skills, Stats, Resources, Model Providers, Browser,
   Work**~~: done in the tools pass (see below).
3. **Brand wordmark (with the rename)**: the app icon, adaptive and
   monochrome icons, splash, favicon, notification icon and the drawer,
   About and Onboarding marks are done (see Brand assets); the wordmark
   itself and the display name change with the rename.

**Tools pass.** Calendar rows have a neutral hairline rail and the state's
mark (`calendarStatusMark`): a due reminder or deadline is Needs you,
Scheduled has no mark. The Providers radio is an ink ring and dot, never the
Ready check; the gateway reads Ready. Status colours are never decoration:
Stats bars and the Activity curve, folder icons, inline code and PSI charts
are ink until a state calls for colour. Row Delete actions are soft ink; only the confirmation is
oxblood. Offline is a Warning, not a failure. Resources keeps Critical on the
Failed mark because processes are about to be killed.

**Calendar item.** A Calendar item opens as a sheet (a 760 pt column on wide
screens): the title, then its schedule in words ("Every day at 8:30 PM ·
Next: tomorrow 8:30 PM", the zone only when it is not the device's), the
one-line summary, a collapsed **Instructions**, and its runs newest first
(date, state mark and word, the result's first line; five, then "Show all").
A run opens in the same sheet: its full result, or its failure on the run,
never on the item; a running run opens its Session. No ids anywhere: the
thread, the Work and the run are taps. An agenda row reads "Running ·
summary" on one line, and a running row opens the live Session.

**Stats** has three ranges, 7 / 14 / 30 days (the daemon's `week`,
`fortnight`, `month`), and opens on 14; a computer whose daemon has no
`fortnight` shows 7 and 30 and opens on 7. The title is the range's total
spend under the eyebrow "Last 14 days". The one chart is spend by day: ink
bars, a tooltip over the day ("Today · $5.20"), moved by hovering, dragging
or ← →, and a tap opens the day. When no day in the range has a known cost
it draws activity instead (tokens, or sessions when tokens are unknown),
titled Activity rather than Spend.

**Resources** answers "what is using my machine, and is anything wrong". It
leads with the pressure state (mark and word) and its freshness, never the
server's name; threshold signals follow, then four compact tiles (label and
value on one line). Refresh and machine details are icon buttons, the
consumer sort is two icon toggles (memory, CPU), filters are one-line chips
with an owner glyph, and a row's owner is a glyph rather than a repeated
word. Only an orphaned Worker keeps words ("Residual") and warning ink. Rows
expand on a chevron, and more rows load from a chevron "N more". Icon-only
controls carry an accessibility label, which `IconButton` also shows as the
web tooltip.

Risky areas to watch:

- **Terminal.** The palette is checked on web with a git diff,
  ls, htop and vim sample (`screenshot-demo?state=chat&fixture=terminal`).
  The native ghostty grid still needs a device check.
- **Markdown.** Chat messages (Brain, Sessions, Work) are one marked parse
  (`InterfaceMessageMarkdownModel`) drawn by `InterfaceMessageBlock`; the
  mermaid theme reads `surfaceMuted` and the accent (ink). The Brain
  workspace and Skills file viewers use the same reader. Check tables and
  code in sent messages.
- **Native modules and fonts.** Font registration differs per platform. Check
  CJK fallback on real iOS and Android devices.
- **Android elevation and translucency.** Keep in-flow cards flat.
