# CI release pipeline (signed Android + daemons)

Release preparation starts in the manual [`.github/workflows/release-next-beta.yml`](../.github/workflows/release-next-beta.yml) workflow. Verified artifact construction and public GitHub Release publication remain in [`.github/workflows/release-artifacts.yml`](../.github/workflows/release-artifacts.yml); the preparation workflow does not build or publish public assets itself.

The separate [`native-libs.yml`](../.github/workflows/native-libs.yml) workflow builds the same pinned `libghostty-vt` C ABI for Android on Linux and as an iOS device/simulator XCFramework on macOS. Ordinary `push` to `main` and pull requests run only fast checks in [`.github/workflows/ci.yml`](../.github/workflows/ci.yml). Signed iOS archive/export and optional TestFlight upload are isolated in [`ios-release.yml`](../.github/workflows/ios-release.yml); see [iOS CI and release automation](ios-ci-release.md). The Google Play App Bundle and its testing-track upload are isolated in [`android-play.yml`](../.github/workflows/android-play.yml); see [Google Play](#google-play).

## Two tracks: tests on main, releases from a green main

Testing and releasing are separate (adopted 11-16, after [Magpie](https://github.com/yetone/magpie)):

- **Tests run on main.** [`ci.yml`](../.github/workflows/ci.yml) runs every suite on each push to `main` and on pull requests. A run on `main` is never cancelled by the next push (a PR's is). A push that changes only `docs/**`, `site/**` or root `*.md` runs no checks. A red `main` is a bug to fix now, at its cause.
- **A release never runs a suite.** It ships a `main` commit whose CI run is green, or the version change on top of one. [`scripts/release-green.py`](../scripts/release-green.py) checks that in seconds. It walks back from the commit to the nearest CI run, and accepts the commits in between only if they are docs-only or the release workflow's `Prepare vX.Y.Z`. A run still going is waited for up to 15 min; a red, cancelled or missing run stops the release.
- **The release path** is build → sign → launch smoke of the exact artifact → publish → released check:
  - each daemon runs on its own platform from a fresh home and state, with auto-update off, and answers `/health` with the release's version;
  - the signed APK installs on a fresh emulator, reports the version, opens and keeps running, and its scanner opens (the camera prompt, then the camera looking for a code: a release build once lost it to R8) (`release-launch.sh`);
  - the same APK is paired to the release's own Linux daemon and the phone lane's flows (`client/scripts/flows/`: send a message, open Calendar, share a picture in) run on it (`release-flows.sh`, `phone.sh flow all` with `PHONE_EXTERNAL=1`).
- **Upgrade tests run on main's schedule.** [`upgrade.yml`](../.github/workflows/upgrade.yml) installs main's signed APK over the latest release's every day; dispatched with `to` = a new tag it proves the previous release → that release instead (the tag's published APK). A failure opens an issue; it does not hold an unrelated release.
- **Release notes are written at release time** from the commits since the previous tag ([`scripts/release-body.py`](../scripts/release-body.py)). Nothing about them is committed, so a release needs no notes commit and no CI round before it. A failed generation falls back to a link to the commits and is reported in the run; it never blocks the release.
- **Every external wait has its own timeout**: each job, and the apt, emulator, download and CI-run waits inside them.

## Released

A release counts only when all of these hold. [`scripts/released.py`](../scripts/released.py) checks them as the release's last job. Run it again before telling anyone (`python3 scripts/released.py --tag vX.Y.Z`):

1. `vX.Y.Z` is an annotated tag on `main` (an ancestor of `origin/main`).
2. Its commit passed CI on `main`, or is the version change on top of a commit that did. A run cancelled by the next push is not a pass.
3. The GitHub release is public and carries every asset, none empty: the four daemon archives, the APK, `SHA256SUMS`, `release-manifest.json` and its `.sig` (8), and for a stable tag Mewla for Mac's five files: `Mewla-<version>-macos-aarch64.zip` (the app's update), `latest.json` and `latest.json.sig` (the update manifest the installed apps read from `releases/latest`), `Mewla-<version>-macos-arm64.dmg` (what people install from) and the same disk image as `Mewla-macos-arm64.dmg`, a name without a version that the site's download links to (13). A beta carries no Mac file: the apps look at the Latest release.
4. A stable tag is marked Latest and is the highest stable version published, or a higher release has since taken the mark. A beta is a prerelease.

A failed release is reported as failed. It is never silently covered by the next tag.

## Ordinary CI triggers


| Event | Checks (`installer`, `daemon`, `daemon-engine`, `daemon-macos`, `client`, `client-ui`, `web-ui`, `windows`, `native-contract`, `release-identity`) | Daemon release binaries |
| --- | --- | --- |
| Push to `main` | yes | no |
| Pull request | yes | no |
| Push to `main` changing only `docs/**`, `site/**` or root `*.md` | no | no |
| Manual `workflow_dispatch` of `ci.yml` | yes | no |

The release binaries are built only by `release-artifacts.yml`.

### Identity

Every commit in Mewla is daoleno's (`daoleno <daodaoonline@gmail.com>`), with no `Co-authored-by` trailer; the one other committer is GitHub's own (`GitHub <noreply@github.com>`) on a merge made on github.com. [`identity.yml`](../.github/workflows/identity.yml) runs [`scripts/check-identity.py`](../scripts/check-identity.py) on a push to any branch and on every PR, over the whole history it carries, so a branch built on the history from before the 11-25 rewrite fails too: rebase it (`git rebase --onto origin/main <old-base>`), never merge it. Every clone commits as daoleno through repo-local `user.name`/`user.email` or the owner's conditional include for this repo's remotes, whatever the machine's default identity; `.claude/settings.json` turns off Claude Code's co-author trailer.

The client's native hosts are not built by `ci.yml`: [`client.yml`](../.github/workflows/client.yml) builds the Android preview APK and the iOS Simulator app (with its unit tests) on a push to `main` that touches `client/` and on dispatch (on the test track: a red run is a bug to fix, and no release waits for it), [`native-cache.yml`](../.github/workflows/native-cache.yml) builds the Android release task graph on `main`, and the release workflows build the signed APK and IPA.

The `daemon` fast job runs `go test ./...` (which includes the scripted behavior contracts), `go vet`, and the publisher-config check. Beside it, the `daemon-engine` job tests the session host's terminal engine (`daemon/ghosttyvt`, build tag `ghosttyvt`) with libghostty-vt linked, through `scripts/test-ghosttyvt.sh -race`. That script builds the static library with `scripts/build-libghostty-desktop.sh` from the same signed, pinned release and Zig as the app, into `~/.cache/mewla/ghostty-desktop`, which the [`ghostty-desktop`](../.github/actions/ghostty-desktop/action.yml) action caches. It never writes `daemon/ghostty-libs`: that directory switches `mewla-dev` to the tagged build. Untagged builds (`go test ./...`, `mewla-dev` without `daemon/ghostty-libs`, `build-mewla-local.sh`) compile the engine's stub and stay `CGO_ENABLED=0`. Release binaries carry the engine: the release-shaped `build-daemon-linux.sh` binaries (linux amd64/arm64 static with `zig cc` and musl, with the embedded web UI) build only with the native/packaging checks; see [Daemon builds](internal/daemon-builds.md). The `client` fast job runs the core tests and `nsh check`, and checks that the open-source licenses (`client/ui/more/settings/licenses/licenses.json`) and the Screen viewer bundle (`client/ui/more/screen/viewerSource.ts`) match what their generators write. The `web-ui` job builds the web UI every daemon embeds as a release does (`scripts/build-web-ui.sh`, with the WebAssembly VT from `main`'s cache): NSH's web package, the splash, the browser terminal, and `scripts/web-page-root.py`, which checks that the page names every file from the site's root (`nsh.json` `web.base` `"/"`), so a reloaded deep link finds them. Until 11-18 that build ran only inside a release, and the NSH 0.6.0 bump broke it on every dev daemon while CI was green. The `client-ui` job runs the UI suite (`nsh test`) on four runners at once: [`client/scripts/ui-test-shard.py`](../client/scripts/ui-test-shard.py) gives each shard its files, every test file `nsh test` would run exactly once, balanced by the seconds each took on CI (`client/scripts/ui-test-seconds.json`; a new file counts as 10 s until the file is refreshed), and runs them one `nsh test <file>` at a time. In one process the suite took 14 min (856 s on 11-14); a shard takes about a quarter of that. Shards are separate runners, not processes side by side, because the tests wait on the clock. `ci.yml` cancels superseded runs for the same ref.

A `daemon-macos` job on `macos-latest` runs the daemon's `go test ./...` and `go vet ./...` built for darwin: every package, including the Mac's own code (Screen, the agent display, the keychain). The tests that need a person's screen, its permissions or real apps skip themselves unless asked for (`MEWLA_ACCEPT` and the like); The session host's tests that need the terminal engine (`ghosttyvt`) run in the Linux `daemon-engine` job's `scripts/test-ghosttyvt.sh`. Its tests get a short temporary directory on its real path (`/private/tmp/mg.*`): sockets in `t.TempDir()` under the Mac's `/var/folders` `TMPDIR` pass the 104-byte Unix socket limit, and `/tmp` is a symlink that tests comparing resolved paths see through.

A `windows` job on `windows-latest` builds every daemon package for Windows, vets and tests the Windows platform layer (`daemon/platform`: file locks, Job Objects, replacing a running executable, the per-user logon task; plus `control`, `sqlitedb` and `selfupdate`), builds `mewla.exe`, and runs `scripts/tests/install_ps1_test.ps1`, the offline tests of `install.ps1` under Windows PowerShell 5.1. The platform tests also cover reading the person's current environment from the registry, and the `launchsh` tests the Shell Session's shell (PowerShell 7 when installed). The job gates the run like the others.

The release flow does not depend on the `ci.yml` tag trigger. `release-next-beta.yml` pushes the tag with `GITHUB_TOKEN` (which does not recursively trigger workflows) and explicitly dispatches `release-artifacts.yml`. That workflow builds and checks everything, publishes the release when every check passed, and then dispatches `ios-release.yml`, which builds the signed iOS IPA and sends the Preview to TestFlight. An Android native break is caught by `release-artifacts.yml` before anything is published, and an iOS compile break by `ios-release.yml` before the IPA is exported or uploaded.

## What it does

| Step | Release tag push or manual rebuild | Publishing tag push / reviewed recovery only |
| --- | --- | --- |
| Checkout selected ref | yes | — |
| `verify-release-identity.sh` | yes | — |
| The daemons' inputs: the session host's terminal engine (`libghostty-vt`) per target and the browser terminal's WebAssembly VT from the caches `main`'s `native-cache.yml` saves (used only when their manifest matches the lock and their sha256, otherwise built in the job from the signed release), and the web UI every daemon embeds, built once (`web-ui`) | yes | — |
| Linux amd64/arm64 (static, musl) and Windows amd64 (static, MinGW) daemons with `zig cc` on Ubuntu, one runner each (`daemon-build`, collected into `mewla-daemon-binaries-linux-<tag>` by `daemon`); macOS arm64 daemon on a `macos-26` runner (`mewla-daemon-binaries-darwin-<tag>`); all with the session host's terminal engine (`ghosttyvt`) | yes | — |
| Run each daemon on its own platform: Linux amd64, Linux arm64 (`ubuntu-24.04-arm`), macOS (`daemon/releasesmoke`), Windows (`windows-latest`: version, the host with the engine, `/health`, `install.ps1` from a release-shaped zip, `scripts/tests/release_smoke_windows.ps1`) | yes | — |
| Build release-grade `libghostty` arm64 | yes | — |
| Signed arm64 APK of the NSH client (`client/android`, `:app:assembleProductionRelease -Pmewla.channel=github`) | yes | — |
| Launch smoke of the signed APK on a fresh emulator (`android-launch`, `client/scripts/release-launch.sh`): it installs, reports the release's version, opens and is still running with nothing in the crash log after 10 s; the emulator boots while the APK builds | yes | — |
| Verify package / version / ABI / Ghostty native imports / notice / cert fingerprint | yes | — |
| Mewla for Mac (`mac-app`, [`macos-release.yml`](../.github/workflows/macos-release.yml) called from here; a stable tag or a candidate, not a beta): the app and its daemon built on `macos-26`, signed with the Developer ID certificate (hardened runtime, secure timestamps, inside out), notarized and stapled, the `.dmg` signed, notarized and stapled, the update zip of the stapled app and the update manifest signed with `MEWLA_MAC_UPDATE_KEY` (a candidate makes no manifest). The files wait in the artifact `mewla-macos-release-<tag>`; `release-ready` needs the job, so a release whose Mac files were not made is not published | yes | — |
| Stage tree + checksums + signed updater manifest (tags only; a candidate is never signed) | yes | — |
| Upload workflow artifact | yes | — |
| `release-ready`: every job above succeeded | yes | — |
| Publish the complete set once: a stable tag as Latest, a beta tag as a prerelease | **no** | yes |
| Notes written from the commits (`scripts/release-body.py`) | **no** | yes |
| `released`: the definition above (`scripts/released.py`) | **no** | yes |
| Dispatch `ios-release.yml` (Preview to TestFlight, submitted for beta review) | **no** | yes |

The canonical normal flow is one manual **Release reviewed version** dispatch with an explicit `X.Y.Z` or `X.Y.Z-beta.N` version. It checks out current `main` with full tag history and checks that `main` passed CI (`scripts/release-green.py`). It then proves the target is newer than the tracked current version and the annotated current tag, increments the Android and iOS build identities, and runs the release identity verifier and the workflow contract tests. Only after every gate passes does it commit exactly those generated paths as daoleno, create the annotated target tag at that commit, prove `origin/main` still equals the starting SHA, and atomically push the new `main` commit and tag. A red `main`, main drift, malformed identity, missing history, a version without its What's New (`release-notes/X.Y.Z.json`) or stale copies of it, dirty state, an existing tag, or a failed check stops before that atomic push.

GitHub suppresses recursive workflow triggers for pushes made with `GITHUB_TOKEN`, so the workflow explicitly dispatches `release-artifacts.yml` with the exact new tag and `publish=true` after the atomic push. The artifact workflow checks out that immutable tag, proves strict syntax, version equality, annotated-tag identity, exact HEAD resolution, and ancestry on `origin/main`, then reruns the tracked identity verifier and `scripts/release-green.py` on the tagged commit. Everything after that is automatic; there is no hold and no manual promotion:

1. The daemons, the signed APK and the APK's launch smoke run side by side. The launch job boots its emulator with the run and waits for this run's APK.
2. Once the daemons have run on their platforms and the APK is verified, a signing job stages the complete set: the four archives, the APK, `SHA256SUMS`, and the signed updater manifest.
3. `release-ready` passes only if every one of those jobs succeeded, `mac-app` among them. Cancelled or skipped counts as not passed; the skips allowed are the stage of a build-only candidate, which is never signed, and the Mac app of a beta tag, which has none.
4. A write-scoped job then writes the notes from the commits since the previous tag, creates the draft, uploads every asset (a stable tag's thirteen: the eight and Mewla for Mac's five, which it checks are there first), verifies the exact set, and makes it public: a stable tag as Latest, a beta tag as a prerelease. A rerun after publication passes only if the release already carries the same `SHA256SUMS`, and never replaces public bytes.
5. `released` checks the [definition](#released). Then `ios-testflight` dispatches `ios-release.yml` at the tag (`app_identity=preview`, `destination=testflight`, the build number `release_identity.py` derives). That workflow uploads the Preview to TestFlight and submits it for beta review. It then expires the older Preview builds still waiting for beta review, so only the newest one waits ([iOS CI](ios-ci-release.md#signed-release-and-testflight)). A bare tag push no longer starts `ios-release.yml`.

A red job publishes nothing: no draft, no assets. Fix the cause and **Re-run failed jobs**; GitHub reruns the failed jobs and every job that depends on them, so `release-ready`, publish and the iPhone dispatch follow. A flaky emulator in `android-launch` is the expected reason for such a rerun.

Until v0.4.4 the desktop archives were public a few minutes before the APK, the release could be held as a prerelease (`MEWLA_RELEASE_HOLD`) while a Worker replayed the real APK on an emulator, and Brain promoted it by hand before dispatching TestFlight. The replay's upgrade from the previous release became `android-upgrade` in v0.4.5, and on 11-16 moved off the release path to [`upgrade.yml`](../.github/workflows/upgrade.yml) on main's schedule. The replay's other two checks are gone from releases:

- **30 computer switches with one computer frozen** were added for the v0.4.0 freeze, NSH's List leaking its `empty` cell. CI covers that cause. `client/core/listEmpty.test.ts` fails the moment an `empty=` comes back. `Sessions.test.tsx` ("a link for the other computer while All Sessions waits…") runs the same switch headless. With one `empty=` put back on NSH 0.5.0 that test hangs, which fails the run, and on the fixed tree it passes in about 2 s. The switches passed every release since (v0.4.1–v0.4.3) without catching anything.
- **Opening on the owner's dev daemon** needed the owner's machine, which a release must not depend on.

`client.yml`'s upgrade gate (Preview to Preview) remains a manual check for the client; releases do not wait for it. Nor does a release wait for any proof after it: there is no post-publish Linux, Windows, Mac or Android pass by hand. What CI cannot check yet is listed in the report of the change (Brain worklog `mewla-fast-release-1116`).

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
| Version | `client/ui/nsh.json` `version`, mirrored in `daemon/cmd/mewla/version.go` |
| Android package | `com.daoleno.mewla` (`client/ui/nsh.json` `id`, `client/android/app/build.gradle.kts`) |
| Android `versionCode` and iOS `CFBundleVersion` | Derived from the version (below); `client/ios/project.yml` carries the iOS values |
| Android ABI | `arm64-v8a` only (`scripts/android-release-apk.sh` `-Pmewla.abis=arm64-v8a`, `native/terminal-vt/native.lock.json`) |
| iOS Preview bundle, marketing version and build number | See [iOS CI](ios-ci-release.md#signed-release-and-testflight) |

One rule (`scripts/lib/release_identity.py`, the same in the client's Gradle build) gives every build number from the version `X.Y.Z` or `X.Y.Z-beta.N`: `X·1 000 000 + Y·10 000 + Z·100 + 99` for a release and `+ N` instead of `+ 99` for `beta.N` (Y and Z at most 99, N at most 98). 0.3.2 is 30299 and 0.3.2-beta.1 is 30201, so the numbers grow with the version and start above every Expo build (Android `versionCode` 142, iOS build 140 at 0.3.1). The iOS marketing version is `X.Y.Z` without `-beta.N`.

`scripts/verify-release-identity.sh` reads the version from `client/ui/nsh.json` and derives the build numbers, and checks that `version.go`, the client's Gradle and Xcode projects, the tag, the release notes and any stage agree with them. Preparation does not edit the verifier. The Android certificate fingerprint stays pinned in it.

## workflow_dispatch inputs

| Input | Type | Default | Meaning |
| --- | --- | --- | --- |
| `ref` | string | — | Exact annotated `vX.Y.Z` or `vX.Y.Z-beta.N` tag to rebuild, or a branch/commit for a build-only candidate |
| `publish` | boolean | `false` | Reviewed recovery authorization to reconcile and publish the matching draft/existing release |
| `candidate_version` | string | empty | Required for a build-only candidate: the `X.Y.Z` its binaries report, newer than the tracked version |

A **build-only candidate** (`ref` is not a tag, `publish=false`) builds and verifies every daemon and the signed APK exactly as a release would, runs each daemon on its platform, builds, signs and notarizes Mewla for Mac (without its update manifest), and stops: no updater-key signing, no stage, no publication. Its artifacts are kept for five days. Use it to test a release candidate before tagging:

```bash
gh workflow run release-artifacts.yml --ref main -f ref=main -f publish=false -f candidate_version=0.3.0
```

Permissions: validation, build and staging jobs have `contents: read`; only the two gated publication jobs have `contents: write`. The jobs that hold the updater signing key never hold a write token, and the reverse.

## Normal release and recovery paths

Prerequisites:

1. Current `main` contains commits after its tracked annotated release tag, and CI passed on it (or on the last commit before docs-only ones).
2. The four `ZEN_ANDROID_*` secrets above are configured on the repository.
3. `ZEN_UPDATE_SIGNING_KEY_BASE64` is configured with the updater manifest key.
4. `release/plugin-publishers.json` contains the public Client IDs of the approved `daoleno`-owned Mewla GitHub OAuth app with Device Flow enabled and of Mewla's Slack app in the Mewla workspace `T0CA7628ECC`. Run `python3 scripts/plugin-publisher-flags.py --require github --require slack` before preparation; the artifact workflow enforces the same requirement before building. Registration handoff is documented in [Publisher setup](internal/plugins-publisher-setup.md). A null ID is a missing publisher configuration, not a request for publication approval. Never supply a client secret or another application's identity to satisfy this gate.

The normal maintainer action is to dispatch **Release reviewed version** from the Actions UI or CLI:

```bash
gh workflow run release-next-beta.yml \
  --ref main \
  -f version=0.1.2
```

Review the requested machine-readable version/tag, identity checks, tests, atomic push, and downstream dispatch. The workflow uses only `GITHUB_TOKEN`; it has no PAT, release service, database, external action bot, or second publication engine.

### Release notes

The GitHub release notes need nothing written before a release (its What's New does, below). The publish job writes the notes from the subjects of the commits since the previous tag, newest first, leaving out merges and `Prepare vX.Y.Z` (`python3 scripts/release-body.py --tag vX.Y.Z` prints them). A subject that says, before its first colon, that something no longer happens, or starts with Fix, goes under **Fixed**; the rest under **Changes**. Then come **Get it** (the update command and one line per download), a compare link, and a collapsed **Verify downloads** block with `SHA256SUMS` usage and the Android certificate fingerprint. Commit subjects are the notes, so write them for someone deciding whether to update. Until v0.4.10 the notes were committed (`docs/releases/vX.Y.Z.md`, indexed in `CHANGELOG.md`), and the notes commit cost a full CI round before every release.

**What's New** (`release-notes/X.Y.Z.json`, the sheet the app shows once after the update, its chat line and About's What's new) is required for every release. Whoever lands a feature a person will feel adds it to the next version's file in the same commit; the release preparation is the backstop: `scripts/prepare-release.py` refuses a version without its file, saying what to write and where, and `scripts/verify-release-identity.sh` (CI on every push, the artifact workflow at the tag) refuses a tracked version without one, so a manual tag cannot ship one either. A release of fixes only is a file with empty `highlights` and its `fixes`; from 0.4.20 on those are at most three short lines (100 characters each), and a release with more to tell gives it a highlight with its scene, so no release reaches people as a wall of text (both backstops refuse it). Run `python3 scripts/release-notes.py` to write the copies the app and the daemon ship (`client/ui/whatsnew/notes.json`, `daemon/whatsnew/notes.json`); CI's `release-identity` job checks them on every push (`--check`). The shape and the rules are in [`release-notes/README.md`](../release-notes/README.md).

### Maintainer fallback: manual annotated tag

If the preparation workflow is unavailable, a maintainer may perform the same tracked identity updates manually (with the version's `release-notes/X.Y.Z.json` and `python3 scripts/release-notes.py`), run `./scripts/verify-release-identity.sh`, which refuses a version without its What's New, and the release tests, commit and push that exact change to `main`, then use the existing annotated-tag path:

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

Preview TestFlight recovery derives the build number from that tag's version when `build_number` is left out; pass a larger one to upload again:

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

- package, `versionName` and `versionCode` match `client/ui/nsh.json` and the derived code; `targetSdkVersion` is 36;
- no `REQUEST_INSTALL_PACKAGES`, `SYSTEM_ALERT_WINDOW` or `RECORD_AUDIO`, and no app-update `FileProvider` in the merged manifest;
- nothing of the GitHub channel: no Phone use service, receiver, accessibility service or `WRITE_SECURE_SETTINGS`, and no in-app update (`-Pmewla.channel=play` links neither module);
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

## iOS update notice (TestFlight)

A Preview build archived for TestFlight carries the build-time channel `testflight` (`MEWLA_IOS_DISTRIBUTION`, set by `ios-release.yml` for tag pushes and for Preview `destination=testflight` dispatches; any other iOS build, including local and Production, stays silent). It shows the same About "Update available" row and drawer dot as Android, and the button opens TestFlight on Mewla's page (`itms-beta://beta.itunes.apple.com/v1/app/<app id>`, falling back to the public link, then testflight.apple.com).

An uploaded, `VALID` build is not installable: the public `Mewla Preview` group gets it only after Beta App Review. [`ios-testflight-available.yml`](../.github/workflows/ios-testflight-available.yml), the last job of **iOS TestFlight review** (started by App Store Connect's webhook, plus a daily sweep), therefore asks App Store Connect (`scripts/testflight-available-build.py`) for the highest-numbered non-expired, processed build in the group whose external beta state is `BETA_APPROVED` (what App Store Connect reports for every reviewed Mewla build) or `IN_BETA_TESTING`, and publishes `testflight-manifest.json` plus its Ed25519 signature (the same updater key as the release manifest) to a rolling prerelease tagged `ios-testflight`. That release is never Latest and is outside the versioned releases' exact asset sets. The manifest carries the bundle ID, the build's release as `marketing_version` (X.Y.Z, named from the build number: a Preview build's own marketing version is its train, `0.4.9` for every 0.4.x, see [iOS CI release](ios-ci-release.md)), build number, the Preview app ID (from the `MEWLA_ASC_PREVIEW_APP_ID` variable) and the public link. The app verifies the signature, rejects another bundle ID, and announces the build only when its build number is higher than its own, which every newer release's is; the version it shows is the release, so testers read "v0.4.10", not "0.4.9". The manifest only moves forward. The ASC key, the signing key and the write token live in three separate jobs.

[`ios-testflight-review.yml`](../.github/workflows/ios-testflight-review.yml) (**iOS TestFlight review**, on each App Store Connect build event through the webhook receiver, daily at 03:47 UTC as a safety sweep, and on dispatch) writes where the newest Preview builds stand into its job summary: each build's number, its Beta App Review state, TestFlight's external state and whether it is in `Mewla Preview` (`scripts/testflight-review-state.py`). Once a build in the group has passed review, it turns the group's public TestFlight link on if it is off, and prints the link; if App Store Connect refuses, the summary says what to click. It then runs the Beta App Review queue (`scripts/testflight-review-queue.py`, see [iOS CI release](ios-ci-release.md)): once the build in review is approved or rejected, it submits the newest build waiting behind it and expires builds superseded before they were submitted; `dry_run` only reads. Its review job holds only the Preview ASC key and writes nothing to the repository; the availability job it calls last publishes the manifest.

## Build-time baseline and optimization

Android SDK setup explicitly requests `platform-tools` because the action's default also requests the removed SDK package `tools`. The following install step retains the pinned Android platform, build-tools, NDK, and CMake packages.

Until v0.4.3 one job built the Linux and Windows daemons one after the other: the engine for three targets (675 s), the web UI (176 s, most of it the browser terminal's WebAssembly VT), then three Go builds (357 s), 1217 s in all, and the macOS job built its own engine and web UI again (438 s). From v0.4.4 to v0.4.12 the engine for each of the four targets and the one web UI built at the same time, each on its own runner and fresh, and were handed to the daemon builds as artifacts of the same run: the engines took 3:30–5:42, the WebAssembly VT about 2:50, a pinned Zig download once 2:01, and each Go cross build 2–4:46 with no cache for its target, so v0.4.12's daemons were ready 10:34 into the run. Since then the engines, the WebAssembly VT, the pinned Zig tarball and each target's Go build cache come from `main` like the Android Ghostty library (below), and each daemon build restores its own engine instead of waiting for an engine job. `build-daemon-linux.sh` takes the engine when its manifest matches `native.lock.json` and the library its recorded sha256 (otherwise it builds it), and `--web-ui prebuilt` takes the staged `daemon/webui/dist` after `scripts/web-page-root.py` finds that its page loads every file from the site's root. So the macOS daemon embeds the same web UI as the others, built on Linux. The per-target intermediates (`daemon-part-*`) are kept one day; the published artifact names did not change. The Android APK helper verifies pinned Ghostty terminal libraries before the client's Gradle build. CI on release tag pushes and manual `ci.yml` dispatch builds the release-shaped daemon binaries; official certificate verification remains in the release workflow, and `release-artifacts.yml` rebuilds the daemon archives before publishing.

GitHub scopes a cache to the ref that saved it: a tag run can read `main`'s caches, but no later run can read a tag's. Until v0.2.5 every release saved a 2.2 GB Gradle cache under its own tag, keyed on the app's version file, so the next release always missed it. In v0.2.5 the Android Gradle step took 642 s with 131 of 996 tasks from cache, and the release was public 13 min 33 s after the tag; the daemon archives had been ready after 2 min 38 s.

Now [`native-cache.yml`](../.github/workflows/native-cache.yml) is the only writer of the Android caches. On `main` it runs the client's release APK build with debug signing and no secrets whenever its Gradle files, the terminal bridge, `native/terminal-vt` or the Ghostty scripts change, every five days so the entries are not evicted, and on dispatch. It saves the Gradle home (keyed on dependencies, not the app version), the verified arm64 Ghostty output, and the pinned Zig and Ghostty source. CI and the release only restore them, through the shared [`android-native`](../.github/actions/android-native/action.yml) action, so their cache keys cannot drift. v0.2.6 was the first release on this setup. Its desktop archives were public 3 min 26 s after the run started, and the APK was attached at 8 min 13 s. The Gradle step took 331 s, with 386 of 996 tasks from the restored cache. The rest is mostly CMake, which Gradle cannot cache, plus the app module, which changes with every release. `build-libghostty.sh` and `build-libghostty-ios.sh` bootstrap the pinned Zig themselves through `scripts/lib/zig.sh`, so no workflow installs Zig.

The same action installs a pinned ccache and points CMake at it through `CMAKE_C_COMPILER_LAUNCHER`/`CMAKE_CXX_COMPILER_LAUNCHER`, so the terminal bridge's C++ objects come from the warmer's cache. The NDK is reinstalled on every runner, so ccache checks the compiler by content. The warmer, CI and the release each print `ccache --show-stats` after the build.

`native-cache.yml` is also the only writer of the desktop release caches in the shared [`desktop-release-cache`](../.github/actions/desktop-release-cache/action.yml) action. Its `desktop` jobs build each Linux and Windows daemon as a release does (against a stand-in web UI), saving the pinned Zig tarball, the target's `libghostty-vt` and its Go and `zig cc` caches; `web` builds the WebAssembly VT and `darwin` the macOS engine on `macos-26`. The engine and VT keys hash only what derives them (the lock, the build scripts, the packer sources), so an ordinary merge leaves them warm; the Go cache is keyed on `daemon/go.sum` and restored by prefix, so a release recompiles only the packages that changed. The trust boundary is the one the Android cache already had: a release uses an output `main`'s own workflow built, and only after `build-libghostty-desktop.sh` or `build-libghostty-web.sh` finds its manifest equal to the current derivation and its sha256 intact.

The Gradle home was 4.1 GB compressed (9.6 GB on disk) by v0.4.12 and took 56 s to restore: 3.3 GB of it belonged to Gradle 9.3.1, carried along by the prefix restore after the wrapper moved to 9.4.1, and 3.4 GB were artifact transforms. On the same runner an APK build took 146 s with all of it, 138 s without 9.3.1 and 147 s without the transforms too. The warmer now prunes both before saving (`android-gradle-v2-`). The release APK also skips `lintVitalProductionRelease`: `client.yml` runs lint on `main` in its `assemblePreviewRelease` build of the same sources.

`native-cache.yml` also has an `xcode-27` job that is the only writer of the iOS caches in the shared [`ios-native`](../.github/actions/ios-native/action.yml) action: the verified iOS Ghostty XCFramework, the pinned Zig and the signed Ghostty release tarballs (no secrets). `ios-release.yml` and `client.yml` only restore them. Before this, the iOS release saved those caches under the tag and rebuilt the XCFramework (about 4 minutes) on every release. Xcode DerivedData remains uncached because signed, identity-sensitive intermediates are large and the observed evidence does not justify that risk and complexity.

The iOS Ghostty build carries one workaround for an Xcode and Zig mismatch: Xcode's macOS SDK lists `libSystem` as arm64e only while Zig wants an exact arm64 match, so `scripts/build-libghostty-ios.sh` builds against a patched SDK clone and `scripts/zig-xcode-compat/xcrun` points Zig at it. (There used to be a second one, a patch to Zig 0.15.2's bundled libcxx for an undeclared `INFINITY` under the Xcode 27 SDK. Zig 0.17.0, which the pinned libghostty-vt release requires, builds libcxx against that SDK unpatched, so the patch, the private Zig lib dir and the `zig_lib_patches` manifest field are gone.) A separate `xcode-27` job in `native-cache.yml` (so that a red result never blocks the iOS cache refresh) runs [`scripts/check-zig-xcode-compat.sh`](../scripts/check-zig-xcode-compat.sh), which reads the SDK's libSystem stub. It passes while the workaround is still needed and fails, naming what to delete, once it is not. The job runs every five days, so the workaround cannot outlive its reason unnoticed.

The generated Screen viewer bundle (`client/ui/more/screen/viewerSource.ts`, noVNC 1.7.0 pinned in the root `package.json`) stays in the repository because the app's WebView loads no remote scripts. The `client` job in `ci.yml` runs `bun client/ui/more/screen/runtime/generate.mjs --check`, which fails when the bundle no longer matches what the pinned noVNC regenerates.

The vendored diagram renderer (`client/core/mermaid/pele.js`, Pele 1.13.0 pinned in the root `package.json`) is checked the same way: the `client` job runs `bun client/core/mermaid/vendor-pele.mjs --check`, which fails when the file no longer matches the pinned package.

Watch:

```bash
gh run list --workflow=release-artifacts.yml --limit 5
gh run watch
```

## Mewla for Mac

[`macos-release.yml`](../.github/workflows/macos-release.yml) makes the Mac app's release files; the release track calls it (`mac-app`) and `publish` uploads them. It needs, in the `app-store-connect-preview` environment, `MEWLA_MAC_DEVELOPER_ID_P12_BASE64` and `MEWLA_MAC_DEVELOPER_ID_P12_PASSWORD` (the Developer ID Application certificate with its private key), `ZEN_ASC_API_KEY_BASE64` and `ZEN_ASC_KEY_ID` (the App Store Connect key, which notarizes) and `MEWLA_MAC_UPDATE_KEY` (the update manifest's key; its public half is built into every app). The gate names any that is missing and stops the release before anything is published. The Developer ID certificate's intermediate (Apple's G2 CA) is fetched from apple.com into the job's temporary keychain; the keychain and the key files are deleted at the end of the step.

Run without a tag (`gh workflow run macos-release.yml --ref <branch>`) it is the proof: two builds of the commit (A at its version, B the next patch), both signed, notarized and stapled, with their update source on loopback, then a clean `macos-26` runner holding no secret installs A from its `.dmg` as a person does (the download's quarantine kept; `spctl`, `stapler validate`), launches it through LaunchServices, checks the daemon, the Keychain item, launchd restarting the daemon with the Sessions kept, and updates A to B (same daemon id, paired device, login item, designated requirement). Nothing is tagged, attached or published.

What is not proven by any run before a first release: that `publish` uploads these files into a real draft (its list is checked against `scripts/released.py` by `scripts/tests/test_released.py`), and the site's `releases/latest/download/Mewla-macos-arm64.dmg` link.

## Staged asset names

Under `dist-download/v<version>/` (and the workflow artifact):

- `mewla-linux-amd64.tar.gz`
- `mewla-linux-arm64.tar.gz`
- `mewla-darwin-arm64.tar.gz`
- `mewla-windows-amd64.zip`
- `mewla-android-arm64-v<version>.apk`
- `SHA256SUMS`
- `release-manifest.json`
- `release-manifest.json.sig`

Not published, kept 90 days as the workflow artifact `mewla-android-symbols-v<version>`: R8's `mapping.txt` (`retrace`) and the client's JS source map `bundleUi.js.map` (`nsh symbolicate`), which turn a captured Android trace or a minified JS stack from that release back into names. `ios-release.yml` keeps the iOS build's map the same way, as `<artifact name>-symbols-<build number>`.

Mewla for Mac's five files are not in this tree or in `SHA256SUMS`: they come from `mac-app` (above) and have their own signed manifest, `latest.json`.

Daemon archives contain `mewla` (`mewla.exe` in the Windows zip), `LICENSE`, `NOTICE`, and `TRADEMARKS.md` at their root. The site serves `install.sh` and `install.ps1` from the repository byte for byte. Release notes stay in the GitHub Release body; Android third-party notices remain embedded in the APK. This keeps the download list focused without dropping required attribution.

## Local helper scripts

| Script | Role |
| --- | --- |
| `scripts/prepare-release.py` | Validate the current annotated release and the new version's What's New, and write the explicit newer identity |
| `scripts/materialize-android-keystore.sh` | Base64 → temp keystore file (0600); prints path only |
| `scripts/verify-apk-release.sh` | Package/version/ABI/notice/certificate checks |
| `scripts/lib/release_identity.py` | The version (`client/ui/nsh.json`) and the build numbers derived from it |
| `scripts/android-release-apk.sh` | The client's signed GitHub APK (`assembleProductionRelease`); `--aab` builds the Play bundle (`bundleProductionRelease -Pmewla.channel=play`) |
| `scripts/verify-aab-play.py` | Play AAB manifest/channel/16 KB/ABI/notice/certificate checks |
| `scripts/google-play-upload.py` | Play Developer API upload to a testing track (`--dry-run`, `--validate-only`) |
| `scripts/stage-release.sh` | Deterministic stage tree |
| `scripts/sign-release-manifest.sh` | Validate the updater key and sign the exact manifest bytes |
| `scripts/verify-release-identity.sh` | Tracked identity + stage layout |
| `scripts/model-picker-gate.sh` | Before a release that touches model switching: walks every agent × moment (new Session, after the first message, while working, after a daemon restart) against the real agents signed in on this computer, on an isolated daemon, and checks each pick in the agent's own record of its next turn; it fails a Claude menu whose names differ from Claude's own (versions included), an idle pick that answers after 15 s, a pick while Claude or Codex works that is not held for the turn's end, and a held pick not in force 15 s after its turn ended (`--routes` adds this computer's default Provider connections). Local only: hosted CI has no agent sign-ins |

## Safety

- Secrets never appear in docs, commits, or intentional log lines.
- The updater, both installers and the app follow GitHub's `releases/latest` (the newest non-prerelease release); there is no channel setting.
- The detached Ed25519 signature authenticates manifest version, platform mapping, archive size, and SHA-256 before download installation.
- CI does not read developer home directories (including `~/.mewla/release-keys`).
- `Release reviewed version` can change `main` and create a tag only in one atomic push after all preparation gates pass and only while `origin/main` still equals its starting SHA.
- Public assets remain build-gated in `release-artifacts.yml`; the preparation workflow dispatches it and the existing Preview TestFlight workflow with the same exact reviewed annotated tag.
- Manual artifact dispatch publishes only with the reviewed boolean; the strict annotated stable/beta tag path remains available for maintainer recovery.
- Gradle caches only dependency/build state under the runner's Gradle home, saved by a debug-signed `main` build with no secrets. Zig/Ghostty source and unsigned native-output keys include the native lock and build/verification inputs; every cache hit still runs release-grade manifest, checksum, ABI, and notice verification.
- No cache path includes a keystore, updater signing key, signed APK, or staged release output. Release tag runs never save a cache (`setup-go` on the arm64 runner, which has no `main` entry, runs with `cache: false`).
- Release desktop engines and the WebAssembly VT come from caches `main` wrote, used only when their manifest matches the current lock and their sha256; a release does not rebuild them from the signed tarballs unless that check fails.
- Pinned Zig archives are SHA-256 verified before extraction under `$MEWLA_TOOL_CACHE` (`$RUNNER_TEMP/tools` in CI; not `/usr/local`). No `sudo` or unverified installers.

### Cloud CLI bootstrap maintenance

Before a Mewla release, check the official Google Cloud SDK versioned archives
and refresh `gcpBootstrapVersion` and all four platform SHA256 values in
`daemon/connections/gcp_install.go` together when the bootstrap is superseded.
Verify the official archives and run the cloud installer failure/rollback tests.
These pins bootstrap Google's own updater; normal SDK updates do not depend on a
Mewla release. AWS uses signed current packages and needs no version pin.
