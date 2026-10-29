import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const moduleRoot = import.meta.dir;
const targetRoot = join(moduleRoot, "../../targets/mewla-live");
const read = (path: string) => readFileSync(path, "utf8");

describe("MewlaLive iOS native contract", () => {
  test("the app module and the widget extension compile the same state model", () => {
    // ActivityKit decodes a push's content-state with the extension's copy of the type, and the app
    // encodes it with its own: one wire shape only holds if the two files are the same file.
    expect(read(join(moduleRoot, "ios/MewlaLiveState.swift"))).toBe(
      read(join(targetRoot, "MewlaLiveState.swift")),
    );
  });

  test("the Swift state decodes every field the daemon projection sends", () => {
    const swift = read(join(moduleRoot, "ios/MewlaLiveState.swift"));
    const daemon = read(join(moduleRoot, "../../../daemon/live/live.go"));
    const wireKeys = [...daemon.matchAll(/`json:"([a-z_]+)(?:,omitempty)?"`/g)].map((match) => match[1]);
    for (const key of ["kind", "epoch", "seq", "run_key", "title", "phase", "started_at", "done", "total", "workers", "worker_count", "needs_count", "question", "choices", "work_id"]) {
      expect(wireKeys).toContain(key);
    }
    // work_id is a Work's id the phone does not need; every other key is decoded under its wire name.
    for (const key of wireKeys.filter((key) => key !== "work_id" && key !== "name")) {
      const camel = key.replace(/_([a-z])/g, (_, letter: string) => letter.toUpperCase());
      expect(swift.includes(`"${key}"`) || new RegExp(`\\b${camel}\\b`).test(swift)).toBe(true);
    }
  });

  test("the module exposes the same calls as the Android module", () => {
    const swift = read(join(moduleRoot, "ios/MewlaLiveModule.swift"));
    const kotlin = read(
      join(moduleRoot, "android/src/main/java/expo/modules/mewlalive/MewlaLiveModule.kt"),
    );
    for (const name of ["isSupported", "apply", "setCurrentServer", "configureWidget", "configureWidgetPushed", "clearWidget", "refreshWidgets", "widgetState", "diagnostics"]) {
      expect(kotlin).toContain(`"${name}"`);
      expect(swift).toContain(`"${name}"`);
    }
  });

  test("the background push path is diagnosable from the device console", () => {
    const delegate = read(join(moduleRoot, "ios/MewlaLiveAppDelegate.swift"));
    // iOS only calls didReceiveRemoteNotification for a content-available push, so a device test
    // needs to tell apart "never called" from "we dropped it": every branch logs, and none of
    // them can log the question text.
    for (const marker of ["received state=", "not a live_state push", "dropped: another server", "skipped ", "applied "]) {
      expect(delegate).toContain(marker);
    }
    expect(delegate).toContain('NSLog("MewlaLive push: %@", message)');
    expect(delegate).not.toContain("question.text");
    expect(delegate).not.toContain("message.state.title");
  });

  test("no NSLock is taken inside an async Task (an error in Swift 6)", () => {
    const swift = read(join(moduleRoot, "ios/MewlaLiveModule.swift"));
    // Every `Task { ... }` body in the module; none may touch the lock directly.
    for (const match of swift.matchAll(/Task(?: \[weak self\] in)? \{([\s\S]*?)\n    \}\n/g)) {
      expect(match[1]).not.toMatch(/\block\.(lock|unlock)\(\)/);
    }
    expect(swift).toContain("private func markObserved");
    expect(swift).toContain("private func forgetObserved");
  });

  test("a chip opens the app on its question with the link the app already routes", () => {
    const views = read(join(targetRoot, "MewlaLiveViews.swift"));
    expect(views).toContain('components.scheme = "mewla"');
    expect(views).toContain('components.host = "live"');
    expect(views).toContain('components.path = "/answer"');
    expect(views).toContain('URLQueryItem(name: "q"');
    expect(views).toContain('URLQueryItem(name: "c"');
  });

  test("the extension's identifiers come from the closed iOS identity", () => {
    const { IOS_IDENTITIES } = require("../../iosIdentity");
    const state = read(join(moduleRoot, "ios/MewlaLiveState.swift"));
    // The Swift side derives group.<app bundle id> from the extension's own id; that only works if
    // every identity keeps the "<app>.live" / "group.<app>" shape.
    for (const identity of Object.values(IOS_IDENTITIES) as Array<Record<string, string>>) {
      expect(identity.liveExtensionBundleIdentifier).toBe(`${identity.bundleIdentifier}.live`);
      expect(identity.appGroup).toBe(`group.${identity.bundleIdentifier}`);
    }
    expect(state).toContain('static let extensionSuffix = ".live"');
    expect(state).toContain('"group." + appBundleId');
  });
});

describe("MewlaLive Android calendar alerts contract", () => {
  const androidMain = join(moduleRoot, "android/src/main");
  const manifest = read(join(androidMain, "AndroidManifest.xml"));
  const kotlin = (name: string) => read(join(androidMain, "java/expo/modules/mewlalive", name));

  test("the module exposes exactly the calls the app uses", () => {
    const module = kotlin("MewlaLiveModule.kt");
    const names = [...module.matchAll(/Function\("(calendarAlerts\w+)"\)/g)].map((match) => match[1]).sort();
    expect(names).toEqual([
      "calendarAlertsCancelItem",
      "calendarAlertsCapabilities",
      "calendarAlertsOpenFullScreenSettings",
      "calendarAlertsSchedule",
      "calendarAlertsSetForeground",
      "calendarAlertsSetStrong",
      "calendarAlertsTakeActions",
    ]);
  });

  test("the manifest declares the permissions and components alerts need", () => {
    for (const permission of ["USE_EXACT_ALARM", "USE_FULL_SCREEN_INTENT", "RECEIVE_BOOT_COMPLETED", "VIBRATE", "POST_NOTIFICATIONS"]) {
      expect(manifest).toContain(`android.permission.${permission}`);
    }
    expect(manifest).toMatch(/SCHEDULE_EXACT_ALARM"\s+android:maxSdkVersion="32"/);
    for (const component of [".CalendarAlarmReceiver", ".CalendarActionReceiver", ".CalendarBootReceiver", ".CalendarAlertActivity"]) {
      expect(manifest).toContain(`android:name="${component}"`);
    }
    for (const action of ["BOOT_COMPLETED", "MY_PACKAGE_REPLACED", "TIME_SET", "TIMEZONE_CHANGED"]) {
      expect(manifest).toContain(`android.intent.action.${action}`);
    }
  });

  test("only the alert activity shows over the lock screen, on its own task", () => {
    const activity = manifest.slice(manifest.indexOf(".CalendarAlertActivity"));
    const tag = activity.slice(0, activity.indexOf("/>"));
    for (const attribute of ['android:showWhenLocked="true"', 'android:turnScreenOn="true"', 'android:taskAffinity=""', 'android:excludeFromRecents="true"', 'android:exported="false"']) {
      expect(tag).toContain(attribute);
    }
    // The main activity is the app's, and must never be shown over the lock screen by this module.
    expect(manifest).not.toContain("MainActivity");
  });

  test("alarms are exact when allowed and never an alarm clock", () => {
    const alerts = kotlin("CalendarAlerts.kt");
    expect(alerts).toContain("setExactAndAllowWhileIdle");
    expect(alerts).toContain("setAndAllowWhileIdle");
    expect(alerts).not.toContain("setAlarmClock(");
  });

  test("a push for a calendar alert is handled before the live and Expo paths", () => {
    const service = kotlin("MewlaFirebaseMessagingService.kt");
    expect(service.indexOf("extractCalendar(remoteMessage.data)")).toBeGreaterThan(0);
    expect(service.indexOf("extractCalendar(remoteMessage.data)")).toBeLessThan(service.indexOf("extractLive(remoteMessage.data)"));
  });
});
