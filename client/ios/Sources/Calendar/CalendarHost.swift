import NativeShell
import UIKit

/// `calendar.*`: the phone's own calendar alerts (local notifications), the time zones QuickJS
/// lacks, and the system's date and time pickers.
enum CalendarHost {
  @MainActor
  static func register(_ app: NshAppView) {
    // Replaces the armed alerts with the app's synced set: [{key, itemId, serverId, title, body, atMs, timezone}].
    app.handle("calendar.alertsSchedule") { data -> Any? in
      guard let list = (data as? [String: Any])?["alerts"] as? [Any] else { throw missing("alerts") }
      return await CalendarAlerts.schedule(list.compactMap { ArmedAlert($0) })
    }
    app.handle("calendar.alertsCancelItem") { data -> Any? in
      let itemId = try LiveHost.field(data, "itemId")
      await CalendarAlerts.cancelItem(itemId)
      return nil
    }
    app.handle("calendar.alertsSetStrong") { data -> Any? in
      CalendarAlerts.strong = (data as? [String: Any])?["strong"] as? Bool ?? true
      return nil
    }
    // iOS arms calendar triggers to the second; it has no full-screen alert.
    app.handle("calendar.alertsCapabilities") { _ -> Any? in
      ["exactAlarms": true, "fullScreen": false, "notifications": await PushHost.allowed()] as [String: Any]
    }
    app.handle("calendar.alertsOpenSettings") { _ -> Any? in
      if let url = URL(string: UIApplication.openNotificationSettingsURLString) { _ = await UIApplication.shared.open(url) }
      return nil
    }
    // iOS notifications here carry no Done / Snooze actions: nothing is ever taken with the app closed.
    app.handle("calendar.alertsTakeActions") { _ -> Any? in [Any]() }
    app.handle("calendar.zones") { data -> Any? in
      let o = data as? [String: Any] ?? [:]
      let now = Int64(Date().timeIntervalSince1970 * 1000)
      let from = (o["fromMs"] as? NSNumber)?.int64Value ?? now
      let to = (o["toMs"] as? NSNumber)?.int64Value ?? from + 400 * 86_400_000
      return Zones.table((o["zones"] as? [Any] ?? []).compactMap { $0 as? String }, fromMs: from, toMs: to)
    }
    app.handle("calendar.pickDate") { data, reply in
      let o = data as? [String: Any] ?? [:]
      DatePickerSheet.pickDate(year: int(o["year"]), month: int(o["month"]), day: int(o["day"])) { reply.resolve($0) }
    }
    app.handle("calendar.pickTime") { data, reply in
      let o = data as? [String: Any] ?? [:]
      DatePickerSheet.pickTime(hour: int(o["hour"]), minute: int(o["minute"])) { reply.resolve($0) }
    }
  }

  private static func int(_ value: Any?) -> Int? { (value as? NSNumber)?.intValue }

  private static func missing(_ name: String) -> NSError {
    NSError(domain: "Mewla", code: 2, userInfo: [NSLocalizedDescriptionKey: "\(name) is required"])
  }
}
