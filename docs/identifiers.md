# Identifiers

Mewla names itself to systems (stores, the OS, Chrome, CI) by the reverse of its domain, mewla.ai. The one source is
`scripts/lib/release_identity.py`; `scripts/verify-release-identity.sh` fails when a hard-coded copy drifts, and
`scripts/tests/test_release_identity.py` fails when the old personal prefix comes back.

**The rule.** A name a system uses to find Mewla is `ai.mewla.<what>` (reverse DNS; Keychain services are the app id).
A name that is ours alone is `MEWLA_<PURPOSE>` (secrets, variables, environment variables) or `mewla-<purpose>`
(channels, files), never a person, a vendor or an older product name. Source namespaces (Kotlin packages, the Go module,
JNI symbols) and the repository URL are not identifiers: nothing outside the code names them.

| What | Id | Before 11-21 |
|---|---|---|
| App: Android, iOS, Mac | `ai.mewla.app` | `com.daoleno.mewla` |
| Mewla Dev (Android debug, Mac `--dev`) | `ai.mewla.app.dev` | `.debug`, `.preview.debug`, `.dev` |
| Preview (Android CI/phone build, never distributed) | `ai.mewla.app.preview` | `com.daoleno.mewla.preview` |
| iOS Live Activity / widgets, App Group | `ai.mewla.app.live`, `group.ai.mewla.app` | `.live`, `group.<id>` |
| Mac daemon: LaunchAgent label | `ai.mewla.daemon` | `com.daoleno.mewla.daemon` |
| Chrome native messaging host | `ai.mewla.browser` | `com.mewla.browser` |
| URL scheme, CLI, `product` | `mewla` | unchanged |
| Linux unit, Windows task | `mewla.service`, `Mewla (<user>)` | unchanged (display names, no old prefix) |

TestFlight ships the one app id; the old team's Preview app (`com.daoleno.mewla.preview`) stays what it is until the new
Apple team ships, then it is retired.

## The move

- **Android** is a different app. The release manifest keeps `android.package` = the old id (the builds in the field only
  take a release that names it) and adds `android.application_id` = the new one. The last old-id build (the bridge) shows
  "New Mewla available", stages the device key and computers in a signature-protected provider before the installer
  opens; the new app reads them once at its first start (`client/core/update/handover.ts`), tells the old app to forget
  them, and offers "Remove" for the old app in Settings. A phone that skipped the bridge scans once.
- **Mac / Linux / Windows daemons**: `mewla boot install` (what an updating daemon runs) boots out the job of the old
  label, installs the new one, removes the old files once the new job owns the state, and starts the old job again when
  it cannot. Mewla for Mac takes over a boot install of either label. The state in `~/.mewla` does not move. macOS
  asks again for the permissions the old bundle id held.
- **Chrome**: the old host manifest stays until an extension that asks for the new name has connected; the extension
  (0.4.2) is told to reload by the daemon it connects to.
