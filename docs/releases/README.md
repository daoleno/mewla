# Releases

Each release ships daemon archives for Linux `amd64`, Linux `arm64` and Apple
Silicon macOS, a signed Android `arm64` APK, `SHA256SUMS` and a signed update
manifest. Run `zen update` to move to the newest one; see
[Install](../install-daemon.md#update).

Stable tags look like `v0.1.15`; beta tags look like `v0.1.0-beta.22`. The
installer and `zen update` pick the highest version.

## Known issues

- **iOS**: the TestFlight preview is awaiting Apple's beta review, so the public
  link may not let you install yet. Build from source in the meantime; see
  [Install](../install-daemon.md#ios).
- **Android**: distributed as an APK outside the Play Store. Android asks to
  allow installs from your browser or file manager, and Play Protect may warn.
- **Plugins**: first-time sign-in is not yet available for Slack, and for
  Google Workspace unless your daemon already has Google sign-in configured.
  See [Plugins](../plugins.md#sign-in-availability).
- **macOS daemon**: the Apple Silicon binary is cross-built; report problems you
  hit on a real Mac.
- **Push notifications** are a single best-effort attempt each; a missed alert
  is not retried.

## Release notes

<!-- release-list -->
