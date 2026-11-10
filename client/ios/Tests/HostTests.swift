import Security
import UserNotifications
import XCTest
@testable import Mewla

/// The Expo installation id, read where expo-notifications kept it: the Expo push token the
/// daemon knows this phone by stays the same across the upgrade.
final class InstallationIdTests: XCTestCase {
  private let key = Data("EXDeviceInstallationUUIDKey".utf8)

  override func setUp() { clear() }
  override func tearDown() { clear() }

  private func clear() {
    SecItemDelete(search() as CFDictionary)
    UserDefaults.standard.removeObject(forKey: "EXDeviceInstallationUUIDKey")
  }

  /// expo-notifications' own query (ServerRegistrationModule.keychainSearchQueryFor).
  private func search() -> [String: Any] {
    [
      kSecClass as String: kSecClassGenericPassword,
      kSecAttrService as String: Bundle.main.bundleIdentifier!,
      kSecAttrGeneric as String: key,
      kSecAttrAccount as String: key,
    ]
  }

  /// expo-notifications' own write (storeStringWithQueries: delete, then add).
  private func expoWrite(_ id: String) {
    SecItemDelete(search() as CFDictionary)
    var add = search()
    add[kSecValueData as String] = Data(id.utf8)
    add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
    XCTAssertEqual(SecItemAdd(add as CFDictionary, nil), errSecSuccess, "the Keychain refused the write (unsigned build?)")
  }

  private func stored() -> (id: String, accessible: String)? {
    var query = search()
    query[kSecMatchLimit as String] = kSecMatchLimitOne
    query[kSecReturnData as String] = true
    query[kSecReturnAttributes as String] = true
    var item: CFTypeRef?
    guard SecItemCopyMatching(query as CFDictionary, &item) == errSecSuccess, let found = item as? [String: Any],
          let data = found[kSecValueData as String] as? Data, let id = String(data: data, encoding: .utf8) else { return nil }
    return (id, found[kSecAttrAccessible as String] as? String ?? "")
  }

  func testTheIdExpoNotificationsKeptReadsBack() throws {
    expoWrite("6F1C2B9A-0D3E-4B7A-9E21-5C8D4F3A2B10")
    XCTAssertEqual(try InstallationId.get(), "6F1C2B9A-0D3E-4B7A-9E21-5C8D4F3A2B10")
  }

  func testAnIdFromUserDefaultsMovesIntoTheKeychain() throws {
    UserDefaults.standard.set("LEGACY-INSTALLATION-ID", forKey: "EXDeviceInstallationUUIDKey")
    XCTAssertEqual(try InstallationId.get(), "LEGACY-INSTALLATION-ID")
    XCTAssertNil(UserDefaults.standard.string(forKey: "EXDeviceInstallationUUIDKey"))
    XCTAssertEqual(stored()?.id, "LEGACY-INSTALLATION-ID")
    XCTAssertEqual(try InstallationId.get(), "LEGACY-INSTALLATION-ID")
  }

  func testAFreshInstallMakesOneAndKeepsItAsExpoWould() throws {
    let id = try InstallationId.get()
    XCTAssertNotNil(UUID(uuidString: id))
    XCTAssertEqual(try InstallationId.get(), id)
    let item = try XCTUnwrap(stored())
    XCTAssertEqual(item.id, id)
    XCTAssertEqual(item.accessible, kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly as String)
  }
}

/// `calendar.zones` gives what the browser's Intl gives (core/calendar/zones.ts intlZoneTable).
final class ZoneTests: XCTestCase {
  private let from: Int64 = 1_767_225_600_000 // 2026-01-01T00:00Z
  private let to: Int64 = 1_798_761_600_000 // 2027-01-01T00:00Z

  func testAZoneWithoutDaylightTimeIsOneSpanNamedByItsOffset() {
    XCTAssertEqual(Zones.spans("Asia/Shanghai", fromMs: from, toMs: to), [ZoneSpan(atMs: from, offsetMinutes: 480, name: "GMT+8")])
  }

  func testNewYorkChangesAtEachDaylightTransition() {
    XCTAssertEqual(Zones.spans("America/New_York", fromMs: from, toMs: to), [
      ZoneSpan(atMs: from, offsetMinutes: -300, name: "EST"),
      ZoneSpan(atMs: 1_772_953_200_000, offsetMinutes: -240, name: "EDT"), // 2026-03-08T07:00Z
      ZoneSpan(atMs: 1_793_512_800_000, offsetMinutes: -300, name: "EST"), // 2026-11-01T06:00Z
    ])
  }

  func testTheSpansStopAtTheWindowAndTheCap() {
    let short = Zones.spans("America/New_York", fromMs: from, toMs: 1_772_953_199_999)
    XCTAssertEqual(short?.count, 1)
    let long = Zones.spans("America/New_York", fromMs: from, toMs: from + 100 * 365 * 86_400_000)
    XCTAssertEqual(long?.count, Zones.maxSpans)
  }

  func testAnIdThatIsNoZoneIsNull() throws {
    XCTAssertNil(Zones.spans("Not/AZone", fromMs: from, toMs: to))
    let table = Zones.table(["Not/AZone", "Asia/Shanghai"], fromMs: from, toMs: to)
    let zones = try XCTUnwrap(table["zones"] as? [String: Any])
    XCTAssertTrue(zones["Not/AZone"] is NSNull)
    XCTAssertEqual((zones["Asia/Shanghai"] as? [[String: Any]])?.first?["offsetMinutes"] as? Int, 480)
    XCTAssertEqual(table["device"] as? String, TimeZone.current.identifier)
    XCTAssertTrue(JSONSerialization.isValidJSONObject(table))
  }
}

/// A notification's routing data, from Expo's wrapping (`body`) or the top level.
final class PushDataTests: XCTestCase {
  func testExpoPutsTheDataUnderBody() {
    let userInfo: [AnyHashable: Any] = [
      "aps": ["alert": ["title": "Agent needs input"]],
      "body": ["screen": "calendar", "calendar_id": "c1", "server_id": "s1", "n": 3] as [String: Any],
    ]
    XCTAssertEqual(PushData.of(userInfo), ["screen": "calendar", "calendar_id": "c1", "server_id": "s1", "n": "3"])
    XCTAssertFalse(PushData.isCalendarAlert(PushData.of(userInfo)))
  }

  func testTheTopLevelStringsWithoutBody() {
    let userInfo: [AnyHashable: Any] = ["aps": ["content-available": 1], "screen": "calendar", "calendar_id": "c1", "alert_key": "c1@1", "count": 2]
    let data = PushData.of(userInfo)
    XCTAssertEqual(data, ["screen": "calendar", "calendar_id": "c1", "alert_key": "c1@1"])
    XCTAssertTrue(PushData.isCalendarAlert(data))
  }

  func testALiveStatePushCarriesItsStateAndServer() throws {
    let payload = #"{"kind":"running","epoch":2,"seq":7,"run_key":"r1","title":"Build","workers":[{"name":"a","phase":"b"}],"worker_count":1}"#
    let push = try XCTUnwrap(LivePush.parse(["body": ["mewla_live": "1", "payload": payload, "server_ref": "s1"]]))
    XCTAssertEqual(push.serverRef, "s1")
    XCTAssertEqual(push.state.seq, 7)
    XCTAssertEqual(push.state.runKey, "r1")
    XCTAssertNil(LivePush.parse(["body": ["screen": "sessions"]]))
  }
}

/// A calendar alert fires at its instant, pinned to the item's zone.
final class CalendarAlertTests: XCTestCase {
  private let at = 1_909_143_000_000.0 // 2030-07-01T13:30:00Z

  func testTheTriggerIsTheWallClockOfTheInstantInTheItemsZone() throws {
    let parts = CalendarAlerts.triggerComponents(atMs: at, timezone: "America/New_York")
    XCTAssertEqual([parts.year, parts.month, parts.day, parts.hour, parts.minute, parts.second], [2030, 7, 1, 9, 30, 0])
    XCTAssertEqual(parts.timeZone?.identifier, "America/New_York")
    let shanghai = CalendarAlerts.triggerComponents(atMs: at, timezone: "Asia/Shanghai")
    XCTAssertEqual([shanghai.day, shanghai.hour], [1, 21])
    for zone in ["America/New_York", "Asia/Shanghai"] {
      let trigger = UNCalendarNotificationTrigger(dateMatching: CalendarAlerts.triggerComponents(atMs: at, timezone: zone), repeats: false)
      XCTAssertEqual(try XCTUnwrap(trigger.nextTriggerDate()).timeIntervalSince1970 * 1000, at, zone)
    }
  }

  func testTheRequestCarriesTheAlertsIdentityAndLevel() throws {
    let alert = ArmedAlert(key: "c1@\(Int64(at))", itemId: "c1", serverId: "s1", title: "Standup", body: "in 10 min", atMs: at, timezone: "Asia/Shanghai")
    let strong = CalendarAlerts.request(alert, strong: true)
    XCTAssertEqual(strong.identifier, alert.key)
    XCTAssertEqual(strong.content.title, "Standup")
    XCTAssertEqual(strong.content.interruptionLevel, .timeSensitive)
    XCTAssertEqual(PushData.of(strong.content.userInfo), ["screen": "calendar", "calendar_id": "c1", "server_id": "s1", "alert_key": alert.key])
    XCTAssertTrue(PushData.isCalendarAlert(PushData.of(strong.content.userInfo)))
    XCTAssertEqual(CalendarAlerts.request(alert, strong: false).content.interruptionLevel, .active)
    XCTAssertEqual(ArmedAlert(["key": alert.key, "itemId": "c1", "serverId": "s1", "title": "Standup", "body": "in 10 min", "atMs": at, "timezone": "Asia/Shanghai"] as [String: Any]), alert)
  }
}
