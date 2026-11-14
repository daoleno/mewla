# iOS app

The iOS app is the NSH client's xcodegen host in `client/ios` (see
[client/README.md](../../client/README.md)). It is available as a source build.

Mewla does not currently claim a generally available App Store build. The repository includes macOS CI for the client's Simulator app (`client.yml`) and a manual, credential-gated signed IPA/TestFlight workflow (`ios-release.yml`). A physical-device source install requires an Apple development team and signing configuration.

## TestFlight Preview access

The [TestFlight Preview public invitation](https://testflight.apple.com/join/rTKCDzMt) is configured. The first external build is awaiting Apple Beta App Review, so the link does not yet guarantee that a new tester can install the Preview. Source builds remain available independently of TestFlight.

## Supported mobile interfaces

The agent interface is the shared NSH UI in `client/ui`, not an Android-only feature. Codex, Claude Code, Cursor Agent, Grok, Pi, and OpenCode sessions open in the structured Chat interface when transcript events are available; the top-bar action switches between Chat and Terminal, and Terminal attaches to the same daemon/tmux session through the platform's Ghostty bridge.

Platform-specific implementation is deliberately limited to native plumbing:

- Android loads `libghostty_vt.so` through JNI (`client/android/app/src/main/cpp/jni_bridge.cpp`).
- iOS links `GhosttyVt.xcframework` statically through a C++ bridge (`client/ios/Sources/Terminal/TerminalVt.cpp`).
- Keyboard inset behavior differs by platform, while the accessory keys and terminal controller are shared.

## Prerequisites

- Apple Silicon Mac
- Full Xcode selected with `xcode-select`
- An installed iOS Simulator runtime, or a signed physical device target
- xcodegen (`brew install xcodegen`)
- NSH and its iOS framework: `client/scripts/fetch-nsh.sh --ios`
- Network access for the first pinned Zig/Ghostty build

The native build script downloads the pinned Zig release when the active `zig` is not the version in `native.lock.json`. Do not substitute an incompatible newer Zig release.

## Build the native terminal

From the repository root:

```bash
./scripts/build-libghostty-ios.sh
./scripts/verify-libghostty-ios.sh
```

This creates the ignored native output under:

```text
native/terminal-vt/libs/ios/GhosttyVt.xcframework
```

The verifier checks the pinned Ghostty release, checksums, headers/module map, an arm64 iOS-device slice, and an arm64 iOS-Simulator slice. `xcodebuild MEWLA_GHOSTTY=<dir>` links one from elsewhere.

## Generate and run the Xcode project

```bash
client/scripts/fetch-nsh.sh --ios
client/scripts/ios-project.sh          # --dev links NativeShellDev for hot reload
open client/ios/Mewla.xcodeproj
```

With `--dev`, launch with `-devServer ws://127.0.0.1:8790/ws` and run `nsh dev --server-only client/ui`. Preview builds pass `MEWLA_APP_ID=com.daoleno.mewla.preview`.

The app uses bundle identifier `com.daoleno.mewla` and declares local-network access because Mewla connects directly to a self-hosted daemon. Camera access is used for pairing QR codes; microphone access is not requested.

## Pair and validate

Start a reachable daemon, create a fresh pairing link, and import it in the iOS app:

```bash
mewla --lan
# In another terminal, run the LAN or Tailscale pair command Mewla prints.
```

For Simulator testing on the same Mac, a locally reachable address can be used. For a physical iPhone, use `mewla --lan` with the Mac's printed LAN/Tailscale address on a trusted private network, or use an HTTPS origin that forwards every required route to bare `mewla`. See [Connect and pair](../connect-and-pair.md).

An end-to-end terminal check should cover:

1. Pair and reconnect without reusing an expired enrollment token.
2. Confirm Codex, Claude Code, Cursor Agent, Grok, Pi, or OpenCode sessions appear in Sessions.
3. Open a structured Worker session and verify Chat is the default interface.
4. Toggle to Terminal and attach to its tmux session.
5. Confirm existing output, cursor, colors, and tmux status are rendered.
6. Enter a command and confirm shell output returns to the same terminal surface.
7. Background/foreground the app and confirm it resumes the connection and mounted surface.

## Native terminal / XCFramework contract

iOS uses the same [terminal row-update pipeline](terminal-rendering.md) as Android.

The machine-readable source of truth is:

[`native/terminal-vt/native.lock.json`](../../native/terminal-vt/native.lock.json)

Important invariants:

1. The libghostty-vt release (signed tarball sha256, dependency tarball sha256, header digest) and the Zig version are pinned in the lock; the build never uses a git checkout. Every XCFramework slice must carry the pinned headers.
2. `GhosttyVt.xcframework`, checksums, build manifest, and copied license notice are generated and gitignored.
3. The XCFramework must contain arm64 device and arm64 Simulator slices.
4. `client/ios/project.yml` links it from `$(MEWLA_GHOSTTY)/libs/ios` (default `native/terminal-vt`) and stops the build with a message when it is missing.
5. Ghostty's MIT notice (`client/ui/more/settings/licenses/texts/Ghostty-MIT.txt`) is copied to bundle-root `GHOSTTY-MIT.txt` by project.yml's "Bundle the Ghostty notice" step and enforced by `scripts/verify-ios-artifact.sh`.
6. App Transport Security is intentional: `NSAllowsLocalNetworking=true` for self-hosted LAN/tailnet daemons, and `NSAllowsArbitraryLoads=false` so cleartext internet remains denied.

## Verification

After native changes, run:

```bash
./scripts/verify-libghostty-ios.sh
./scripts/verify-libghostty.sh --contract
cd client/ui && ~/.nsh/bin/nsh check && ~/.nsh/bin/nsh test
```

Then perform a real Xcode build-and-run (client.yml runs the unit tests and launches the Simulator app). Static checks do not replace launching the app because Swift ABI, signing, dyld, local-network permission, and native registration failures only appear in the Apple runtime path.

## Current distribution status

The source-build path is working, but a bare clone does not contain the ignored Ghostty XCFramework. CI rebuilds/checksums it and builds the client's Simulator app. The protected Preview workflow has signed, exported, verified, and uploaded an IPA to App Store Connect; Apple Beta App Review remains the gate before public-link testers can install it. Mewla does not claim a generally available App Store build. See [iOS CI and release automation](../ios-ci-release.md).
