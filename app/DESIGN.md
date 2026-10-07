# Mewla design language (app)

The app follows the landing page (`site/index.html`, `site/styles.css`,
`site/sealcat.js`): warm paper, warm ink and one vermilion seal with a cat
carved in it. This file maps that language onto the app's theme system
(`app/theme/`, `app/constants/tokens.ts`). It does not define a second one.
Product and repository names are still `zen` in code; the rename is a
separate change.

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
   per screen, and every pose reflects a real Brain state.
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
`danger` Failed, `needs` the seal pill, and `neutral` a plain paper tag.
Brain lifecycles map onto the six states in `brainWorkLifecycleStatus`
(Reviewing runs, Waiting and Cancelled are inert, Needs review warns, Done
and Ready share the check).

The old sage, ink, clay and stone accent picker is gone; any stored accent
preference resolves to ink.

## Navigation

- **Phone.** The app bar has the menu (☰), the Brain · Sessions switch (text
  tabs with an ink underline; a seal dot on Sessions when a Session needs
  you) and one ⋯ page action. The menu slides over the page.
- **Wide (≥ 1024 pt, any platform).** The menu docks as a permanent sidebar
  with Brain and Sessions as its first rows; the app bar shows the page title.
- **Menu.** The current server (read-only; switching lives in Settings), then
  *On this computer*: Calendar, Plugins, Skills, Stats, Resources; then
  *App*: Settings. Rows are a soft-ink glyph and a label, no tiles.
- Settings holds Servers, Channels (Telegram), Agents (Model Providers),
  Appearance and About. Browser stays hidden from the menu.

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
  state is two to eight precomputed SVG frames, flipped by opacity, so there
  are no per-frame React renders. The sleeping seal breathes (a 3.6 s scale)
  and lets out three z's.
- **Stopping.** All cat motion stops when the screen loses focus or the app
  is backgrounded. Under reduced motion it stops completely: the seal is
  still and the z's are fixed, as on the landing.

## Iconography

Ionicons outline remains the UI glyph set, at a 1.5–1.8 stroke feel matching
the landing's line icons. Real product marks (Claude, Codex, GitHub and so on)
keep their own logos, as on the landing. The seal is the brand mark. The
app icon and notification icon are planned in the rollout below; the
`zen-*` branding assets are untouched here.

## Terminal

The terminal grid keeps its own ANSI palette (`constants/terminalThemes.ts`).
Chat chrome takes the new tokens (cursor = accent). Retuning the terminal
canvas to the landing's `#141210` / `#E9E4DB` is a separate, risky slice
(see the rollout below): TUIs depend on the 16 ANSI colours, and both the
xterm.js and ghostty renderers read them.

## The cat

The geometry has one source: `site/sealcat.js` and `site/seal-icon.svg`.
`components/mewla/sealCatGeometry.ts` copies its paths, poses and rig as pure
data, and `sealCatGeometry.test.ts` reads the landing files and fails on any
drift. Rendering uses react-native-svg, which is already a dependency, so no
Lottie or new package is needed. The landing's ink-paste filters
(`feTurbulence`) are not ported. The app draws the clean `seal-icon.svg`
look, with the landing's bolder carving at small sizes.

### State map

| Product state | Cat | Where |
| --- | --- | --- |
| Brain idle, empty chat | Curled in the seal, breathing, three z's | Brain empty state |
| Connecting or loading | In the seal, one eye open | Brain empty state (busy), status screen |
| Brain's turn running | Out of the seal, walking in place | Working row, newest edge of the Brain timeline |
| Work needs your input | Alert, ears up, seal ping | **Perched on that Work's newest slip**; the tail row only when the slip is not in this conversation |
| Delegated Work on Workers | Sitting, dispatch dots | Tail row, "Waiting on Workers · title" |
| Unread result | Loafing with a parcel | Tail row, "Brought something back · title" |
| Offline | Asleep in a greyed seal | Brain status screen |
| No computer paired | The empty bed (a ghost cat in the seal) | Brain status screen, Onboarding "Give Brain a home" |
| Paired | Asleep in the seal (moved in) | Onboarding, connected |
| Failure, pull to refresh, app icon, splash, notification icon, haptics | — | later |

State comes from `resolveBrainCatPresence` (`components/mewla/brainCatState.ts`).
Placement comes from `mergeBrainPresenceIntoTimeline`
(`brainPresenceTimeline.ts`): a running turn keeps the cat in the Working
row; otherwise "attention" perches it on the newest slip whose `work_id`
matches, and the tail row is dropped. Both are pure and tested, and there is
never more than one cat on screen. Session chats never provide
`BrainCompanionContext`, so they have no cat.

## Rollout

Stage A (branch `mewla-seal-slip`) shipped the tokens and contrast tests,
StatusMark and StatusPill, list rows, empty states and notices, Send, the
navigation shell (menu, switch, sidebar), the Brain slips and the cat's
perch, TaskNotificationCard, Settings, Plugins, Onboarding and the Session
row marks. Still to get a dedicated pass:

1. **Sessions list and Worker chat chrome**: the server header pill, the FAB,
   the activity rows' mono status words, the chat header.
2. **Calendar**: the coloured event-type bars and its status words.
3. **Skills, Stats, Resources, Model Providers, Browser, Work**: they take the
   tokens; check hard-coded colours (`rg '#[0-9A-Fa-f]{6}' app/components`).
4. **Brand assets (with the rename)**: wordmark, app icon, adaptive and
   monochrome icons, splash, notification icon, all from `seal-icon.svg`.

Risky areas to watch:

- **Terminal.** The ANSI palette and two renderers. Keep it in its own slice
  with screenshots of real TUIs (vim, htop, git diff).
- **Markdown.** Native enriched-markdown styles and the mermaid theme read
  `surfaceMuted` and the accent (ink). Check tables and code in sent messages.
- **Native modules and fonts.** Font registration differs per platform. Check
  CJK fallback on real iOS and Android devices. Decide whether to drop the
  bundled Source Han Sans SC files, which would save about 33 MB.
- **Android elevation and translucency.** Keep in-flow cards flat.
