# Mewla design language (app)

The app follows the landing page (`site/index.html`, `site/styles.css`,
`site/sealcat.js`): warm paper, warm ink and one vermilion seal with a cat
carved in it. This file maps that language onto the app's theme system
(`app/theme/`, `app/constants/tokens.ts`). It does not define a second one.
Product and repository names are still `zen` in code; the rename is a
separate change.

## Principles

1. **Paper and ink.** Content sits on warm white paper with warm-ink text. Use
   hairlines and tint for structure, not colour.
2. **One accent.** Vermilion is the seal. It marks the one thing that wants you
   (the primary action, the selection, the unread dot), and it is never a
   status.
3. **One cat.** The cat is Brain. It appears only where Brain is, and every
   pose it takes reflects a real Brain state. It never decorates.
4. **Quiet motion.** Motion eases out on the landing's curve, stops when the
   screen is hidden, and is off under reduced motion.

## Colour

All colours are `AppColors` fields built in `theme/primitives.ts`.
`theme/contrast.test.ts` enforces WCAG AA in both schemes: 4.5:1 for text,
3:1 for affordances, and OKLab ΔE > 0.06 between statuses.

| Landing token | Role | `AppColors` | Light | Dark |
| --- | --- | --- | --- | --- |
| `--bg` paper | canvas | `bgPrimary`, chat background | `#FBFAF7` | `#141210` (`--dark`) |
| `--card` | cards, composer | `bgSurface`, `inputBackground` | `#FFFFFF` | `#201D19` |
| `--tint` | raised fill, quiet wells | `bgElevated`, `surfaceSubtle`, chat `surfaceMuted` | `#F5F3EE` | `#2C2823` |
| – | pressed well | `surfacePressed` | `#ECE8DF` | `#3A3530` |
| `--ink` | text | `textPrimary` | `#161412` | `#F4F0EA` |
| `--soft` | secondary text | `textSecondary` / `textTertiary` | `#57514A` / `#6C665D` | `#C9C1B6` / `#A9A196` |
| `--line` | hairline | `borderSubtle` / `border` | `#EBE6DC` / `#DDD6C9` | `#2A2622` / `#3F3933` |
| `--faint` | decoration only | `borderStrong` (3:1) | `#857E73` | `#8A8378` |
| `--red` | accent (seal) | `accent` / `accentStrong` | `#BC3328`* / `#A92B21` | `#FF8F80` / `#FFB3A8` |
| `--red-soft` | selection wash | `accentSoft`, `surfaceActive` | `#FBEBE6` | `#3A1F1B` |

\* Light `accent` is a hair deeper than the seal's `#C8372B` (ΔE 0.026,
below a just-noticeable difference) so it still reads at 4.5:1 on pressed
paper. The seal artwork keeps `#C8372B` (`ZEN_BRAND_COLORS.vermilion`).
`--faint` (`#A29B90`) fails text contrast, so it is reserved for decoration.

**Chat.** Your messages sit in an ink bubble with paper text, as on the
landing; Brain's replies sit on the paper. In dark mode, sent messages use a
lit warm panel (`#35302A`) instead. Code, tables and attachments inside the
bubble use wells mixed from the bubble colour, never canvas fills.
`chrome.surfaceMuted` is the tint, decoupled from the sent bubble.

**Status.** These are the landing's status chips. Every status ships with a
glyph and a label, so hue is never the only signal.

| Status | Light | Dark | Notes |
| --- | --- | --- | --- |
| running | `#2C55C0` | `#8FB0FF` | the landing's "run" blue; never the accent |
| done / success | `#22703C` on `#E3F4E8` | `#7BD394` on `#12291A` | |
| blocked / warning | `#94600A` on `#FDF1DC` | `#F6C16B` on `#302412` | |
| failed / danger | `#A3194F` on `#FBE4EC` | `#F584C0` on `#3A1A2A` | rose crimson, ΔE > 0.1 from vermilion |
| unknown | `textTertiary` | `textTertiary` | |

The old sage, ink, clay and stone accents and their Settings picker are gone.
A stored legacy accent preference resolves to vermilion.

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

| Product state | Cat | Where | v1 |
| --- | --- | --- | --- |
| Brain idle, empty chat | Curled in the seal, breathing, three z's | Brain empty state | ✅ |
| Connecting or loading | In the seal, one eye open | Brain empty state (busy), status screen | ✅ |
| Brain's turn running | Out of the seal, walking in place | Working row, newest edge of the Brain timeline | ✅ |
| Delegated Work on Workers | Sitting, tail swaying, dispatch dots | Tail row, "Waiting on Workers · title" | ✅ |
| Work needs your input | Alert: ears up, wide eyes, vermilion ping | Tail row, "Needs you · title" | ✅ |
| Unread result | Loafing, happy eyes, a parcel at its paws | Tail row, "Brought something back · title" | ✅ |
| Offline | Asleep in a greyed seal | Brain status screen | ✅ |
| No computer paired | The empty bed (a ghost cat in the seal) | Brain status screen, "Give Brain a home" | ✅ |
| Failure | Ears back (rig `dangle`/ears) | Failure cards keep their glyph and label | later |
| Onboarding and pairing | Hops out of the seal on success | Onboarding | later |
| Pull to refresh / loading | Peeking seal | Lists | later |
| App icon, splash | The seal (`seal-icon.svg`) | Native assets | later (rename) |
| Notification icon | Seal silhouette, monochrome | Android small icon | later (rename) |
| Haptics | One soft tick when the cat brings a result | Delivered | later |

The v1 scope is the Brain screen only, because that is where Brain is. State
comes from `resolveBrainCatPresence` (`components/mewla/brainCatState.ts`), a
pure, tested function of the connection and `BrainCurrentWork`. A running
turn always wins through the Working row. Between turns the order is
attention, then a delivered result, then delegated Work, then idle. The
Brain screen opts in through `BrainCompanionContext`. Session chats never
provide that context, so they keep their usual rows and no cat.

There is never more than one cat on screen at a time. Play from the landing
(petting, toys) stays on the landing, because an assistant you rely on
shouldn't wander around your work.

## Rollout

Each slice can go to a Worker independently once this foundation merges.
Every slice must pass `cd app && bunx tsc --noEmit`, `bun test` and
`lint:design`, plus Android and iOS export. Visible changes need before and
after screenshots in light and dark.

1. **Shared components first.** Button, ListSection/ListRow, SegmentedControl,
   StatusPill, InlineNotice, ActionMenu, BottomSheetFrame, Toast, IconButton.
   Apply pill buttons, hairlines over shadows, the `title` role for sheet
   titles and the status chip styling. Everything downstream inherits from
   these.
2. **Navigation shell.** PrimaryDrawerShell, the app bar and drawer: the
   Brain/Sessions switch, the drawer list and the wordmark (after the rename).
   This slice could also hold a small presence dot for the cat.
3. **Sessions.** The Sessions list, Session rows and avatars, and the Session
   chat header. Chat content is already done by the tokens.
4. **Brain and Work cards.** BrainWorkEventCard, TaskNotificationCard and the
   Work screens. Other Workers edit these, so coordinate. Decide whether a
   "Ready" result keeps the vermilion wash or moves to a neutral card with a
   vermilion dot.
5. **Settings, Skills, Plugins, Providers, Stats, Calendar.** Mostly
   token-driven; check hard-coded colours (`rg '#[0-9A-Fa-f]{6}' app/components`).
6. **Onboarding and pairing.** Add the cat's "moves in" moment on a
   successful pair.
7. **Brand assets (with the rename).** App icon, adaptive icon, monochrome
   icon, splash and notification icon, all from `seal-icon.svg`, replacing
   `assets/branding/zen-*`.

Risky areas to watch:

- **Terminal.** The ANSI palette and two renderers. Keep it in its own slice
  with screenshots of real TUIs (vim, htop, git diff).
- **Markdown.** Native enriched-markdown styles and the mermaid theme read
  `surfaceMuted` and the accent. Check tables and code inside the ink bubble.
- **Native modules and fonts.** Font registration differs per platform. Check
  CJK fallback on real iOS and Android devices. Decide whether to drop the
  bundled Source Han Sans SC files, which would save about 33 MB.
- **Android elevation and translucency.** Keep in-flow cards flat.
