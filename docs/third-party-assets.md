# Third-party assets

Inclusion does not imply endorsement. Prefer verifiable upstream license text over guesswork. Assets without defensible provenance are listed in [release-blockers.md](release-blockers.md).

## Fonts (bundled under `app/assets/fonts/`)

| In-app file                   | Upstream                                                                      | Evidence                                                                                                                      | License                                        |
| ----------------------------- | ----------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------- |
| `MapleMono-CN-Regular.ttf`    | [subframe7536/maple-font](https://github.com/subframe7536/maple-font)         | v7.900, subset to the common Han set (see below). Name table: Maple Mono Project Authors + SIL OFL 1.1; upstream `OFL.txt`                                                      | SIL OFL 1.1                                    |
| `MapleMono-CN-SemiBold.ttf`   | same                                                                          | same                                                                                                                          | SIL OFL 1.1                                    |
| `web/MapleMono-Subset-Regular.ttf`, `web/MapleMono-Subset-SemiBold.ttf` | Subsets of the two Maple Mono files above, for the web UI terminal | Name table kept, including the OFL notice (`nameID` 0/13/14); no Reserved Font Name is declared | SIL OFL 1.1 |
| `Inter-Regular.ttf`, `Inter-Medium.ttf`, `Inter-SemiBold.ttf` | [rsms/inter](https://github.com/rsms/inter), static instances served by Google Fonts (v20) | Name table: "Copyright 2016 The Inter Project Authors" + OFL URL (`nameID` 0/14) | SIL OFL 1.1 |
| `BricolageGrotesque-SemiBold.ttf`, `BricolageGrotesque-ExtraBold.ttf` | [ateliertriay/bricolage](https://github.com/ateliertriay/bricolage), static instances served by Google Fonts (v9) | Name table: "Copyright 2022 The Bricolage Grotesque Project Authors" + OFL URL (`nameID` 0/14) | SIL OFL 1.1 |

The native Maple Mono CN files keep every upstream glyph except Han
ideographs outside GB2312 and Big5 level 1 (10,591 of 22,731 codepoints, about
7.7 MB each instead of 18.6 MB). Name table, metrics, hinting and layout
features are unchanged, and no Reserved Font Name is declared. To regenerate
them from the upstream v7.900 TTFs (SHA-256 pinned in the script; git history
before the subset holds the same files), run from the repository root:

```bash
python3 scripts/subset-maple-mono-cn.py <upstream-dir> app/assets/fonts
```

The web subsets keep only Latin, punctuation, arrows, technical symbols, box
drawing, shapes, dingbats, braille, and Powerline glyphs, with no layout
features. To regenerate them from `app/`, run this for `Regular` and `SemiBold`:

```bash
pyftsubset assets/fonts/MapleMono-CN-Regular.ttf \
  --unicodes='U+0020-007E,U+00A0-017F,U+2000-206F,U+20A0-20CF,U+2100-214F,U+2190-23FF,U+2460-24FF,U+2500-27BF,U+2800-28FF,U+E0A0-E0D7,U+FFFD' \
  --layout-features='' --name-IDs='*' --name-legacy --name-languages='*' \
  --notdef-outline --drop-tables+=meta \
  --output-file=assets/fonts/web/MapleMono-Subset-Regular.ttf
```

OFL redistribution still expects copyright/license notice availability to recipients; keep this file (and upstream LICENSE links) with source releases.

## Images / branding

| Path                                                                          | Provenance                              | License notes                                                              |
| ----------------------------------------------------------------------------- | --------------------------------------- | -------------------------------------------------------------------------- |
| `app/assets/pets/*`, `site/pets/*`                                            | First-party Mewla pets: ten characters and their state clips, generated for Mewla with OpenAI image models, then sliced, matted and encoded by our own scripts | Product assets; not third-party. See [Pet and brand art](#pet-and-brand-art). |
| `app/assets/branding/mewla-*`, `site/mark.png`, `site/favicon*`, `site/apple-touch-icon.png`, `site/og-card.png` | First-party Mewla mark: the default pet (`p05`) in the vermilion seal, generated the same way | Product assets; not third-party. See [TRADEMARKS.md](../TRADEMARKS.md). |
| `docs/assets/*`                                                               | First-party drawings (`scripts/site-svg/`, embedding the mark and the default pet) and demo app screenshots | Product assets; not third-party.                                     |
| Product logos in `site/index.html` and `app/components/plugins/ServiceMarks.tsx` | [LobeHub Icons](https://github.com/lobehub/lobe-icons) (AI brands), [Simple Icons](https://simpleicons.org) (the rest) | MIT and CC0; the marks stay their owners' trademarks. |
| `app/assets/theme/`                                                           | No bundled rasters                      | README only; do not add unattributed stock.                                |

Removed from the tree (unknown provenance): former `sky-meadow-ambient.webp` and `moonlit-meadow-ambient.webp`.

### Pet and brand art

- **Generated, not traced.** The ten character sheets were generated with gpt-image-2 (through Codex `image_gen`). Every pet's clips are drawn in Codex with its image generation (`$imagegen`) and OpenAI's `hatch-pet` skill ([openai/skills](https://github.com/openai/skills), `skills/.curated/hatch-pet`, Apache-2.0): a few candidate base images per pet from its sheet, one chosen base, then one horizontal strip per row on a flat blue chroma-key background, grounded in that base and a slot guide. Each pet has the skill's nine rows (idle, running right and left, waving, jumping, failed, waiting, running, review) and four home rows drawn with the cat inside its seal (asleep, hopping out, hopping back, peeking over); the seal is drawn as part of each whole drawing, never composited. The empty seal is its own generated drawing. Each pet also has a 12-frame walk of small even steps, drawn as whole drawings (one 12-slot strip; for Nori six keys and six in-betweens in a second strip, interleaved). Every pet also has play rows for the landing's games (stalk, wiggle, pounce, swat, rear up, eat, run), drawn for each pet from its own base with the default pet's (`p05`) play strip as a pose guide; swat, rear up and eat draw no toy or treat, since the landing draws its own. Where a pet's running-left row had no side-specific detail it is the right row mirrored frame by frame. The "catonchair" animation was used only as a style reference for the look of a hand-drawn crayon cat; none of its frames or pixels were traced or copied into this art.
- **Processing.** The hatch-pet extraction keys out the background and cuts each row with one shared viewport, and removes the blue fringe. The four home rows and the empty seal are registered as whole frames, by one uniform scale and shift each, to one shared seal ring. Converting to our pack format (asset lab `build_codex_packs.py`) moves nothing inside a frame: each row is scaled and placed as whole frames, so its cat matches idle's size and its ground sits on the canvas ground line. The standing rows draw the cat at the size of the earlier packs; a row that would leave the canvas at that size is scaled down on its own. The home rows and the empty seal share one scale, the largest that keeps the ring on the canvas. The source draws the ring large next to the cat, so the hops zoom at their seam: over the three frames nearest it, each whole frame's scale ramps until the cat matches the standing cat, with its feet where the standing cat's are. The ring grows past the 256 px canvas there, which cuts it in the app's clips; the hops are also written whole on a padded canvas for the landing. The app states map to rows: idle asleep in the seal, offline the same desaturated, waking hopping out, going back hopping back, homeless the empty seal, delegating the idle row, working the in-place running (work) row, attention waiting, delivered jumping. Frame holds follow the skill's row timings. `scripts/import-pets.py` then re-encodes each pack for the app, and writes sprite strips of every pet's clips for the landing (`--site` rewrites only those), with how its walking clips meet the floor (the measured planted-paw travel per frame, contact and bob) so the landing moves the pet by the drawings' own stride. No frame was redrawn by hand.
- **Pack format.** `pets/<id>/meta.json` (`id`, `name.en`/`name.zh`, `default`, and `actions` keyed by Brain state plus `going_back` and any play clips, each with `file`, `loop`, `frames`, per-frame `durations` in ms and `anchor`; plus the source `row`), one animated WebP per action on a shared 256 px canvas, the registered frames as PNGs in `frames/<action>/`, and a `portrait.png`. The two hops are also in `frames-wide/<action>/` on a canvas padded by the action's `wide` px on every side, so their zoom isn't cut. `pets/index.json` lists the pets in order.

## Native dependency: Ghostty VT

| Artifact                                                       | Upstream                                                      | License                                                         | Notes                                                                                                                                                                                                                                                              |
| -------------------------------------------------------------- | ------------------------------------------------------------- | --------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `libghostty_vt.so` (Android) / `GhosttyVt.xcframework` (Apple) | [ghostty-org/ghostty](https://github.com/ghostty-org/ghostty) | [MIT](https://github.com/ghostty-org/ghostty/blob/main/LICENSE) | Built from the immutable pin in `app/modules/terminal-vt/native.lock.json` via `scripts/build-libghostty.sh` (Android) and `scripts/build-libghostty-ios.sh` (iOS). Binaries are gitignored; both mobile bridges link the generated artifacts.                 |
| Notice source                                                  | same                                                          | MIT                                                             | `app/assets/notices/GHOSTTY-MIT.txt` embeds the pinned Ghostty `LICENSE` body (`license_sha256` in `native.lock.json`). Module pointer: `NOTICE.Ghostty`.                                                                                                          |
| Notice in APK                                                  | same                                                          | MIT                                                             | Expo plugin `withAndroidRelease` copies the notice to `android/app/src/main/assets/notices/GHOSTTY-MIT.txt` → APK path `assets/notices/GHOSTTY-MIT.txt`. Verify: `./scripts/verify-apk-notice.sh <apk>`.                                                        |
| Notice in iOS app / IPA                                        | same                                                          | MIT                                                             | Expo plugin `withIOSBuild` copies the notice into the Xcode app resources → bundle path `GHOSTTY-MIT.txt` at the app root (Xcode flattens ordinary files; see `native.lock.json` `ios.notice_bundle_path`). Verify: `./scripts/verify-ios-artifact.sh simulator | ipa <artifact>`. |

**ABI contract:** only `arm64-v8a` (device/sideload) and `x86_64` (emulator). See [android.md](internal/android-development.md).

**Redistribution:** APKs and iOS app bundles/IPAs must embed the MIT notice (paths above). Prebuilt `.so` / XCFramework archives should include an adjacent `GHOSTTY-MIT.txt` (written by the platform build scripts).

## npm / Go dependencies

Application and daemon library licenses are those of their respective packages (`bun.lock`, `daemon/go.mod`). This document focuses on **bundled fonts and vendored native binaries**, which are easy to miss in automated SCA.

## Bundled JavaScript: Mermaid

| Artifact | Upstream | License | Notes |
| --- | --- | --- | --- |
| `app/components/markdown/mermaidRuntimeSource.js` | [mermaid-js/mermaid](https://github.com/mermaid-js/mermaid) `11.6.0` | MIT | Offline `mermaid.min.js` for fenced Markdown flowcharts. SHA-256 in `mermaidRuntimeMeta.ts`. Runs in a sandboxed WebView; user diagrams cannot override `securityLevel`, load a CDN, or run callbacks. |
| Notice source | same | MIT | `app/assets/notices/MERMAID-MIT.txt`. |
