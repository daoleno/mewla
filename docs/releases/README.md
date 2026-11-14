# Releases

Each release ships daemon archives for Linux `amd64`, Linux `arm64`, Apple
Silicon macOS and Windows x64, a signed Android `arm64` APK, `SHA256SUMS` and a signed update
manifest. Run `mewla update` to move to the newest one; see
[Install](../install-daemon.md#update).

Stable tags look like `v0.1.15`; beta tags look like `v0.1.0-beta.22`. The
installer and `mewla update` pick the highest version.

## Known issues

- **iOS**: distributed as a TestFlight beta,
  [Mewla Preview](https://testflight.apple.com/join/nMQheDCE), not yet on the
  App Store. You need Apple's TestFlight app; see
  [Install](../install-daemon.md#ios). As of 2026-11-14, TestFlight has the
  0.3.1 Preview while Apple reviews 0.4.1.
- **Windows**: beta, not yet tested by us on real hardware.
  [Tell us how it went](https://github.com/daoleno/mewla/issues/new?title=Windows%3A%20&body=Windows%20version%3A%0A%0Amewla%20version%3A%0A%0Amewla%20doctor%3A%0A%0AWhat%20happened%3A%0A).
- **Android**: distributed as an APK outside the Play Store. Android asks to
  allow installs from your browser or file manager, and Play Protect may warn.
- **Extensions**: first-time sign-in is not yet available for Slack, and for
  Google Workspace unless your daemon already has Google sign-in configured.
  See [Extensions](../extensions.md#sign-in-availability).
- **macOS daemon**: the Apple Silicon binary is cross-built; report problems you
  hit on a real Mac.
- **Push notifications** are a single best-effort attempt each; a missed alert
  is not retried.

## Release notes

<!-- release-list -->
