import Foundation

/// The routing data of a notification, remote or local. Expo's service, and expo-notifications for
/// the local ones the Expo app scheduled, put the push's `data` under `body`; a notification this
/// app schedules, or a push sent without Expo, carries the keys at the top level.
enum PushData {
  static func of(_ userInfo: [AnyHashable: Any]) -> [String: String] {
    if let body = userInfo["body"] as? [String: Any] { return strings(body) }
    if let body = userInfo["body"] as? String, let data = body.data(using: .utf8),
       let parsed = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] {
      return strings(parsed)
    }
    var top: [String: String] = [:]
    for (key, value) in userInfo {
      guard let key = key as? String, key != "aps", let value = value as? String else { continue }
      top[key] = value
    }
    return top
  }

  /// A calendar alert this phone armed: while the app is in front it shows its own prompt.
  static func isCalendarAlert(_ data: [String: String]) -> Bool {
    data["screen"] == "calendar" && !(data["alert_key"] ?? "").isEmpty
  }

  private static func strings(_ object: [String: Any]) -> [String: String] {
    var out: [String: String] = [:]
    for (key, value) in object {
      switch value {
      case let string as String: out[key] = string
      case let number as NSNumber: out[key] = number.stringValue
      case is NSNull: continue
      default:
        if JSONSerialization.isValidJSONObject(value), let data = try? JSONSerialization.data(withJSONObject: value) {
          out[key] = String(data: data, encoding: .utf8)
        }
      }
    }
    return out
  }
}

/// The APNs environment the device token belongs to, the way expo-application reads it: the
/// `aps-environment` entitlement of the embedded provisioning profile. Without a profile (the
/// Simulator) the token is a sandbox one.
enum ApsEnvironment {
  static var isDevelopment: Bool {
    guard let url = Bundle.main.url(forResource: "embedded", withExtension: "mobileprovision") else {
      #if targetEnvironment(simulator)
      return true
      #else
      return false
      #endif
    }
    return environment(ofProfile: url) == "development"
  }

  /// The profile is a CMS envelope around a plist; the plist is plain text inside it.
  static func environment(ofProfile url: URL) -> String? {
    guard let raw = try? Data(contentsOf: url),
          let start = raw.range(of: Data("<?xml".utf8)),
          let end = raw.range(of: Data("</plist>".utf8), in: start.lowerBound..<raw.endIndex),
          let profile = (try? PropertyListSerialization.propertyList(from: raw.subdata(in: start.lowerBound..<end.upperBound), format: nil)) as? [String: Any],
          let entitlements = profile["Entitlements"] as? [String: Any] else { return nil }
    return entitlements["aps-environment"] as? String
  }
}
