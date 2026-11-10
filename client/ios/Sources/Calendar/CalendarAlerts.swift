import Foundation
import UserNotifications

/// One alert the app armed, core/calendar/alerts.ts's `ArmedAlert`.
struct ArmedAlert: Equatable {
  let key: String
  let itemId: String
  let serverId: String
  let title: String
  let body: String
  let atMs: Double
  let timezone: String

  init(key: String, itemId: String, serverId: String, title: String, body: String, atMs: Double, timezone: String) {
    self.key = key
    self.itemId = itemId
    self.serverId = serverId
    self.title = title
    self.body = body
    self.atMs = atMs
    self.timezone = timezone
  }

  init?(_ value: Any) {
    guard let o = value as? [String: Any], let key = o["key"] as? String, !key.isEmpty,
          let itemId = o["itemId"] as? String, let atMs = (o["atMs"] as? NSNumber)?.doubleValue else { return nil }
    self.init(key: key, itemId: itemId, serverId: o["serverId"] as? String ?? "", title: o["title"] as? String ?? "",
              body: o["body"] as? String ?? "", atMs: atMs, timezone: o["timezone"] as? String ?? "")
  }
}

/// The phone's own calendar alerts: local notifications on a calendar trigger, so they fire with
/// the computer or the network gone. Each is identified by its alert key; the daemon's push for
/// the same alert carries that key, and the app in front shows its own prompt for either.
enum CalendarAlerts {
  static let strongKey = "mewla.calendar.strongAlerts"
  /// iOS keeps the soonest 64 pending local notifications of an app and drops the rest.
  static let limit = 64

  /// "Strong calendar alerts": Time Sensitive, which breaks through Focus where the app holds the
  /// entitlement (iOS delivers an ordinary alert otherwise). On unless turned off.
  static var strong: Bool {
    get { UserDefaults.standard.object(forKey: strongKey) as? Bool ?? true }
    set { UserDefaults.standard.set(newValue, forKey: strongKey) }
  }

  /// The wall clock of `atMs` in the item's zone, with the zone set: the instant holds if the phone
  /// changes zone. Gregorian whatever the phone's calendar, as the app's dates are.
  static func triggerComponents(atMs: Double, timezone: String) -> DateComponents {
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = TimeZone(identifier: timezone) ?? .current
    var parts = calendar.dateComponents([.year, .month, .day, .hour, .minute, .second], from: Date(timeIntervalSince1970: atMs / 1000))
    parts.calendar = calendar
    parts.timeZone = calendar.timeZone
    return parts
  }

  static func request(_ alert: ArmedAlert, strong: Bool) -> UNNotificationRequest {
    let content = UNMutableNotificationContent()
    content.title = alert.title
    content.body = alert.body
    content.sound = .default
    content.userInfo = ["screen": "calendar", "calendar_id": alert.itemId, "server_id": alert.serverId, "alert_key": alert.key]
    content.interruptionLevel = strong ? .timeSensitive : .active
    let trigger = UNCalendarNotificationTrigger(dateMatching: triggerComponents(atMs: alert.atMs, timezone: alert.timezone), repeats: false)
    return UNNotificationRequest(identifier: alert.key, content: content, trigger: trigger)
  }

  /// A calendar notification of this feature, this app's or one the Expo app scheduled (data under `body`).
  static func isCalendar(_ userInfo: [AnyHashable: Any], item: String? = nil) -> Bool {
    let data = PushData.of(userInfo)
    guard data["screen"] == "calendar" else { return false }
    return item == nil || data["calendar_id"] == item
  }

  /// Replaces every pending calendar alert with these (the soonest 64 still ahead); how many are armed.
  static func schedule(_ alerts: [ArmedAlert], now: Date = Date()) async -> Int {
    let center = UNUserNotificationCenter.current()
    let nowMs = now.timeIntervalSince1970 * 1000
    let wanted = Array(alerts.filter { $0.atMs > nowMs }.sorted { $0.atMs < $1.atMs }.prefix(limit))
    let keys = Set(wanted.map(\.key))
    let stale = await center.pendingNotificationRequests()
      .filter { isCalendar($0.content.userInfo) && !keys.contains($0.identifier) }
      .map(\.identifier)
    center.removePendingNotificationRequests(withIdentifiers: stale)
    let strong = self.strong
    var armed = 0
    // Adding an identifier already pending replaces it.
    for alert in wanted {
      do {
        try await center.add(request(alert, strong: strong))
        armed += 1
      } catch {
        NSLog("Mewla calendar: cannot arm an alert: %@", error.localizedDescription)
      }
    }
    return armed
  }

  /// The item's armed alerts and the ones on screen: answered, nothing of it may still ring.
  static func cancelItem(_ itemId: String) async {
    let center = UNUserNotificationCenter.current()
    let pending = await center.pendingNotificationRequests().filter { isCalendar($0.content.userInfo, item: itemId) }.map(\.identifier)
    center.removePendingNotificationRequests(withIdentifiers: pending)
    let shown = await center.deliveredNotifications().filter { isCalendar($0.request.content.userInfo, item: itemId) }.map(\.request.identifier)
    center.removeDeliveredNotifications(withIdentifiers: shown)
  }
}
