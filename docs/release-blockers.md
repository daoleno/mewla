# Release blockers

For the iOS build, signing, artifact, and Apple distribution gates, see [iOS CI and release automation](ios-ci-release.md).

This file records release-readiness blockers and the evidence that resolved them. It is **not** an attribution source.

## Open blockers

None.

## Resolved

### `ios-distribution-artifacts` (resolved 2026-10-15)

- **Summary:** Mewla `0.2.4 (42)` was waiting for Apple Beta App Review; the release automation had completed, and public installability remained an Apple review gate.
- **Acceptance:** CI verifies the XCFramework and Ghostty notice, signs/archives Mewla, and publishes a publicly installable TestFlight/App Store path; an IPA upload alone is insufficient.
- **Resolution evidence:** Apple approved the Preview build and the public TestFlight path is live. iOS `v0.2.14`, build `52` installs from [Mewla TestFlight](https://testflight.apple.com/join/nMQheDCE); see [iOS CI](ios-ci-release.md) and the [0.2.14 release](releases/v0.2.14.md).

### `github-plugin-publisher-registration` (resolved 2026-10-04)

- **Summary:** The artifact workflow requires a registered Mewla GitHub publisher before building a public release.
- **Resolution evidence:** commit `9a8988cd` commits the public Client ID of the `daoleno`-owned Mewla OAuth app to `release/plugin-publishers.json`; `python3 scripts/plugin-publisher-flags.py --require github` passes.
- **Handoff (historical):** [Publisher setup](internal/plugins-publisher-setup.md#github-approved-owner-and-concrete-registration-handoff).

### `android-native-terminal-artifacts` (resolved 2026-08-24)

- **Summary:** `libghostty_vt.so` is required for the Android terminal and is gitignored; a bare clone still has no terminal binaries until build or release artifacts exist.
- **Acceptance:** Documented prebuilt APK/libs with MIT notice, or a reproducible CI artifact pipeline.
- **Resolution evidence:** [`v0.1.0-beta.22`](https://github.com/daoleno/mewla/releases/tag/v0.1.0-beta.22) publishes the signed arm64 APK, three daemon archives, `SHA256SUMS`, and the signed update manifest. The release workflow verifies ABI, native imports, notice packaging, package identity, signing certificate, checksums, and manifest signature before publication.
- **User/CI commands:** `./scripts/verify-libghostty.sh --contract`; `./scripts/build-libghostty.sh` then `./scripts/verify-libghostty.sh --release`; APK `./scripts/android-release-apk.sh` + `./scripts/verify-apk-notice.sh <apk>`.

### `theme-image-provenance-unknown` (resolved)

- All unknown `app/assets/theme/*.webp` rasters removed.

## Non-blockers recorded for honesty

- Fonts (Maple Mono CN, Inter, Bricolage Grotesque): upstream OFL evidence recorded in `third-party-assets.md`.
- Ghostty: MIT; redistribution of built `.so`/APK and iOS app/IPA needs notice packaging (Android + iOS verifiers).
