# Mewla design language (app)

The app follows the landing page (`site/index.html`, `site/styles.css`,
`site/sealcat.js`): warm paper, warm ink and one vermilion seal with a cat
carved in it. This file maps that language onto the app's theme system
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
Focus rings and text selection use the Running blue. The Stats heatmap ramps
toward the Ready green, because activity is good news.

**Chat.** Your messages sit on a quiet paper tint (`#F0ECE4`, dark
`#2E2A25`) in ink; Brain's replies sit on the page. Links are ink and
underlined. The conversation is capped at a reading width of 820 pt
(`ChatCanvas`).

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

The old sage, ink, clay and stone accent picker is gone; any stored accent
preference resolves to ink.

## Navigation

- **Phone.** The app bar has the menu (☰), the Brain · Sessions switch (text
  tabs with an ink underline; a seal dot on Sessions when a Session needs
  you) and one ⋯ page action. The menu slides over the page.
- **Wide (≥ 1024 pt, any platform).** The menu docks as a permanent sidebar;
  the app bar shows the page title.
- **Menu.** One model on every layout, one ordered list without captions or
  cards: Brain, Sessions (selected in place, tint on the current one, seal dot
  when a Session needs you), Calendar, Plugins, Skills, Stats, Resources. On
  phone, choosing Brain or Sessions also closes the menu. Settings sits apart
  at the bottom, and under it one quiet line names the current server and its
  state. Rows are a soft-ink glyph and a label, no tiles.
- **Connection status has one home**: that footer line. It is read-only
  (switching lives in Settings), and healthy is quiet: only Offline or a
  connection issue takes colour. Pages never open with a standing server
  header. A page names the server only when it is offline and the page can't
  work, as a compact `ServerOfflineNotice` (Sessions' offline notice is the
  same idea). Context the page needs stays, such as the project Skills were
  read for.
- Settings holds Servers, Channels (Telegram), Agents (Model Providers),
  Appearance and About. Browser stays hidden from the menu.
- **One home per destination.** Every screen you can go to (Calendar,
  Plugins, Skills, Stats, Resources, Settings) has exactly one entry point:
  its menu row. A ⋯ menu, a header title tap or a page button never repeats
  a menu destination. A ⋯ menu holds only actions on the current screen:
  Brain's is New chat, Switch executor, Open terminal, Browse workspace; a
  Session's is New terminal, Rename, Model, Open Web, Open Brain (its linked
  Work), Terminate. Sessions' ⋯ holds New session and Services. Contextual
  recovery links ("Open Settings" in a no-server empty state) are fine: they
  fix the screen you are on rather than navigate elsewhere.

## Brain and its Work

Brain has one Work surface, built from the daemon's `current_work`
(`components/brain/brainWorkSurface.ts`, pure and tested):

- **Wide (≥ 1024 pt).** A 360 pt Work column to the right of the conversation
  (`BrainWorkColumn`): "Work", a count line, then slips grouped Needs you ·
  Running · Back · Waiting. The cat sits on the first Needs-you slip there,
  and the conversation drops its between-turn tail row. While Brain's turn
  runs, the cat is in the Working row and the column has none.
- **Phone.** Under the app bar, one line ("● 6 need you · 2 running · 2 back ›")
  opens the same grouped list as a sheet. The seal dot shows only when
  something needs you.
- **Goal line.** When Brain has declared an objective
  (`mewla brain objective set`), it sits under the title:
  "Ship atlas-notes v1.4 this week · 2 of 5 back".
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
admission is uncertain.

**Stuck and stale.** Outcome unknown and No decision are the Warning state
(triangle) under Needs you; a snoozed slip waits under Waiting with
"Snoozed" until tomorrow 09:00. A running slip leads its line with the
Worker's reported phase ("Verifying · Running go test"). These are app
derivations from `attention_since`, `attention_reason` and `snoozed_until`;
the daemon writes nothing on a timer.

A Work result in the conversation carries that Work's inline actions while
the Work is current; a tap still opens the result itself, so you read what
came back before you accept it.

In the conversation, every Work result is a full slip (`WorkSlip`):
executor · project · time, the state, the title and one line. A Work's slip
moves to when its newest result came back. A turn's tool rows fold into one
quiet "Worked · N steps" row that expands to the steps (Brain only; Session
chats keep every row). The composer says "Tell Brain…".

## Type

| Role | Face | Size / line | Use |
| --- | --- | --- | --- |
| `display`, `largeTitle` | Bricolage Grotesque ExtraBold | 34/40, 30/36, −0.025em | hero numbers, screen titles, wordmark |
| `title` | Bricolage Grotesque SemiBold | 20/26, −0.3 | sheet and empty-state titles |
| `heading` | Inter SemiBold | 17/24 | row titles |
| `body`, `compact` | Inter Regular | 15/24, 14/22 | everything read at length |
| `label`, `micro` | Inter Medium | 13/18, 11/15 | controls, chips |
| `caption` | Inter Regular | 12/17 | metadata |
| `mono`, terminal | Maple Mono CN | 13/20 | code, terminal grid |

- **CJK on web.** Each family is a stack: Inter, then Source Han Sans SC,
  Noto Sans CJK SC, PingFang SC, Microsoft YaHei. Only the Latin faces are
  shipped to the browser.
- **CJK on native.** There are no font stacks on native. Inter and Bricolage
  fall back to the OS CJK face (PingFang SC on iOS, Noto Sans CJK on Android,
  which is Source Han's design). The bundled Source Han Sans SC stays
  registered as the `cjk` role. Line heights are fixed per role, so fallback
  glyphs never change a row's height. **Native CJK rendering is not yet
  verified on a device.**
- **Mono.** JetBrains Mono is the landing's mono, but the app keeps Maple Mono
  CN: it has CJK at a 2:1 cell width, and xterm and ghostty size cells from
  the first family.
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
  and the menu is two lines.

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

Every brand asset is the seal: seal-icon.svg's vermilion block with the
curled paper cat under a crescent moon. `components/mewla/mewlaBrandArt.ts`
draws them all from `sealCatGeometry.ts`; `bun scripts/render-mewla-brand.ts`
writes the SVG sources (`assets/branding/source/`) and the PNGs
(`assets/branding/mewla-*.png`), and `mewlaBrandArt.test.ts` fails when a
committed file drifts or the config strays from the theme colours.

| Surface | Drawing |
| --- | --- |
| iOS icon | The seal is the icon: vermilion full bleed, cat and moon inside 80% |
| iOS dark / tinted | The seal stamped at 72% on transparent (iOS adds the dark ground) / white seal, cat cut out, on black |
| Android adaptive | Vermilion background colour; the paper cat and moon fitted to the 66 dp safe circle |
| Android themed | The cat and crescent alone, knife lines cut through |
| Splash | The seal at 128 pt on paper `#FBFAF7` / ink `#141210` (fits Android 12's 192 dp circle mask) |
| Notification | The white seal with the cat cut out, tinted vermilion |
| Favicon | The seal, carved for 48 px |
| Drawer and About | `MewlaMark`, the same seal in react-native-svg |

The mark is the logo, not Brain: it never moves or changes with state. The
drawer, the About row and Onboarding's brand row all show `MewlaMark`. Small
inline waits (composer send, upload chip, sheets) use the platform activity
indicator in ink, never a brand mark.

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
  own leading column. "New session" is the page's one ink pill: compact,
  content-width, floating at the bottom right
  (`components/workers/SessionsListView.tsx`), never a full-width bar. Nothing sits
  above the list unless the server is unreachable.
- **Worker chat.** The conversation keeps Brain's 820 pt reading width; the
  terminal grid stays full width. The header avatar carries the Session's
  StatusMark (Running, Blocked, Failed only).
- **Tool rows.** A bare soft-ink tool glyph, the title, then the StatusMark
  in place of the old mono status word: Running and Waiting spin the arc,
  Succeeded and Passed are the Ready check, Failed is the crossed box with its
  cause ("exit 1"), Blocked is the dashed ring. "Done" and "Finished" only
  say the call returned, so they show nothing. A failed call's output sits in
  the same neutral well as any other, in full ink
  (`splitActivityStatusDetail` in `InterfaceTimelineActivityModel.ts`).
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

Both renderers (xterm.js on web, ghostty on native) read the same palette, and
chat code, inline code and git diffs inherit it.

## The cat

The geometry has one source: `site/sealcat.js` and `site/seal-icon.svg`.
`components/mewla/sealCatGeometry.ts` copies its paths, poses and rig as pure
data, and `sealCatGeometry.test.ts` reads the landing files and fails on any
drift. Rendering uses react-native-svg, which is already a dependency, so no
Lottie or new package is needed. The landing's ink-paste filters
(`feTurbulence`) are not ported. The app draws the clean `seal-icon.svg`
look, with the landing's bolder carving at small sizes.

### State map

| Product state | Cat | Where | Tap |
| --- | --- | --- | --- |
| Brain idle | Curled in the seal, breathing, three z's | Brain empty state; tail row "All quiet" | Says the status: "All quiet. 2 running, nothing needs you." |
| App start, connecting, loading history | In the seal, one eye open, the right ear flicks twice every 3.2 s | App start (`CatSplash`), Brain status screen, chat loading, Sessions loading/connecting; tail row "Waking up" | "Still waking up…" |
| Brain's turn running | Out of the seal, sitting up and kneading, tail swishing | Working row, which also shows Brain's newest step | "Right now: Read routing.md" |
| Work needs your input | Alert, ears up, seal ping | **Perched on the newest slip of Work that needs you**; the tail row ("6 need you") only when no such slip is in this conversation; on wide screens, on the first Needs-you slip in the Work column | Opens the first Work that needs you |
| Delegated Work on Workers | Sitting, dispatch dots | Tail row, "Waiting on 3 Workers" | The status line |
| Unread result | Loafing with a parcel | Tail row, "Brought 2 things back" | The status line |
| Offline | Asleep in a greyed seal | Brain status screen, Sessions offline; tail row "Can't reach your computer" | Retries the connection: "Knocking on your computer…" |
| No computer paired | The empty bed (a ghost cat in the seal) | Brain status screen, Sessions, Onboarding | Opens pairing |
| Paired | Asleep in the seal (moved in) | Onboarding, connected; an empty Work sheet | — |

**Tap feedback.** Every tap has a body response (`TappableCat`): a standing
cat hops 12% of its size and lands on the landing's curve (130 + 220 ms); a
cat in the seal stirs (a 5% squash). A selection haptic goes with it. The
answer replaces the row's text for 4.5 s and is announced to screen readers.
Under reduced motion only the haptic and the words remain. The cat's poses
still mean only Brain states; a tap never changes the state.

State comes from `resolveBrainCatPresence` (`components/mewla/brainCatState.ts`).
Placement comes from `mergeBrainPresenceIntoTimeline`
(`brainPresenceTimeline.ts`): a running turn keeps the cat in the Working
row; otherwise "attention" perches it on the newest slip of any Work that
needs you, and the tail row is dropped. The tail row is presence only: a
count, never one Work's title (a single title went stale for days), and
tapping it opens the Work list. Both are pure and tested, and there is
never more than one cat on screen. Session chats never provide
`BrainCompanionContext`, so they have no cat.

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
Stats bars, folder icons, inline code and PSI charts are ink until a state
calls for colour. Row Delete actions are soft ink; only the confirmation is
oxblood. Offline is a Warning, not a failure. Resources keeps Critical on the
Failed mark because processes are about to be killed.

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

- **Terminal.** The palette is checked on web (xterm.js) with a git diff,
  ls, htop and vim sample (`screenshot-demo?state=chat&fixture=terminal`).
  The native ghostty grid still needs a device check.
- **Markdown.** Native enriched-markdown styles and the mermaid theme read
  `surfaceMuted` and the accent (ink). Check tables and code in sent messages.
- **Native modules and fonts.** Font registration differs per platform. Check
  CJK fallback on real iOS and Android devices. Decide whether to drop the
  bundled Source Han Sans SC files, which would save about 33 MB.
- **Android elevation and translucency.** Keep in-flow cards flat.
