# CI release pipeline (signed Android + daemons)

Release preparation starts in the manual [`.github/workflows/release-next-beta.yml`](../.github/workflows/release-next-beta.yml) workflow. Verified artifact construction and public GitHub Release publication remain in [`.github/workflows/release-artifacts.yml`](../.github/workflows/release-artifacts.yml); the preparation workflow does not build or publish public assets itself.

The separate [`native-libs.yml`](../.github/workflows/native-libs.yml) workflow builds the same pinned `libghostty-vt` C ABI for Android on Linux and as an iOS device/simulator XCFramework on macOS. Ordinary `push` to `main` and pull requests run only fast checks in [`.github/workflows/ci.yml`](../.github/workflows/ci.yml). Signed iOS archive/export and optional TestFlight upload are isolated in [`ios-release.yml`](../.github/workflows/ios-release.yml); see [iOS CI and release automation](ios-ci-release.md). The Google Play App Bundle and its testing-track upload are isolated in [`android-play.yml`](../.github/workflows/android-play.yml); see [Google Play](#google-play).

## Ordinary CI triggers

| Event | Fast checks (`installer`, `daemon`, `app`, `native-contract`, `release-identity`) | Native/packaging checks (`ios-native`, `android-native`, daemon release binaries) |
| --- | --- | --- |
| Push to `main` | yes | no |
| Pull request | yes | no |
| Release tag push (`vX.Y.Z` / `vX.Y.Z-beta.N`) | yes | yes |
| Manual `workflow_dispatch` of `ci.yml` | yes | yes |

The `daemon` fast job runs `go test ./...`, `go vet`, the scripted behavior contracts, and the publisher-config check; the release-shaped `build-daemon-linux.sh` binaries (linux amd64/arm64, darwin arm64, with the embedded web UI) build only with the native/packaging checks. The `app` fast job runs unit tests, typecheck and the Android and iOS JS exports; the web export runs only inside the daemon release build, which embeds it. `ci.yml` cancels superseded runs for the same ref.

The release flow does not depend on the `ci.yml` tag trigger. `release-next-beta.yml` pushes the tag with `GITHUB_TOKEN` (which does not recursively trigger workflows) and explicitly dispatches `release-artifacts.yml` and `ios-release.yml`. Those workflows build the signed Android APK and the signed iOS IPA, so an Android native break is caught by `release-artifacts.yml` before the public release is published, and an iOS compile break is caught by `ios-release.yml` before the IPA is exported or uploaded. The unsigned Simulator build and the bounded Android native link additionally run on a manual `ci.yml` dispatch and on a maintainer-pushed release tag.

## What it does

| Step | Release tag push or manual rebuild | Publishing tag push / reviewed recovery only |
| --- | --- | --- |
| Checkout selected ref | yes | — |
| `verify-release-identity.sh` | yes | — |
| Linux amd64/arm64 + macOS arm64 daemon archives | yes | — |
| Build release-grade `libghostty` arm64 | yes | — |
| Clean Expo prebuild + signed arm64 APK | yes | — |
| Verify package / version / ABI / Ghostty native imports / notice / cert fingerprint | yes | — |
| Stage tree + checksums + signed updater manifest | yes | — |
| Upload workflow artifact | yes | — |
| Upload/replace assets on **existing** GitHub Release | **no** | yes |

The canonical normal flow is one manual **Release reviewed version** dispatch with an explicit `X.Y.Z` or `X.Y.Z-beta.N` version. It checks out current `main` with full tag history, proves the target is newer than the tracked current version and annotated current tag, increments Android and iOS build identities, builds the canonical notes from the committed user-facing notes, updates the root changelog index, and runs the release identity and focused contract tests. Only after every gate passes does it commit exactly those generated paths as `github-actions[bot]`, create the annotated target tag at that commit, prove `origin/main` still equals the starting SHA, and atomically push the new `main` commit and tag. Main drift, malformed identity, missing history, missing or malformed user-facing notes, dirty state, existing output, or any failed test stops before that atomic push.

GitHub suppresses recursive workflow triggers for pushes made with `GITHUB_TOKEN`, so the workflow explicitly dispatches both downstream workflows after the atomic push. It dispatches `release-artifacts.yml` with the exact new tag and `publish=true`, and dispatches `ios-release.yml` at the same tag with the tracked next iOS build number, `app_identity=preview`, and `destination=testflight`. This preserves the two downstream effects of a maintainer-pushed release tag. The artifact workflow checks out that immutable tag, proves strict syntax, version equality, annotated-tag identity, exact HEAD resolution, and ancestry on `origin/main`, then reruns the tracked identity verifier. Daemon builds and Android/native work run in parallel, and the desktop archives do not wait for Android:

1. As soon as the daemon binaries are verified, a signing job creates the deterministic archives, `SHA256SUMS` and the signed updater manifest for them. A write-scoped job then creates or reuses the matching draft, uploads that set and publishes the release. `mewla update` and the installer can use it from this point.
2. When the signed APK is verified, a second signing job stages the complete set. It fails unless the daemon archives are byte-identical to the published ones. A write-scoped job then checks the published archives against the new `SHA256SUMS`, uploads the APK, `SHA256SUMS`, `release-manifest.json` and its signature with `--clobber`, and verifies the exact seven-asset set.

Stable tags become normal Latest releases; beta tags remain prereleases. A failed daemon build cannot create a public release, and a failed first upload can leave only a draft. If Android fails, the release stays public with the desktop archives only, until a rerun of the failed jobs attaches the APK. While the Android assets replace the desktop-only `SHA256SUMS` and manifest, an install or update in those few seconds can fail its checksum or signature check and should be retried.

## Repository secrets (names only)

Configure under GitHub → Settings → Secrets and variables → Actions. Values are **not** documented and must never be committed or printed in logs. The secrets keep their original `ZEN_*` names because stored values cannot be read back to copy; each workflow step maps them once into the `MEWLA_*` names that scripts read.

| Secret name | Purpose |
| --- | --- |
| `ZEN_ANDROID_KEYSTORE_BASE64` | Base64 encoding of the PKCS#12 (or JKS) release keystore binary |
| `ZEN_ANDROID_KEYSTORE_PASSWORD` | Keystore password |
| `ZEN_ANDROID_KEY_ALIAS` | Key alias inside the keystore |
| `ZEN_ANDROID_KEY_PASSWORD` | Key password |
| `ZEN_UPDATE_SIGNING_KEY_BASE64` | Base64 encoding of the Ed25519 private PEM matching `release/mewla-update-public-key.pem` |

Runtime mapping (CI materializes the file, mode `0600`, then shreds it):

| CI env | Source |
| --- | --- |
| `MEWLA_ANDROID_KEYSTORE` | Temp path from `scripts/materialize-android-keystore.sh` |
| `MEWLA_ANDROID_KEYSTORE_PASSWORD` | secret `ZEN_ANDROID_KEYSTORE_PASSWORD` |
| `MEWLA_ANDROID_KEY_ALIAS` | secret `ZEN_ANDROID_KEY_ALIAS` |
| `MEWLA_ANDROID_KEY_PASSWORD` | secret `ZEN_ANDROID_KEY_PASSWORD` |

Local maintainer builds use a filesystem path via `MEWLA_ANDROID_KEYSTORE` (see [android.md](internal/android-development.md)); agents must not read `~/.mewla/release-keys`.

## Public certificate identity

Release APKs must match this fingerprint, which `scripts/verify-release-identity.sh` owns and release preparation copies into the notes' **Verify downloads** block (public metadata, not a secret):

```
C2:FC:5B:09:B3:86:92:EE:70:59:71:1F:E7:ED:B8:79:4C:E3:65:FE:1C:7A:06:AB:95:4E:5D:D1:BD:CD:A4:FD
```

`scripts/verify-apk-release.sh` enforces this after the build.

## Release identity

Release notes leave out maintainer identity. It lives in tracked files instead:

| Fact | Source |
| --- | --- |
| Version | `app/app.base.json` `expo.version`, mirrored in `daemon/cmd/mewla/version.go` |
| Android package | `com.daoleno.mewla` (`app/app.base.json`) |
| Android `versionCode` | `app/app.base.json`; preparation increments it by one |
| Android ABI | `arm64-v8a` only (`app/package.json` `build:apk`, `app/modules/terminal-vt/native.lock.json`) |
| iOS Preview bundle, marketing version and build number | See [iOS CI](ios-ci-release.md#signed-release-and-testflight) |

`scripts/verify-release-identity.sh` reads the version, `versionCode` and iOS build number from `app/app.base.json` and `app/ios-build.json`, and checks that `version.go`, the resolved app config, the tag, the release notes and any stage agree with them. Preparation does not edit the verifier. The Android certificate fingerprint stays pinned in it.

## workflow_dispatch inputs

| Input | Type | Default | Meaning |
| --- | --- | --- | --- |
| `ref` | string | — | Exact annotated `vX.Y.Z` or `vX.Y.Z-beta.N` tag to rebuild |
| `publish` | boolean | `false` | Reviewed recovery authorization to reconcile and publish the matching draft/existing release |

Permissions: validation, build and staging jobs have `contents: read`; only the two gated publication jobs have `contents: write`. The jobs that hold the updater signing key never hold a write token, and the reverse.

## Normal release and recovery paths

Prerequisites:

1. Current `main` is clean and contains commits after its tracked annotated release tag.
2. All existing release notes are indexed in root `CHANGELOG.md`; future preparation prepends one link without copying full notes.
3. The four `ZEN_ANDROID_*` secrets above are configured on the repository.
4. `ZEN_UPDATE_SIGNING_KEY_BASE64` is configured with the updater manifest key.
5. `release/plugin-publishers.json` contains the public Client IDs of the approved `daoleno`-owned Mewla GitHub OAuth app with Device Flow enabled and of Mewla's Slack app in the Mewla workspace `T0CA7628ECC`. Run `python3 scripts/plugin-publisher-flags.py --require github --require slack` before preparation; the artifact workflow enforces the same requirement before building. Registration handoff is documented in [Publisher setup](internal/plugins-publisher-setup.md). A null ID is a missing publisher configuration, not a request for publication approval. Never supply a client secret or another application's identity to satisfy this gate.

The normal maintainer action is to dispatch **Release reviewed version** from the Actions UI or CLI:

```bash
gh workflow run release-next-beta.yml \
  --ref main \
  -f version=0.1.2
```

Review the requested machine-readable version/tag, identity checks, tests, atomic push, and downstream dispatch. The workflow uses only `GITHUB_TOKEN`; it has no PAT, release service, database, external action bot, or second publication engine.

### Write the release notes first

Before dispatch, commit the user-facing notes at `docs/releases/reviewed/vX.Y.Z.md` (with the beta suffix when applicable). Preparation fails without them, and never falls back to commit subjects. Write for someone deciding whether to update: what changed for them, in their words. Leave out commit hashes, identity data and dated status.

```markdown
One plain sentence: what this release is about, for a user.

## New

- A user-facing change.

## Fixed

- A user-facing fix.
```

Keep at least one of `## New` and `## Fixed`, with `- ` bullets only. Preparation adds the version heading, **Get it** (update command and one line per download), a compare link, and a collapsed **Verify downloads** block with `SHA256SUMS` usage and the Android certificate fingerprint. Do not precreate the canonical `docs/releases/vX.Y.Z.md`, which remains a generated, fail-closed output.

The generated notes have no iPhone line, because the TestFlight upload runs after the tag. Once the build is in the public `Mewla Preview` group, add `- **iPhone:** [Mewla Preview on TestFlight](https://testflight.apple.com/join/nMQheDCE)` after the Android line in `docs/releases/vX.Y.Z.md`, commit it, and apply the same file to the GitHub Release with `gh release edit vX.Y.Z --notes-file docs/releases/vX.Y.Z.md`.

Preparation, normal app CI and the Android release job run `app/androidAssetNames.test.js` before an expensive signed build. The gate uses Metro asset parsing and React Native's actual Android resource folder/name normalization across repository image and font assets, including density variants. Extensions, punctuation and letter case do not necessarily distinguish Android resource names: use distinct basenames for files such as a PNG and JPEG in the same directory.

### Maintainer fallback: manual annotated tag

If the preparation workflow is unavailable, a maintainer may perform the same tracked identity and canonical-notes updates manually, run `./scripts/verify-release-identity.sh` and the release tests, commit and push that exact change to `main`, then use the existing annotated-tag path:

```bash
git tag -a vX.Y.Z -m "Mewla vX.Y.Z"
git push origin vX.Y.Z
```

The manual tag remains a recovery/maintainer fallback, not a second release engine. Its tag push invokes the same existing artifact workflow.

### Recover artifact publication

If the atomic main/tag push succeeded but either explicit downstream dispatch failed, or a downstream run later failed, do not recreate or move the tag. Recover only the failed path at the exact existing tag.

Artifact publication recovery:

```bash
gh workflow run release-artifacts.yml \
  --ref vX.Y.Z \
  -f ref=vX.Y.Z \
  -f publish=true
```

Preview TestFlight recovery uses the tracked build number from `app/ios-build.json` at that tag:

```bash
gh workflow run ios-release.yml \
  --ref vX.Y.Z \
  -f ref=vX.Y.Z \
  -f build_number=N \
  -f app_identity=preview \
  -f destination=testflight
```

Do not create a GitHub Release by hand. Recovery repeats every identity, SHA, main-ancestry, asset, checksum, and signature gate; it can safely resume a matching draft or reconcile an existing release with `--clobber`. It never creates or rewrites a tag and never force-pushes.

## Google Play

[`.github/workflows/android-play.yml`](../.github/workflows/android-play.yml) builds the Play App Bundle. It is separate from the GitHub release: `release-artifacts.yml` and its sideload APK do not change.

The bundle is built with `MEWLA_ANDROID_DISTRIBUTION=play-store` (`./scripts/android-release-apk.sh --aab`, Gradle `bundleRelease`, arm64-v8a). That channel leaves out `REQUEST_INSTALL_PACKAGES`, the self-update `FileProvider` and the in-app GitHub update check; Play delivers updates. The bundle is signed with the existing release key (`C2:FC:5B…:A4:FD`), which is also the Play upload key. With Play App Signing set to that same key, an install from Play and an install of the GitHub APK have the same signature and can update each other.

### Dispatch

| Input | Values |
| --- | --- |
| `ref` | Any ref that has the Play channel for `artifact-only`; an exact reviewed `vX.Y.Z` / `vX.Y.Z-beta.N` tag for an upload, so a `versionCode` is never spent on an unreleased commit |
| `destination` | `artifact-only` (default), `internal`, or `alpha` (the default closed testing track) |
| `release_status` | `draft` (default) or `completed`. Play accepts only `draft` until the app's first release has been rolled out in Console |

```bash
# Build and verify only; the AAB is a 14-day workflow artifact.
gh workflow run android-play.yml --ref main -f ref=main -f destination=artifact-only

# Upload a release tag to closed testing as a draft, then roll it out in Console.
gh workflow run android-play.yml --ref main -f ref=vX.Y.Z -f destination=alpha -f release_status=draft
```

Every run checks the built AAB with `scripts/verify-aab-play.py` and the pinned bundletool:

- package, `versionName` and `versionCode` match `app/app.base.json`; `targetSdkVersion` is 36;
- no `REQUEST_INSTALL_PACKAGES`, `SYSTEM_ALERT_WINDOW` or `RECORD_AUDIO`, and no app-update `FileProvider` in the merged manifest;
- the embedded Expo config says `distribution.android = play-store`, so the update check stays hidden;
- only arm64-v8a libraries, every `.so` has 16 KB ELF `LOAD` alignment, and every split APK bundletool generates passes `zipalign -c -P 16`;
- the universal APK carries the Ghostty notice and passes the native import check;
- the upload certificate SHA-256 is the public release identity.

The build job then prints the planned Play API calls with `scripts/google-play-upload.py --dry-run`. The upload job runs only for `internal` or `alpha`, in the `google-play` environment, with the uploader from the workflow commit. It never holds the signing key. It opens one edit, uploads the bundle, checks the `versionCode` and SHA-256 Play reports, sets the track release and commits; on any failure it deletes the edit. `--validate-only` (local use) lets Play validate the edit and then discards it.

### Secrets

| Secret | Where | Purpose |
| --- | --- | --- |
| `ZEN_ANDROID_KEYSTORE_BASE64`, `ZEN_ANDROID_KEYSTORE_PASSWORD`, `ZEN_ANDROID_KEY_ALIAS`, `ZEN_ANDROID_KEY_PASSWORD` | repository (already set) | The release keystore, the same as the APK build |
| `MEWLA_PLAY_SERVICE_ACCOUNT_JSON` | environment `google-play` | The full JSON key of the Play Developer API service account |

### Play Console checklist

Do these once, in order. Steps 1–4 and 6 are in Play Console or Google Cloud; nothing here needs a code change.

1. **Create the app.** Play Console → *Create app*: name `Mewla`, default language, *App*, *Free*, accept the declarations. The package name `com.daoleno.mewla` is fixed by the first uploaded bundle.
2. **Prepare Play App Signing with the existing key.** You choose the signing key when you create the first release (step 6), but get the export ready now. In *Test and release → App integrity → Play app signing*, or in the first release's signing dialog, choose *Use existing app signing key from Java keystore* (wording varies: *Export and upload a key from Java keystore*). Download `pepk.jar` and the encryption public key it offers, then on the machine that holds the release keystore:

   ```bash
   java -jar pepk.jar --keystore=<release keystore> --alias=<alias> \
     --output=mewla-signing-key.zip --include-cert --rsa-aes-encryption \
     --encryption-key-path=encryption_public_key.pem
   ```

   Do not accept the Google-generated key. That key is permanent, and GitHub APK installs could never update to Play builds, or back.
3. **Create the service account.** In Google Cloud Console, pick or create a project, enable *Google Play Android Developer API*, then *IAM & Admin → Service accounts → Create*. It needs no Cloud role. Under *Keys → Add key → JSON*, download the key. In Play Console *Users and permissions → Invite new users*, enter the service account email. Under *App permissions*, add Mewla with *View app information* and *Release apps to testing tracks*. Then in GitHub *Settings → Environments*, create `google-play`, optionally require a reviewer, and add the secret `MEWLA_PLAY_SERVICE_ACCOUNT_JSON` with the whole JSON file as its value. Delete the downloaded file afterwards.
4. **Set up the testing tracks.** *Test and release → Testing → Closed testing*: use the default *Closed testing – Alpha* track (API name `alpha`), add a tester email list with at least 12 testers (personal developer accounts need 12 testers opted in for 14 days in a row before production), and pick the countries. Under *Internal testing*, add yourself as a tester.
5. **Build the first AAB.** Dispatch `android-play.yml` with the release tag and `destination=artifact-only`, then download the `mewla-android-play-v<version>-<versionCode>` artifact.
6. **Upload the first release by hand.** The Play API can only address an app after a bundle has fixed its package name, so the first upload goes through Console. In *Internal testing → Create new release*, pick the signing key from step 2 and upload `mewla-signing-key.zip`. Upload the AAB, then save and roll out. Check *App integrity*: both the app signing and the upload certificate SHA-256 must be `C2:FC:5B:09:…:BD:CD:A4:FD`.
7. **Later releases go through the workflow.** Dispatch with `destination=internal` or `alpha`. Use `release_status=draft` until the first rollout is done, then `completed`. Each upload needs a new `versionCode`; release tags already increase it.

## Build-time baseline and optimization

Android SDK setup explicitly requests `platform-tools` because the action's default also requests the removed SDK package `tools`. The following install step retains the pinned Android platform, build-tools, NDK, and CMake packages.

The Linux release job builds all daemon targets with CGO disabled. The Android APK helper verifies pinned Ghostty terminal libraries before clean prebuild and Gradle. Native CI (release tag pushes and manual `ci.yml` dispatch) builds Android debug/release variants with development signing and the release-shaped daemon binaries; official certificate verification remains in the release workflow, and `release-artifacts.yml` rebuilds the daemon archives before publishing.

GitHub scopes a cache to the ref that saved it: a tag run can read `main`'s caches, but no later run can read a tag's. Until v0.2.5 every release saved a 2.2 GB Gradle cache under its own tag, keyed on `app/app.base.json`, so the next release always missed it. In v0.2.5 the Android Gradle step took 642 s with 131 of 996 tasks from cache, and the release was public 13 min 33 s after the tag; the daemon archives had been ready after 2 min 38 s.

Now [`native-cache.yml`](../.github/workflows/native-cache.yml) is the only writer of the Android caches. On `main` it runs the release APK build with debug signing and no secrets whenever the dependencies, the Android plugins, the terminal module or the Ghostty scripts change, every five days so the entries are not evicted, and on dispatch. It saves the Gradle home (keyed on dependencies, not the app version), the verified arm64 Ghostty output, and the pinned Zig and Ghostty source. CI and the release only restore them, through the shared [`android-native`](../.github/actions/android-native/action.yml) action, so their cache keys cannot drift. v0.2.6 was the first release on this setup. Its desktop archives were public 3 min 26 s after the run started, and the APK was attached at 8 min 13 s. The Gradle step took 331 s, with 386 of 996 tasks from the restored cache. The rest is mostly CMake, which Gradle cannot cache, plus the app module, which changes with every release. `build-libghostty.sh` and `build-libghostty-ios.sh` bootstrap the pinned Zig themselves through `scripts/lib/zig.sh`, so no workflow installs Zig.

The same action installs a pinned ccache and points CMake at it through `CMAKE_C_COMPILER_LAUNCHER`/`CMAKE_CXX_COMPILER_LAUNCHER`, so the React Native, codegen and native-module C++ objects come from the warmer's cache. The NDK is reinstalled on every runner, so ccache checks the compiler by content. The warmer, CI and the release each print `ccache --show-stats` after the build.

`native-cache.yml` also has a `macos-26` job that is the only writer of the iOS caches in the shared [`ios-native`](../.github/actions/ios-native/action.yml) action: the verified iOS Ghostty XCFramework, the pinned Zig and Ghostty source, and the CocoaPods download cache (it runs an unsigned prebuild and `pod install`, no secrets). `ios-release.yml` and the tag-time `ios-native` CI job only restore them. Before this, the iOS release saved those caches under the tag and rebuilt the XCFramework (about 4 minutes) on every release. Xcode DerivedData remains uncached because signed, identity-sensitive intermediates are large and the observed evidence does not justify that risk and complexity.

The iOS Ghostty build carries a workaround for an Xcode and Zig mismatch: Xcode 26.6's macOS SDK lists `libSystem` as arm64e only while Zig 0.15.2 wants an exact arm64 match, so `scripts/build-libghostty-ios.sh` builds against a patched SDK clone and `scripts/zig-xcode-compat/xcrun` points Zig at it. A separate `macos-26` job in `native-cache.yml` (so that a red result never blocks the iOS cache refresh) runs [`scripts/check-zig-xcode-compat.sh`](../scripts/check-zig-xcode-compat.sh), which passes while the runner's SDK still needs the workaround and fails, naming what to delete, once it lists arm64. The job runs every five days, so the workaround cannot outlive its reason unnoticed.

The generated viewer bundles (`app/components/markdown/mermaidRuntimeSource.js`, `app/components/screen/screenViewerSource.js` and `screenViewerRuntime.js`, about 4 MB) stay in the repository because the app's Metro bundle imports them. The `App` job in `ci.yml` runs `bun run mermaid:generate --check` and `bun run screen:generate --check`, which fail when a bundle no longer matches what its pinned dependency regenerates.

Watch:

```bash
gh run list --workflow=release-artifacts.yml --limit 5
gh run watch
```

## Staged asset names

Under `dist-download/v<version>/` (and the workflow artifact):

- `mewla-linux-amd64.tar.gz`
- `mewla-linux-arm64.tar.gz`
- `mewla-darwin-arm64.tar.gz`
- `mewla-android-arm64-v<version>.apk`
- `SHA256SUMS`
- `release-manifest.json`
- `release-manifest.json.sig`

Daemon archives contain `mewla`, `LICENSE`, `NOTICE`, and `TRADEMARKS.md`. Release notes stay in the GitHub Release body; Android third-party notices remain embedded in the APK. This keeps the download list focused without dropping required attribution.

## Local helper scripts

| Script | Role |
| --- | --- |
| `scripts/prepare-release.py` | Validate the current annotated release and write the explicit newer identity, canonical notes, and changelog entry |
| `scripts/materialize-android-keystore.sh` | Base64 → temp keystore file (0600); prints path only |
| `scripts/verify-apk-release.sh` | Package/version/ABI/notice/certificate checks |
| `scripts/android-release-apk.sh` | Clean prebuild + signed assembleRelease; `--aab` builds the play-store bundle |
| `scripts/verify-aab-play.py` | Play AAB manifest/channel/16 KB/ABI/notice/certificate checks |
| `scripts/google-play-upload.py` | Play Developer API upload to a testing track (`--dry-run`, `--validate-only`) |
| `scripts/stage-release.sh` | Deterministic stage tree |
| `scripts/sign-release-manifest.sh` | Validate the updater key and sign the exact manifest bytes |
| `scripts/verify-release-identity.sh` | Tracked identity + stage layout |

## Safety

- Secrets never appear in docs, commits, or intentional log lines.
- The updater accepts one public release stream, including prereleases by semantic-version precedence; it has no channel setting.
- The detached Ed25519 signature authenticates manifest version, platform mapping, archive size, and SHA-256 before download installation.
- CI does not read developer home directories (including `~/.mewla/release-keys`).
- `Release reviewed version` can change `main` and create a tag only in one atomic push after all preparation gates pass and only while `origin/main` still equals its starting SHA.
- Public assets remain build-gated in `release-artifacts.yml`; the preparation workflow dispatches it and the existing Preview TestFlight workflow with the same exact reviewed annotated tag.
- Manual artifact dispatch publishes only with the reviewed boolean; the strict annotated stable/beta tag path remains available for maintainer recovery.
- Gradle caches only dependency/build state under the runner's Gradle home, saved by a debug-signed `main` build with no secrets. Zig/Ghostty source and unsigned native-output keys include the native lock and build/verification inputs; every cache hit still runs release-grade manifest, checksum, ABI, and notice verification.
- No cache path includes a keystore, updater signing key, signed APK, or staged release output. Release tag runs never save a cache.
- Pinned Zig archives are SHA-256 verified before extraction under `$MEWLA_TOOL_CACHE` (`$RUNNER_TEMP/tools` in CI; not `/usr/local`). No `sudo` or unverified installers.
