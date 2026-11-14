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
- Before pushing to main, run `scripts/ci-local.py` (the jobs your diff touches; `--all` for everything). It runs the commands of .github/workflows/ci.yml, so it also fails on a stale client/ui/more/settings/licenses/licenses.json or Screen viewer bundle, or a node_modules that differs from bun.lock (bun install does not prune; delete the listed directories from node_modules). After pushing, wait for the CI run on your commit to go green before calling the work done. If main is already red from someone else's commit, say so and do not pile on.
- Test changed behavior and required repository gates. Broaden or repeat only after edits, failures or unresolved concerns. Use device/UI evidence for visible changes; report platform limitations.

## Style And Delivery

Use TypeScript, two-space indentation in TS/TSX, gofmt for Go, PascalCase components, use-prefixed hooks and camelCase functions. Keep terminal UI in client/ui/terminal and its model in client/core/terminal. Use structured APIs instead of string parsing when available.

Update relevant product docs. When authorized to commit, use short imperative messages and scoped commits. Report behavior changes, verification and remaining risks; include screenshots for visible UI changes. Do not publish or deploy without authorization.
