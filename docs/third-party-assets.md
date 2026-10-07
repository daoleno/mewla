# Third-party assets

Inclusion does not imply endorsement. Prefer verifiable upstream license text over guesswork. Assets without defensible provenance are listed in [release-blockers.md](release-blockers.md) and `release-blockers.json`.

## Fonts (bundled under `app/assets/fonts/`)

| In-app file                   | Upstream                                                                      | Evidence                                                                                                                      | License                                        |
| ----------------------------- | ----------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------- |
| `SourceHanSansSC-Regular.otf` | [adobe-fonts/source-han-sans](https://github.com/adobe-fonts/source-han-sans) | Name table: Adobe copyright + SIL OFL 1.1 URL (`nameID` 0/13/14)                                                              | SIL OFL 1.1                                    |
| `SourceHanSansSC-Medium.otf`  | same                                                                          | same                                                                                                                          | SIL OFL 1.1                                    |
| `MapleMono-CN-Regular.ttf`    | [subframe7536/maple-font](https://github.com/subframe7536/maple-font)         | Name table: Maple Mono Project Authors + SIL OFL 1.1; upstream `OFL.txt`                                                      | SIL OFL 1.1                                    |
| `MapleMono-CN-SemiBold.ttf`   | same                                                                          | same                                                                                                                          | SIL OFL 1.1                                    |
| `web/MapleMono-Subset-Regular.ttf`, `web/MapleMono-Subset-SemiBold.ttf` | Subsets of the two Maple Mono files above, for the web UI terminal | Name table kept, including the OFL notice (`nameID` 0/13/14); no Reserved Font Name is declared | SIL OFL 1.1 |
| `Inter-Regular.ttf`, `Inter-Medium.ttf`, `Inter-SemiBold.ttf` | [rsms/inter](https://github.com/rsms/inter), static instances served by Google Fonts (v20) | Name table: "Copyright 2016 The Inter Project Authors" + OFL URL (`nameID` 0/14) | SIL OFL 1.1 |
| `BricolageGrotesque-SemiBold.ttf`, `BricolageGrotesque-ExtraBold.ttf` | [ateliertriay/bricolage](https://github.com/ateliertriay/bricolage), static instances served by Google Fonts (v9) | Name table: "Copyright 2022 The Bricolage Grotesque Project Authors" + OFL URL (`nameID` 0/14) | SIL OFL 1.1 |

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
| `app/assets/branding/mewla-*.png` and `branding/source/*.svg`                 | First-party Mewla seal mark, drawn by `scripts/render-mewla-brand.ts` | Product assets; not third-party. See [TRADEMARKS.md](../TRADEMARKS.md). |
| `site/seal-icon.svg`, `site/sealcat.js`, `docs/assets/*`                      | First-party Mewla artwork and demo app screenshots | Product assets; not third-party.                                     |
| Product logos in `site/index.html`                                            | [LobeHub Icons](https://github.com/lobehub/lobe-icons) (AI brands), [Simple Icons](https://simpleicons.org) (the rest) | MIT and CC0; the marks stay their owners' trademarks. |
| `app/assets/theme/`                                                           | No bundled rasters                      | README only; do not add unattributed stock.                                |

Removed from the tree (unknown provenance): former `sky-meadow-ambient.webp` and `moonlit-meadow-ambient.webp`.

## Native dependency: Ghostty VT

| Artifact                                                       | Upstream                                                      | License                                                         | Notes                                                                                                                                                                                                                                                              |
| -------------------------------------------------------------- | ------------------------------------------------------------- | --------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `libghostty_vt.so` (Android) / `GhosttyVt.xcframework` (Apple) | [ghostty-org/ghostty](https://github.com/ghostty-org/ghostty) | [MIT](https://github.com/ghostty-org/ghostty/blob/main/LICENSE) | Built from the immutable pin in `app/modules/zen-terminal-vt/native.lock.json` via `scripts/build-libghostty.sh` (Android) and `scripts/build-libghostty-ios.sh` (iOS). Binaries are gitignored; both mobile bridges link the generated artifacts.                 |
| Notice source                                                  | same                                                          | MIT                                                             | `app/assets/notices/GHOSTTY-MIT.txt` embeds the pinned Ghostty `LICENSE` body (`license_sha256` in `native.lock.json`). Module pointer: `NOTICE.Ghostty`.                                                                                                          |
| Notice in APK                                                  | same                                                          | MIT                                                             | Expo plugin `withZenAndroidRelease` copies the notice to `android/app/src/main/assets/notices/GHOSTTY-MIT.txt` → APK path `assets/notices/GHOSTTY-MIT.txt`. Verify: `./scripts/verify-apk-notice.sh <apk>`.                                                        |
| Notice in iOS app / IPA                                        | same                                                          | MIT                                                             | Expo plugin `withZenIOSBuild` copies the notice into the Xcode app resources → bundle path `GHOSTTY-MIT.txt` at the app root (Xcode flattens ordinary files; see `native.lock.json` `ios.notice_bundle_path`). Verify: `./scripts/verify-ios-artifact.sh simulator | ipa <artifact>`. |

**ABI contract:** only `arm64-v8a` (device/sideload) and `x86_64` (emulator). See [android.md](internal/android-development.md).

**Redistribution:** APKs and iOS app bundles/IPAs must embed the MIT notice (paths above). Prebuilt `.so` / XCFramework archives should include an adjacent `GHOSTTY-MIT.txt` (written by the platform build scripts).

## npm / Go dependencies

Application and daemon library licenses are those of their respective packages (`bun.lock`, `daemon/go.mod`). This document focuses on **bundled fonts and vendored native binaries**, which are easy to miss in automated SCA.

## Bundled JavaScript: Mermaid

| Artifact | Upstream | License | Notes |
| --- | --- | --- | --- |
| `app/components/markdown/mermaidRuntimeSource.js` | [mermaid-js/mermaid](https://github.com/mermaid-js/mermaid) `11.6.0` | MIT | Offline `mermaid.min.js` for fenced Markdown flowcharts. SHA-256 in `mermaidRuntimeMeta.ts`. Runs in a sandboxed WebView; user diagrams cannot override `securityLevel`, load a CDN, or run callbacks. |
| Notice source | same | MIT | `app/assets/notices/MERMAID-MIT.txt`. |
