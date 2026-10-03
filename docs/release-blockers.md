# Release blockers

For the iOS build, signing, artifact, and Apple distribution gates, see [iOS CI and release automation](ios-ci-release.md).

Machine-readable companion: [`release-blockers.json`](release-blockers.json).

This file records release-readiness blockers and the evidence that resolved them. It is **not** an attribution source.

## Open blockers

### `github-plugin-publisher-registration`

- **Summary:** The artifact workflow requires a registered Zen GitHub publisher before building a new public release, but `release/plugin-publishers.json` still has a null GitHub `client_id`.
- **Acceptance:** The approved owner `daoleno` supplies the public Client ID of the Zen OAuth app with Device Flow enabled; commit that public value to the manifest and pass `python3 scripts/plugin-publisher-flags.py --require github` before dispatching release preparation.
- **Evidence:** `.github/workflows/release-artifacts.yml` runs that validation before tag/identity validation. With the current manifest it reports `github: Zen publisher registration is not configured; cannot publish`.
- **Handoff:** Follow [Publisher setup](plugins-publisher-setup.md#github-approved-owner-and-concrete-registration-handoff) in the owner's browser. The missing value is public configuration; release publication is already authorized. Do not distribute a client secret or substitute another application's identity.

## Resolved

### `android-native-terminal-artifacts` (resolved 2026-08-24)

- **Summary:** `libghostty_vt.so` is required for the Android terminal and is gitignored; a bare clone still has no terminal binaries until build or release artifacts exist.
- **Acceptance:** Documented prebuilt APK/libs with MIT notice, or a reproducible CI artifact pipeline.
- **Resolution evidence:** [`v0.1.0-beta.22`](https://github.com/daoleno/zen/releases/tag/v0.1.0-beta.22) publishes the signed arm64 APK, three daemon archives, `SHA256SUMS`, and the signed update manifest. The release workflow verifies ABI, native imports, notice packaging, package identity, signing certificate, checksums, and manifest signature before publication.
- **User/CI commands:** `./scripts/verify-libghostty.sh --contract`; `./scripts/build-libghostty.sh` then `./scripts/verify-libghostty.sh --release`; APK `./scripts/android-release-apk.sh` + `./scripts/verify-apk-notice.sh <apk>`.

### `ios-distribution-artifacts` (resolved 2026-08-24)

- **Summary:** The iOS source build and Simulator runtime path work. `GhosttyVt.xcframework` remains generated/gitignored. A signed Preview IPA can be uploaded to App Store Connect via the protected release workflow, but that is not the same as public TestFlight/App Store installability.
- **Acceptance:** Reproducible CI produces and verifies the pinned XCFramework, packages the Ghostty MIT notice into the app bundle, archives/signs the app, and publishes a supported installation path with checksummed artifacts where applicable.
- **Resolution evidence:** the protected `v0.1.0-beta.22` iOS workflow completed archive, identity, signature, upload, processing, public TestFlight-group attachment, and Beta App Review submission. The supported public Preview URL is <https://testflight.apple.com/join/rTKCDzMt>.
- **User/CI commands:** `bun run native:build:ios`; `bun run native:verify:ios`; Expo prebuild/Pods; the `CI` macOS job; or a protected manual `iOS signed release` dispatch.

### `theme-image-provenance-unknown` (resolved)

- All unknown `app/assets/theme/*.webp` rasters removed.
- `SkyNatureBackdrop` is first-party gradients only (no stock images).

## Non-blockers recorded for honesty

- Fonts (Source Han Sans SC, Maple Mono CN): upstream OFL evidence recorded in `third-party-assets.md`.
- Ghostty: MIT; redistribution of built `.so`/APK and iOS app/IPA needs notice packaging (Android + iOS verifiers).
