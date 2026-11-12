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
client/scripts/fetch-nsh.sh          # NSH 0.5.0 and its Android AARs into $NSH_HOME (default ~/.nsh, checksums pinned); --ios also fetches the framework
cd client && bun test core           # core
cd client/ui && nsh check && nsh test
```

## Run it

- **Android, hot reload:** `client/scripts/dev-android.sh` installs the preview debug build on the one device `adb`
  sees and opens it on `nsh dev` through the start link (tap Load on the device): every save reloads it. The debug build
  takes a dev server only through `mewla-dev://start?server=ws://…`, which it confirms first.
- **Android, a build to install:** `cd client/android && ./gradlew assemblePreviewRelease -Pmewla.abis=arm64-v8a`.
  The preview identity (`com.daoleno.mewla.preview`, "Mewla Preview") installs beside the store app. A local
  build is signed with your debug key; the Preview `client.yml` keeps on dispatch carries the release key
  (`client/scripts/preview-signer.sh`), so each one installs over the last, and its upgrade gate proves it.
- **iOS:** `client/scripts/fetch-nsh.sh --ios && client/scripts/ios-project.sh` (`--dev` links NativeShellDev), then
  open `client/ios/Mewla.xcodeproj`. With `--dev`, launch with `-devServer ws://127.0.0.1:8790/ws` and run
  `nsh dev --server-only client/ui`. Preview builds pass `MEWLA_APP_ID=com.daoleno.mewla.preview`, which also names
  the Live Activity / widget extension (`<id>.live`) and the App Group (`group.<id>`).
- **Web:** `nsh dev --target web client/ui`.

## What the hosts give the app

| Call / event / component | |
|---|---|
| `secure.get` / `secure.set` / `secure.remove` `{key, value}` | The device key and other secrets, in the store and format the Expo app used (Android: expo-secure-store's `SecureStore` prefs and Keystore key; iOS: Keychain service `app:no-auth`). A stored item that can't be read is an error, never `null`. |
| `app.legacyStorage` → entries or `null`, `app.legacyStorageCopied` | The Expo app's AsyncStorage `mewla*` entries, once; `core/legacyStorage.ts` copies them into `Storage` before anything reads it. |
| `links.open` `{url}` (event) | A `mewla://` link the system opened the app with; held until the app listens. |
| `mewla.qrScanner` `{active}` → `scan {value}`, `unavailable {reason}` | The camera's QR reader. |
| `host.params.deviceName` | The name a computer lists this phone under. |
| `push.permission` / `push.requestPermission` / `push.deviceToken`, `push.opened {data}` (event) | Notifications allowed, the system prompt, the FCM / APNs token with the Expo installation id kept where expo-notifications kept it (Android: its uuid file; iOS: its Keychain item), so the Expo token survives the upgrade. A tapped notification, cold start included, is `push.opened`. |
| `live.*` | The live notification (Android) / Live Activity (iOS) and the home-screen widgets of the current computer. iOS: the Expo app's WidgetKit extension (`ios/LiveExtension`, same bundle id, widget kind and activity type), its App Group files, and content-available `mewla_live` pushes applied in the background. |
| `calendar.alerts*`, `calendar.zones`, `calendar.pickDate` / `pickTime` | The phone's own calendar alerts (Android: exact alarms and the lock-screen alert; iOS: local notifications on a calendar trigger pinned to the item's zone), the zone table QuickJS lacks, the system date and time pickers. |

## Upgrade

The version is `ui/nsh.json` `version` (also the daemon's; `scripts/prepare-release.py` moves it). The Android
version code and iOS build number are `major·1 000 000 + minor·10 000 + patch·100 + 99`, or `+ N` for `-beta.N`
(`scripts/lib/release_identity.py`; 0.3.2 is 30299), above every Expo build (142 / 140). The
upgrade gate (`client.yml`, manual dispatch) installs the released Expo APK on an emulator, pairs it with a fresh
daemon and types a draft, installs the release-signed NSH app over it, and passes only if the daemon still lists
that one device and sees it sign in again and the draft is in the new storage
(`client/scripts/upgrade-gate.sh`). On iOS, `ios/Tests/UpgradeTests.swift` reads Keychain items and AsyncStorage
files written in the Expo app's formats; that the App Store build keeps the same Keychain access group is
proven only by a TestFlight upgrade.
