# Third-party assets

Inclusion does not imply endorsement. Prefer verifiable upstream license text over guesswork. Assets without defensible provenance are listed in [release-blockers.md](release-blockers.md).

## Fonts

NSH draws the UI with the platform's faces. The terminal bundles **Maple Mono NF 7.9** ([subframe7536/maple-font](https://github.com/subframe7536/maple-font), SIL OFL 1.1, no Reserved Font Name), Regular, SemiBold, Italic and SemiBold Italic, in `client/native/terminal/fonts/` with its `OFL.txt`. `scripts/subset-terminal-font.py` makes the four files from the sha256-pinned release zip (`MapleMono-NF.zip`): CJK, hinting, ligatures and alternates are removed; Regular keeps the Nerd Fonts icons Maple patches in, SemiBold is Latin only, and the italics keep only Latin, Latin-1, Latin Extended-A, Greek, Cyrillic, combining marks, punctuation, currency and arrows (about 70 KB each; other characters in italic text are the upright face slanted). The OFL text is also in the in-app license list (`texts/MapleMono-OFL.txt`). The phones draw with it natively; the web terminal (xterm.js) loads the same two files, which `scripts/build-web-ui.sh` copies beside its page (`/terminal/*.ttf`).

## Images / branding

| Path                                                                          | Provenance                              | License notes                                                              |
| ----------------------------------------------------------------------------- | --------------------------------------- | -------------------------------------------------------------------------- |
| `client/ui/more/assets/pets/*`, `site/pets/*`, `scripts/site-svg/pet-attention.webp` | First-party Mewla pets: ten characters and their state clips, generated for Mewla with OpenAI image models, then sliced, matted and encoded by our own scripts | Product assets; not third-party. See [Pet and brand art](#pet-and-brand-art). |
| `client/ios/Assets.xcassets/AppIcon.appiconset/*`, the client's Android launcher icons, `site/mark.png`, `site/favicon*`, `site/apple-touch-icon.png`, `site/og-card.png` | First-party Mewla mark: the default pet (`p05`) in the vermilion seal, generated the same way | Product assets; not third-party. See [TRADEMARKS.md](../TRADEMARKS.md). |
| `docs/assets/*`                                                               | First-party drawings (`scripts/site-svg/`, embedding the mark and the default pet) and demo app screenshots | Product assets; not third-party.                                     |
| Agent logos in `client/ui/icons/agents/` (`AgentIcon.tsx` names each source) | [LobeHub Icons](https://github.com/lobehub/lobe-icons) (Claude, Codex, Grok, OpenCode, DeepSeek); the Cursor and [Pi](https://pi.dev) marks | MIT; listed in Settings > Open-source licenses; the marks stay their owners' trademarks. |
| Product logos in `site/index.html` and `client/core/extensions/serviceMarkPaths.ts` | [LobeHub Icons](https://github.com/lobehub/lobe-icons) (AI brands), [Simple Icons](https://simpleicons.org) (the rest) | MIT and CC0; the marks stay their owners' trademarks. |
| Hyperliquid mark in `site/index.html` and `client/core/extensions/serviceMarkPaths.ts` | The symbol (first path) of the logo SVG inlined on [hyperliquid.xyz](https://hyperliquid.xyz); Simple Icons has no Hyperliquid glyph | Hyperliquid's own mark, shown only to name its plugin; it stays Hyperliquid's trademark. |

Removed from the tree (unknown provenance): former `sky-meadow-ambient.webp` and `moonlit-meadow-ambient.webp`.

### Pet and brand art

- **Generated, not traced.** The ten character sheets were generated with gpt-image-2 (through Codex `image_gen`). Every pet's clips are drawn in Codex with its image generation (`$imagegen`) and OpenAI's `hatch-pet` skill ([openai/skills](https://github.com/openai/skills), `skills/.curated/hatch-pet`, Apache-2.0): a few candidate base images per pet from its sheet, one chosen base, then one horizontal strip per row on a flat blue chroma-key background, grounded in that base and a slot guide. Each pet has the skill's nine rows (idle, running right and left, waving, jumping, failed, waiting, running, review) and four home rows drawn with the cat inside its seal (asleep, hopping out, hopping back, peeking over); the seal is drawn as part of each whole drawing, never composited. The empty seal is its own generated drawing. Each pet also has a 12-frame walk of small even steps, drawn as whole drawings (one 12-slot strip; for Nori six keys and six in-betweens in a second strip, interleaved). Every pet also has play rows for the landing's games (stalk, wiggle, pounce, swat, rear up, eat, run), drawn for each pet from its own base with the default pet's (`p05`) play strip as a pose guide; swat, rear up and eat draw no toy or treat, since the landing draws its own. Where a pet's running-left row had no side-specific detail it is the right row mirrored frame by frame. The "catonchair" animation was used only as a style reference for the look of a hand-drawn crayon cat; none of its frames or pixels were traced or copied into this art.
- **Processing.** The hatch-pet extraction keys out the background and cuts each row with one shared viewport, and removes the blue fringe. The four home rows and the empty seal are registered as whole frames, by one uniform scale and shift each, to one shared seal ring. Converting to our pack format (asset lab `build_codex_packs.py`) moves nothing inside a frame: each row is scaled and placed as whole frames, so its cat matches idle's size and its ground sits on the canvas ground line. The standing rows draw the cat at the size of the earlier packs; a row that would leave the canvas at that size is scaled down on its own. The home rows and the empty seal share one scale, the largest that keeps the ring on the canvas. The source draws the ring large next to the cat, so the hops zoom at their seam: over the three frames nearest it, each whole frame's scale ramps until the cat matches the standing cat, with its feet where the standing cat's are. The ring grows past the 256 px canvas there; the hops are also written whole on a padded canvas for the landing. The Brain states map to rows: idle asleep in the seal, offline the same desaturated, waking hopping out, going back hopping back, homeless the empty seal, delegating the idle row, working the in-place running (work) row, attention waiting, delivered jumping. Frame holds follow the skill's row timings. `scripts/import-pets.py` then re-encodes each pack's clips and writes sprite strips of every pet's clips for the landing, with how its walking clips meet the floor (the measured planted-paw travel per frame, contact and bob) so the landing moves the pet by the drawings' own stride. No frame was redrawn by hand.
- **Pack format.** `pets/<id>/meta.json` (`id`, `name.en`/`name.zh`, `default`, and `actions` keyed by Brain state plus `going_back` and any play clips, each with `file`, `loop`, `frames`, per-frame `durations` in ms and `anchor`; plus the source `row`), one animated WebP per action on a shared 256 px canvas, the registered frames as PNGs in `frames/<action>/`, and a `portrait.png`. The two hops are also in `frames-wide/<action>/` on a canvas padded by the action's `wide` px on every side, so their zoom isn't cut. `pets/index.json` lists the pets in order.

## Native dependency: Ghostty VT

| Artifact                                                       | Upstream                                                      | License                                                         | Notes                                                                                                                                                                                                                                                              |
| -------------------------------------------------------------- | ------------------------------------------------------------- | --------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `libghostty_vt.so` (Android) / `GhosttyVt.xcframework` (Apple) / `libghostty-vt.a` (daemon session host) | [ghostty-org/ghostty](https://github.com/ghostty-org/ghostty) | [MIT](https://github.com/ghostty-org/ghostty/blob/main/LICENSE) | Built from the sha256-pinned, minisign-signed libghostty-vt `vt-1.0.1` release tarballs recorded in `native/terminal-vt/native.lock.json` via `scripts/build-libghostty.sh` (Android), `scripts/build-libghostty-ios.sh` (iOS) and `scripts/build-libghostty-desktop.sh` (the daemon's session host, linked only with the `ghosttyvt` build tag). Binaries are gitignored; both mobile bridges link the generated artifacts.                 |
| Notice source                                                  | same                                                          | MIT                                                             | `client/ui/more/settings/licenses/texts/Ghostty-MIT.txt` embeds the pinned Ghostty `LICENSE` body (`license_sha256` in `native.lock.json`). Pointer: `native/terminal-vt/NOTICE.Ghostty`. The texts of the code libghostty-vt compiles in (Highway, simdutf, uucode, Wuffs, translate-c) are `texts/libghostty-*.txt` beside it. |
| Notice in APK                                                  | same                                                          | MIT                                                             | The `ghosttyNotice` task of `client/android/app/build.gradle.kts` adds the notice to the APK's assets → APK path `assets/notices/GHOSTTY-MIT.txt`. Verify: `./scripts/verify-apk-notice.sh <apk>`.                                                        |
| Notice in iOS app / IPA                                        | same                                                          | MIT                                                             | `client/ios/project.yml`'s "Bundle the Ghostty notice" step copies the notice to the app bundle root as `GHOSTTY-MIT.txt` (see `native.lock.json` `ios.notice_bundle_path`). Verify: `./scripts/verify-ios-artifact.sh simulator | ipa <artifact>`. |

**ABI contract:** only `arm64-v8a` (device/sideload) and `x86_64` (emulator). See [android.md](internal/android-development.md).

**Redistribution:** APKs and iOS app bundles/IPAs must embed the MIT notice (paths above). Prebuilt `.so` / XCFramework archives should include an adjacent `GHOSTTY-MIT.txt` (written by the platform build scripts).

## Bundled JavaScript: noVNC

| Artifact | Upstream | License | Notes |
| --- | --- | --- | --- |
| `client/ui/more/screen/viewerSource.ts` | [novnc/noVNC](https://github.com/novnc/noVNC) `1.7.0` (includes pako) | MPL-2.0 (pako: MIT) | The Screen viewer's RFB client, bundled with `client/ui/more/screen/runtime/` by `bun client/ui/more/screen/runtime/generate.mjs` (`--check` reports stale output) from the `@novnc/novnc` package pinned in the root `package.json`. The only change to noVNC source is that script's rewrite of one top-level `await` in `core/util/browser.js`. SHA-256 in `viewerSource.ts`. Opens no connection of its own. |
| Notice source | same | MPL-2.0, MIT | `client/ui/more/settings/licenses/texts/noVNC-MPL-2.0.txt`. |

## Bundled JavaScript: xterm.js

| Artifact | Upstream | License | Notes |
| --- | --- | --- | --- |
| `client/ui/web/terminal/` (vendored) | [xtermjs/xterm.js](https://github.com/xtermjs/xterm.js) `6.0.0` | MIT | Draws a Session's terminal in the browser; served by the computer beside the web app. Notice: `client/ui/more/settings/licenses/texts/xterm-MIT.txt`. |

## In-app license list

Settings → Open-source licenses shows `client/ui/more/settings/licenses/licenses.json`,
written by `python3 client/ui/more/settings/licenses/generate.py` from the list
kept in that script (the NSH runtime, the code bundled into the app, and the
native libraries of `client/android/app/build.gradle.kts` and
`client/ios/project.yml`) and the license texts in `texts/` beside it. CI's
`client` job runs it with `--check`, which fails when `licenses.json` is stale.
After adding a dependency, add it to `generate.py` and regenerate.

## npm / Go dependencies

Daemon library licenses are those of their packages (`daemon/go.mod`). The root `package.json` / `bun.lock` hold only noVNC, which the Screen viewer bundles. This document focuses on **bundled code and vendored native binaries**, which are easy to miss in automated SCA.

## Code derived from other projects

| In the repository | Upstream | License |
| --- | --- | --- |
| `client/android/phone/src/main/java/com/daoleno/mewla/phone/adb/` (Wireless debugging pairing with SPAKE2, the adb TLS connection, mDNS port discovery) | [wangzan101/mika](https://github.com/wangzan101/mika) `core/adb` and `cap/phone/shell/adb`, commit `7171a707`. Translated comments, renamed package and identity strings; the Android key storage and mDNS finder are adapted. | Apache License 2.0; the notice is in `NOTICE` |
