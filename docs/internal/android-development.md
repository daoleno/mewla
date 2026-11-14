# Android app

The Android app is the NSH client's Gradle host in `client/android` (see
[client/README.md](../../client/README.md)): the `ui/` bundle built by `nsh build`,
native views, the Keystore secrets, the camera reader and the terminal's
libghostty-vt.

## Install the APK

Open [GitHub Releases](https://github.com/daoleno/mewla/releases) and download the newest `mewla-android-arm64-v*.apk` together with `SHA256SUMS`.

The APK supports 64-bit ARM Android devices (`arm64-v8a`). It does not support x86 phones or 32-bit ARM devices. iOS uses a separate source build; see [iOS app](ios-development.md).

Before installing, download `SHA256SUMS` from the same release and verify the APK:

```bash
sha256sum -c SHA256SUMS --ignore-missing
```

The official release certificate SHA-256 fingerprint is:

```text
C2:FC:5B:09:B3:86:92:EE:70:59:71:1F:E7:ED:B8:79:
4C:E3:65:FE:1C:7A:06:AB:95:4E:5D:D1:BD:CD:A4:FD
```

Android will ask you to allow installation from the browser or file manager you used. A Play Protect warning is possible because Mewla is distributed outside Play Store.

After installation, follow [Connect and pair](../connect-and-pair.md): use `mewla --lan` on a trusted private network or keep bare `mewla` behind an HTTPS endpoint, then scan or import the generated pairing code in Settings.

## Architecture / ABI contract

Canonical machine-readable contract:

[`native/terminal-vt/native.lock.json`](../../native/terminal-vt/native.lock.json)

| Android ABI | Zig target              | Role                              | Sideload required |
| ----------- | ----------------------- | --------------------------------- | ----------------- |
| `arm64-v8a` | `aarch64-linux-android` | Physical devices / release APK    | **Yes**           |
| `x86_64`    | `x86_64-linux-android`  | Emulator / host-side native debug | No                |

**Unsupported (invalid for Mewla terminal):** `armeabi-v7a`, `x86`, and any other ABI.

Invariants:

1. Only the ABIs listed above may appear under `native/terminal-vt/libs/android/`.
2. Release APKs are **arm64-v8a only** (`-Pmewla.abis=arm64-v8a`).
3. `libghostty_vt.so` and the Ghostty C headers are **gitignored**. A bare clone has no terminal binaries until it builds them or uses release artifacts.
4. The toolchain and source pins live in `native.lock.json` (Zig version, the signed Ghostty release tarballs and their digests).
5. Redistributed APKs embed Ghostty MIT at `assets/notices/GHOSTTY-MIT.txt` (source: `client/ui/more/settings/licenses/texts/Ghostty-MIT.txt`), packaged by the `ghosttyNotice` task of `client/android/app/build.gradle.kts`. Verify with `./scripts/verify-apk-notice.sh <apk>`.
6. Release-grade native builds come only from the signed libghostty-vt release tarball pinned in the lock, taken unmodified (`release_grade: true`, `provenance: signed_release` in `build-manifest.json`). There is no developer override; `./scripts/verify-libghostty.sh --release` checks the manifest against the lock.
7. Raw and APK-packaged `libghostty_vt.so` artifacts must not import symbols listed in `android.forbidden_undefined_symbols`. `verify-libghostty.sh` and the APK verifiers enforce this before distribution.

Wiring (source of truth for packaging):

- `client/android/app/build.gradle.kts`: `mewla.ghostty` (default `native/terminal-vt`) and `mewla.abis` (default `arm64-v8a,x86_64`); fails when a selected ABI's `libghostty_vt.so` is missing.
- `client/android/app/CMakeLists.txt`: imports `libs/android/${ANDROID_ABI}/libghostty_vt.so` and includes `include/` (the headers) from `MEWLA_GHOSTTY`.
- `./scripts/verify-libghostty.sh --contract` checks this wiring, the lock and the notice (CI's native-contract job).

## Prerequisites

- NSH: `client/scripts/fetch-nsh.sh` (pinned, into `~/.nsh`)
- JDK 17 and the Android SDK, NDK 27.1 and CMake 3.22 (`.github/actions/android-native` lists the packages)
- For the native terminal: the signed **libghostty-vt `vt-1.0.1` release tarballs** (downloaded on demand into `~/.cache/mewla/ghostty`, sha256-pinned in the lock, minisign-checked when `minisign` or Python `cryptography` is available) and Zig **0.17.0** (see lock). `build-libghostty.sh` uses a matching `zig` from `PATH` or downloads the pinned, checksum-verified one into `.cache/mewla-tools` (`MEWLA_TOOL_CACHE`). The build is offline (`zig build --system` with the dependency tarball); `MEWLA_GHOSTTY_RELEASE_DIR` points it at pre-downloaded tarballs

## Day-to-day workflow

- **Hot reload:** `client/scripts/dev-android.sh` builds the preview debug build with this host's `nsh dev` server (`-Pmewla.devServer`, from `DEV_HOST` or else the private LAN address `client/scripts/lan-address.sh` picks and prints: tunnels, bridges and virtual interfaces are skipped, so a proxy's TUN is never taken, and with no LAN address it stops before building; no address is in the source), installs it on the one device `adb` sees and serves client/. Mewla Dev is live from its icon: the first open on a server asks once (Load) and remembers it; later opens connect straight away when the server answers within two seconds, else start on the built-in bundle without a dialog and reopen on the server when the app comes to the front and it answers. The start link `mewla-dev://start?server=ws://…` points it at another server on this network, always confirmed first, and is remembered the same way. Release builds have none of this (src/debug only; `scripts/tests/test_android_dev_start_link.py`).
- **A build to install:** `cd client/android && ./gradlew assemblePreviewRelease -Pmewla.abis=arm64-v8a`. The preview identity (`com.daoleno.mewla.preview`, "Mewla Preview") installs beside the store app.
- **Checks:** `cd client && bun test core`, then `cd client/ui && ~/.nsh/bin/nsh check && ~/.nsh/bin/nsh test`.

## Pairing on device

1. Start `mewla --lan` for trusted private-network access, or start bare `mewla` behind an HTTPS endpoint.
2. In another terminal, run the complete `mewla pair` command Mewla or your HTTPS setup provides.
3. Open Settings in the app.
4. Paste the `mewla://...` link, scan the QR, or import a QR photo.

The app allows cleartext HTTP (`usesCleartextTraffic` in `client/android/app/src/main/AndroidManifest.xml`) so dynamic LAN and Tailscale IPs work. Keeping `http://` to private addresses is the daemon address book's job (`addressbook.Normalize`). Use HTTP only on a trusted private network; use an HTTPS endpoint on shared or untrusted networks.

Remote push works in official builds: `client/android/app/google-services.json` is the public Firebase client config for `com.daoleno.mewla`, which `build.gradle.kts` turns into the resources Firebase reads. FCM delivery needs the FCM V1 key uploaded to Mewla's Expo project (see [Notifications](../notifications.md)). A variant whose package has no entry in the file builds without push (the preview identity); a production release build without its entry fails (`checkProductionReleasePush`). A fork with its own package replaces `google-services.json` and uploads its own key.

## Native terminal library

Android and iOS share the [terminal row-update pipeline](terminal-rendering.md).

`native/terminal-vt/libs/android/*/libghostty_vt.so` is **gitignored**. Without those binaries the Gradle build stops and names the missing ABI.

### Build (reproducible path)

```bash
# Downloads the pinned signed release tarballs into ~/.cache/mewla/ghostty;
# MEWLA_GHOSTTY_RELEASE_DIR=/dir/with/tarballs builds offline.
./scripts/build-libghostty.sh

# Device ABI only:
./scripts/build-libghostty.sh --abis arm64-v8a

# Verify ABI/ELF (+ checksums if present)
./scripts/verify-libghostty.sh
./scripts/verify-libghostty.sh --release    # arm64 only
./scripts/verify-libghostty.sh --contract   # CI: lock + notice + wiring, no .so
```

The builder downloads the signed release tarball and dependency tarball, checks
their SHA-256 against the lock (and the minisign signature when a verifier is
available), unpacks them into a disposable directory, builds each ABI offline
with `zig build --system` and a fresh Zig cache, and records the release
version, both tarball digests, the Zig version and the library digests in the
build manifest. Nothing is patched.

Build outputs under `native/terminal-vt/` (all gitignored; the lock and `NOTICE.Ghostty` are tracked):

| Path                                  | Purpose                                                          |
| ------------------------------------- | ---------------------------------------------------------------- |
| `libs/android/<abi>/libghostty_vt.so` | Imported by CMake                                                |
| `libs/android/SHA256SUMS`             | Per-ABI digests for release notes                                |
| `libs/android/build-manifest.json`    | Release version + tarball digests + Zig version + library digests |
| `libs/android/GHOSTTY-MIT.txt`        | Copy of MIT notice for tarballs                                  |
| `include/ghostty/`                    | Headers for the JNI bridge                                       |

There is **no** committed binary. Redistributable terminal support requires either:

1. Building from the pin locally, or
2. Publishing checksummed libs/APK with the MIT notice (GitHub Release), or
3. Running the optional `native-libs` workflow and attaching its artifacts.

## Release APK (sideload)

```bash
# 1) Native libs (arm64)
./scripts/build-libghostty.sh --abis arm64-v8a
./scripts/verify-libghostty.sh --release

# 2) APK + local artifact folder under dist-download/
./scripts/android-release-apk.sh
```

The release script builds `:app:assembleProductionRelease` (or the Play bundle with `--aab`) and checks the APK's package, version name and version code against `client/ui/nsh.json` through `scripts/lib/release_identity.py`.

### Signing (secret-safe)

- **Never** commit keystores, passwords, or `.jks` files.
- Without a keystore, release variants are debug-signed (fine for personal sideload only).
- Release signing reads the environment (`System.getenv` in `client/android/app/build.gradle.kts`, never `-P` secrets on the command line):

```bash
export MEWLA_ANDROID_KEYSTORE=/absolute/path/to/release.keystore
export MEWLA_ANDROID_KEYSTORE_PASSWORD='…'   # do not commit; do not echo
export MEWLA_ANDROID_KEY_ALIAS='…'
export MEWLA_ANDROID_KEY_PASSWORD='…'
./scripts/android-release-apk.sh
```

`android-release-apk.sh` aborts if `client/android/app/build.gradle.kts` lacks the `MEWLA_ANDROID_*` signing config, and never prints secret values.

## Package identity

The version is `client/ui/nsh.json`'s; `scripts/lib/release_identity.py` derives the version code from it (X·1 000 000 + Y·10 000 + Z·100 + 99, or + N for beta.N). The package is `com.daoleno.mewla`, the one already installed from the store, so an update replaces it in place.

Verify with `./scripts/verify-release-identity.sh` (also `bun run release:identity`).

Official release APKs are signed by the release pipeline. Local builds without signing env are debug-signed and only suitable for personal testing. Never commit `MEWLA_ANDROID_*` secrets or local signing files.

### Staging (local, not a GitHub Release)

```bash
# Clean stage each run: top-level Linux binaries + legal notices + notes (no APK):
./scripts/stage-release.sh
# → dist-download/vVERSION/
#    mewla-linux-amd64  mewla-linux-arm64
#    LICENSE  NOTICE  TRADEMARKS.md  GHOSTTY-MIT.txt
#    RELEASE_NOTES.md  SHA256SUMS  identity.json

# Optional: include a prebuilt/signed APK path
./scripts/stage-release.sh --apk path/to/app-release.apk
```

Each release has versioned notes under `docs/releases/`; the release page includes the matching notes, certificate fingerprint, and sideload warnings.

`stage-release.sh` never creates tags or GitHub Releases. Publishing is a separate Brain/maintainer step.

### CI (GitHub Actions)

Signed arm64 APK and daemon binaries are built in parallel when a reviewed annotated stable or beta tag is pushed by [`.github/workflows/release-artifacts.yml`](../../.github/workflows/release-artifacts.yml). The workflow publishes the matching GitHub Release with the verified daemon archives first, then attaches the signed APK and the complete signed manifest when the Android build is verified. Stable tags become normal Latest releases; beta tags remain prereleases. Manual dispatch is build-only by default and requires an explicit reviewed boolean for publication recovery. A push to `main` that touches `client/`, or a manual dispatch, builds the preview APK in `client.yml`; `native-cache.yml` builds the release task graph on main. Required secret **names** and release preparation details: [ci-release.md](../ci-release.md).

## Related docs

- [CI release pipeline](../ci-release.md)
- [Connect and pair](../connect-and-pair.md)
- [Third-party assets (Ghostty MIT)](../third-party-assets.md)
- [Release blockers](../release-blockers.md)
- [Troubleshooting](../troubleshooting.md)

### Attachments

Composer attachments use the system's `ACTION_GET_CONTENT` chooser with
`CATEGORY_OPENABLE` (`client/android/app/src/main/java/com/daoleno/mewla/files/Picker.kt`):
any app that offers files, several at once. Each picked `content://` URI and its
read grant are kept as they are and streamed through the authenticated upload
transport; nothing is copied and no storage permission is used. A file that
cannot be read comes back with a selection error instead of an attachment.
