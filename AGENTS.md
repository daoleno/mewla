# Repository Guidelines

Mewla is a mobile-native control plane. Go code is in daemon/ (cmd/mewla, server, auth, work, terminal, watcher); the app is the NSH client in client/ (core: plain TypeScript protocol and state; ui: the NSH app; android and ios: the native hosts; see client/README.md). The terminal's libghostty-vt is pinned in native/terminal-vt/. Product documentation is in docs/; the Pages homepage and docs site are built from site/ and scripts/site-docs/.

## Execution

Read the relevant code and directory instructions before editing. Complete authorized work, preserve unrelated changes, and use existing conventions and typed boundaries. Ask only for a consequential missing decision or new authority; finish independent authorized preparation first. Keep changes scoped and avoid compatibility layers unless requested.

## Product Invariants

- Android and iOS share every product feature by default; web is outside scope unless requested. The first reproducing platform sets test priority, not the solution boundary. Isolate genuinely platform-exclusive capabilities behind a shared contract with explicit counterpart behavior.
- Exactly one server is current. Sessions, Brain, Terminal, Calendar, Work, Skills and related UI use that canonical owner. Do not aggregate servers, add feature-local selection or invent fallbacks. On a server switch, rebind or clear old-server state.
- client/core never imports nsh; what the person sees lives in client/ui.
- Preserve auth, pairing, WebSocket, upload and filesystem boundaries. Never commit secrets, pairing links, daemon state, tunnel URLs or .env.local.

## Commands And Verification

- Install: client/scripts/fetch-nsh.sh (NSH into ~/.nsh); bun install (only the Screen viewer's noVNC).
- App: cd client && bun test core; cd client/ui && ~/.nsh/bin/nsh check && ~/.nsh/bin/nsh test after TS/TSX edits.
- Android: client/scripts/dev-android.sh (hot reload), or cd client/android && ./gradlew assemblePreviewDebug (Java 17; libghostty-vt from scripts/build-libghostty.sh). iOS: client/scripts/ios-project.sh, then Xcode.
- Daemon: bun run daemon:build; cd daemon && go test ./... before daemon/protocol/auth/terminal/work/session changes.
- cd daemon && go run ./cmd/mewla-dev starts a hot-reloading daemon. Editing watched Go files can restart it; check live watchers before changes.
- While developing, run the tests your change touches directly (`go test` for the package, `bun test` for the file, `nsh test` for the UI). Push; main's CI is the gate, and nobody waits on it while developing. If main goes red after your merge, fix it at once; if it is already red from someone else's commit, say so and never pile onto it.
- Terminal drawing (client/ui/terminal, client/core/terminal, client/native/terminal, native/terminal-vt): `scripts/terminal-corpus/run.sh` (add `--android` for the lane phone). Chrome extension (daemon/agentuse/extension): `cd daemon && go test ./agentuse`.
- Test changed behavior and required repository gates. Broaden or repeat only after edits, failures or unresolved concerns. Use device/UI evidence for visible changes; report platform limitations.

## Proving A Change

Pick the cheapest lane that shows what the change touches (details: client/README.md, "Proving a change"):

1. UI change: `client/scripts/shots.sh` (one image: phone 390 light and dark, desktop 1280) plus `nsh test` of the touched files. No device.
2. The warm phone only for the Kotlin/Swift host, a manifest or intent filter, a permission, a notification, the keyboard or insets, the app lifecycle or the camera: `client/scripts/phone.sh`, minutes, then `down`. Never a new AVD, never a clean release build.
3. A fault the owner found gets its test in the fixing commit: headless if it can be, a `scripts/shots/` scene if it is layout, a `phone.sh` flow if only Android shows it.
4. Do not ask for, or run per change, "exact release build, fresh install, first run, real QR": that is the release gate, once per release, run by CI. No emulator per task, no screenshot per tap.
5. The report carries one composed image and the command that made it, not a gallery.
6. Do not wait on remote CI during work; one green run before main or a release.
7. Anything that takes the heavy lock (Gradle builds, not the phone lane) does so for minutes, by script, and lets go. The phone lane has its own lock and a memory check.
8. A new journey is written as a file (a `shots` scene, a `phone.sh` flow), not performed by hand and photographed.

## Style And Delivery

Use TypeScript, two-space indentation in TS/TSX, gofmt for Go, PascalCase components, use-prefixed hooks and camelCase functions. Keep terminal UI in client/ui/terminal and its model in client/core/terminal. Use structured APIs instead of string parsing when available.

Update relevant product docs. When authorized to commit, use short imperative messages and scoped commits. Report behavior changes, verification and remaining risks; include screenshots for visible UI changes. Do not publish or deploy without authorization.
