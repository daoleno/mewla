# Mewla client (NSH)

The app on [NSH](https://nsh.dev): one TSX app with native views on iOS and Android, and the same TSX on the web.
It replaces the Expo app in one release, in place: same package and signing key on Android, same bundle id on iOS,
so a phone keeps its pairing and its drafts. The terminal links libghostty-vt from `native/terminal-vt/` (built by
`scripts/build-libghostty.sh` and `scripts/build-libghostty-ios.sh`, pinned by its `native.lock.json`).

| Directory | What it is | Owns |
|---|---|---|
| `core/` | Plain TypeScript: pairing, device auth, the transport, stored computers, the upgrade copy. Never imports `nsh`; `bun test core`. | Protocol and state |
| `ui/` | The NSH app (`nsh.json`, `App.tsx`): the TabBar shell (Brain · Sessions · Calendar · More), screens, `platform.ts`. `nsh check`, `nsh test`. | Everything the person sees |
| `android/` | Gradle host: `NshView`, the Keystore secrets, the camera reader, `mewla://` links. Bundles `ui/` with `nsh build`. | Android-only capabilities |
| `ios/` | xcodegen host: `NshAppView`, the Keychain, the camera reader, links. Bundles `ui/` the same way. | iOS-only capabilities |
| `scripts/` | Pinned NSH install, hot reload, the upgrade gate. | |

## Set up

```sh
client/scripts/fetch-nsh.sh          # NSH 0.6.1 and its Android AARs into $NSH_HOME (default ~/.nsh, checksums pinned); --ios also fetches the framework
cd client && bun test core           # core
cd client/ui && nsh check && nsh test
```

## Run it

- **Android, hot reload:** `client/scripts/dev-android.sh` builds the Dev app (preview debug) with this host's `nsh dev`
  server (`-Pmewla.devServer=ws://<LAN address>:8790/ws`: `DEV_HOST`, or the private LAN address `lan-address.sh` picks and prints,
  never a proxy's or VPN's tunnel; with none it stops before building), installs it on the one device `adb` sees and serves client/.
  Mewla Dev opens on that server from its icon, asking once (Load); every save reloads it. When the server does not
  answer within two seconds, it starts on its built-in bundle and reopens on the server once it answers and the app
  comes to the front. `mewla-dev://start?server=ws://…` points it at another server, confirmed first.
- **Android, a build to install:** `cd client/android && ./gradlew assemblePreviewRelease -Pmewla.abis=arm64-v8a`.
  The preview identity (`com.daoleno.mewla.preview`, "Mewla Preview") installs beside the store app. A local
  build is signed with your debug key; the Preview `client.yml` keeps on dispatch carries the release key
  (`client/scripts/preview-signer.sh`), so each one installs over the last, and its upgrade gate proves it.
- **iOS:** `client/scripts/fetch-nsh.sh --ios && client/scripts/ios-project.sh` (`--dev` links NativeShellDev), then
  open `client/ios/Mewla.xcodeproj`. With `--dev`, launch with `-devServer ws://127.0.0.1:8790/ws` and run
  `nsh dev --server-only client/ui`. Preview builds pass `MEWLA_APP_ID=com.daoleno.mewla.preview`, which also names
  the Live Activity / widget extension (`<id>.live`) and the App Group (`group.<id>`).
- **Web:** `nsh dev --target web client/ui`.

## Proving a change

Pick the cheapest lane that can show what the change touches. Nobody starts an emulator of their own, builds a release
APK, or walks first run and pairing by hand to prove a change: that is the release gate, once per release, run by CI.

| The change touches | Prove it with | Cost |
|---|---|---|
| Logic, state, protocol (`core/`, `ui/` behaviour, the daemon) | `bun test core`, `cd client/ui && nsh check && nsh test <touched files>` (whole `nsh test` before main), `go test` for the daemon | seconds; 0 model tokens |
| What a screen looks like (layout, copy, dark mode, a narrow or short window) | `client/scripts/shots.sh`: the app on its fixture computer in headless Chrome, no device | 3 s a picture |
| How the terminal draws (the packer, glyphs, the web page's canvas, `ui/terminal`, `core/terminal`) | `scripts/terminal-corpus/run.sh`: every corpus screen in Chromium and Firefox with pixel checks, one PASS or FAIL per engine; `--android` adds the lane phone | 40 s (`--android`: +70 s, the lane phone booted and shut down); CI runs it with WebKit when the terminal's paths change |
| What only Android shows (intent filters and the share sheet, permissions, notifications, keyboard and insets, lifecycle, the camera) | one shared emulator, see below | minutes; no queue unless another lane emulator runs |
| Install, first run, every pairing route including the scanned QR, upgrade from the last release | the release gate (`client.yml`, `release-artifacts.yml`, the daily upgrade test): CI, on the exact artifact | not per change |

**Lane 1, `client/scripts/shots.sh`.** `client/scripts/shots.sh` runs the tour in `client/scripts/shots/tour.js` (Brain,
the computer page, Calendar) and prints a directory holding one PNG per scene: phone 390 wide light, the same dark and
desktop 1280 wide, side by side in one image. Your own scenes are plain `nsh test` steps in a file
(`client/scripts/shots.sh my.js`) or inline (`-e 'await app.press("id:home-cat"); await shot("computer")'`; `snap(name)`
takes the current viewport only, `app.setViewport(390, 520)` makes a short window). Put that one image in your report, not
a gallery. It is not a phone: no status bar, navigation bar or keyboard, the browser's fonts, `platform.os` is "web".

**Lane 2, the warm phone, `client/scripts/phone.sh`.** One AVD, `mewla-lane`, with the Dev app paired to an isolated
test daemon of its own and saved as a snapshot; never create another AVD and never use the live daemon or the port of
the owner's dev server (8790). It has its own lock (`~/.mewla/lane/emulator.lock`: one lane emulator at a time) and does
not use the heavy lock; `up` refuses with one line when less than 8 GiB of memory is available or a lane emulator already
runs, and `down` (or the 30-minute cap) stops it. Only the Gradle build of `phone.sh install` takes the heavy lock.
`client/scripts/phone.sh -h` lists the verbs: `reset` (the app stopped and started again, Brain open and connected: 6–10 s), `restore` (the phone back to the snapshot: 11–14 s, but about 1 in 20 takes 50–150 s waiting for the app to show Brain after the restore, cause not found, so `reset` is the default), `shot`, `tap`,
`dump` (the screen as text), `type`, `shows`, `share-file` / `share-text` (a share from another app, about 1 s), `corpus OUT` (the terminal corpus in the Dev app's TerminalView, one PNG per screen; `scripts/terminal-corpus/run.sh --android` checks it), `install`
(incremental debug build), `run VERB -- VERB...` (up, the verbs, down). `reset` does not put the phone back (drafts, files and the lane daemon's message thread stay), so a flow sends fresh text and names. `phone.sh flow all` runs the journeys in `client/scripts/flows/` (send a
message, open Calendar, share a picture in), each after a `reset`; they are the local phone lane and part of the release
gate, not a per-push CI job. `open-with` is untested. adb must stay the shell user: a share started as root is refused ("Cannot read this file"), and the script stops with that message.

A fault the owner found gets its test in the fixing commit: headless if it can be, a scene in `scripts/shots/` if it is
layout, a `phone.sh` flow if only Android shows it. What no lane sees (Screen, the QR camera) has its own checks, see AGENTS.md. The
Chrome extension's service worker is run against a stand-in `chrome` in `go test ./agentuse` (`TestExtensionBackground`,
`TestSiteDataGuard`: node, no browser, under a second).

**What runs when.**
- Per change: the lanes above, nothing else (`bun test core`, `nsh check` / `nsh test`, `go test`, `shots.sh`, `terminal-corpus/run.sh`, `phone.sh` for what only Android shows). CI on main runs its fast checks; the terminal corpus only when the terminal's paths change.
- Per release (`release-artifacts.yml`, on the exact signed APK): the launch smoke with the scanner (`release-launch.sh`), the flow replay (`release-flows.sh`: `phone.sh flow all` on the release build, paired to the release's own daemon), and the first-run + scanned-QR walk, which stays a person's.
- Daily (`upgrade.yml`): `release-upgrade.sh`, the latest release upgraded to main.
- On dispatch of `client.yml` with `gate`: the Expo leg (`upgrade-gate.sh`: Expo v0.3.1 → NSH, the device key, the stored computers and the draft come over) and Preview → Preview (`preview-upgrade.sh`).

## A call into JS has 2 s

NSH cuts off a call into JS that runs past 2 s (`Error: interrupted`). A `computed` whose run is cut off, or
throws for any other reason, never told what reads it of a change again while it lived, up to NSH 0.5.1 (0.6.0 fixed it,
longbridge/nsh#2065; `ui/kit/derived.test.tsx` pins the fix). So what a thread's rows are made from is `derived` (`ui/kit/derived.ts`, an effect's
value: a failed run keeps the last value and the next change makes it again), never `computed`: on 11-14 one slow
update froze Brain's thread until the app started again (`ui/chat/ThreadCutOff.test.tsx`).

Keep each call well inside the 2 s: a phone runs it some 8 times slower than a dev box. `ui/chat/OpenBudget.test.tsx`
opens the app on a computer shaped as the owner's (`ui/testing/ownerShape.ts`) and fails when one call that hands the
app its messages passes 100 ms. What a thread or a Session list shows is made once per event or Session object and kept
(`activityOf`, `sessionRowModel`, `primarySemanticAction`), not again on every change.

## What the hosts give the app

| Call / event / component | |
|---|---|
| `secure.get` / `secure.set` / `secure.remove` `{key, value}` | The device key and other secrets, Android: in `SecureStore` prefs under a Keystore key (expo-secure-store's format, kept so existing installs keep their key); iOS: Keychain service `app:no-auth`. A stored item that can't be read is an error, never `null`. |
| `links.open` `{url}` (event) | A `mewla://` link the system opened the app with; held until the app listens. |
| `mewla.qrScanner` `{active}` → `scan {value}`, `unavailable {reason}` | The camera's QR reader. |
| `host.params.deviceName` | The name a computer lists this phone under. |
| `push.permission` / `push.requestPermission` / `push.deviceToken`, `push.opened {data}` (event) | Notifications allowed, the system prompt, the FCM / APNs token with the install's own id, the `deviceId` of the Expo push token exchange (Android: a uuid file in the app's no-backup directory; iOS: its Keychain item), made on first use. A tapped notification, cold start included, is `push.opened`. |
| `live.*` | The live notification (Android) / Live Activity (iOS) and the home-screen widgets of the current computer. iOS: the Expo app's WidgetKit extension (`ios/LiveExtension`, same bundle id, widget kind and activity type), its App Group files, and content-available `mewla_live` pushes applied in the background. |
| `share.take` → shares, `share.ack {ids}`, `share.release {uris}`, `share.arrived` (event) | Android: what other apps share (the share sheet) or open with Mewla (ACTION_VIEW of a content:// or file:// URI), copied into the app's cache at once because the sender's read grant ends with the activity. `share.take` lists the shares not yet acknowledged (`{id, text, problem, assets: [UploadDocumentAsset]}`, each asset's `uri` a file:// copy that `files.upload` streams); the app acknowledges a share once its files are chips in Brain's composer and gives each copy back when its chip goes. A share waits on disk until acknowledged; a start deletes what no chip holds. `ui/more/files/shareIn.ts`. |
| `calendar.alerts*`, `calendar.zones`, `calendar.pickDate` / `pickTime` | The phone's own calendar alerts (Android: exact alarms and the lock-screen alert; iOS: local notifications on a calendar trigger pinned to the item's zone), the zone table QuickJS lacks, the system date and time pickers. |

## Upgrade

The version is `ui/nsh.json` `version` (also the daemon's; `scripts/prepare-release.py` moves it). The Android
version code and iOS build number are `major·1 000 000 + minor·10 000 + patch·100 + 99`, or `+ N` for `-beta.N`
(`scripts/lib/release_identity.py`; 0.3.2 is 30299). Upgrades are proved by `release-upgrade.sh` (the previous release's
APK pairs with its daemon and keeps a draft, this one installs over it: still paired, same device, draft kept; daily, `upgrade.yml`) and, on
dispatch of `client.yml`, by Preview → Preview (`preview-upgrade.sh`). Nothing upgrades from the Expo app any more (owner, 2026-11-21: no
one runs an Expo build): its AsyncStorage and push-id readers are gone. A device key made by an Expo build is still read where
`SecureStore.kt` finds it, because an install that began as an Expo build may hold its key in that form.
