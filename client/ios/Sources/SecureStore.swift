import Foundation
import Security

/// The device key and the other secrets, kept exactly where the Expo app kept them
/// (expo-secure-store: generic passwords of service `app:no-auth`, the key as account),
/// so the upgrade reads the same device identity and nobody pairs again. Nothing is copied.
struct SecureStore {
  private static let service = "app:no-auth"
  /// Items written by expo-secure-store before it split services by authentication.
  private static let legacyService = "app"

  func get(_ key: String) throws -> String? {
    for service in [Self.service, Self.legacyService] {
      var query = Self.query(key, service)
      query[kSecMatchLimit as String] = kSecMatchLimitOne
      query[kSecReturnData as String] = true
      var item: CFTypeRef?
      let status = SecItemCopyMatching(query as CFDictionary, &item)
      if status == errSecItemNotFound { continue }
      guard status == errSecSuccess, let data = item as? Data else { throw Self.failure(status, key) }
      return String(data: data, encoding: .utf8)
    }
    return nil
  }

  func set(_ key: String, _ value: String) throws {
    let query = Self.query(key, Self.service)
    let data = Data(value.utf8)
    var status = SecItemUpdate(query as CFDictionary, [kSecValueData as String: data] as CFDictionary)
    if status == errSecItemNotFound {
      var add = query
      add[kSecValueData as String] = data
      add[kSecAttrAccessible as String] = kSecAttrAccessibleWhenUnlocked
      status = SecItemAdd(add as CFDictionary, nil)
    }
    guard status == errSecSuccess else { throw Self.failure(status, key) }
  }

  func remove(_ key: String) throws {
    for service in [Self.service, Self.legacyService] {
      let status = SecItemDelete(Self.query(key, service) as CFDictionary)
      guard status == errSecSuccess || status == errSecItemNotFound else { throw Self.failure(status, key) }
    }
  }

  private static func query(_ key: String, _ service: String) -> [String: Any] {
    let account = Data(key.utf8)
    return [
      kSecClass as String: kSecClassGenericPassword,
      kSecAttrService as String: service,
      kSecAttrGeneric as String: account,
      kSecAttrAccount as String: account,
    ]
  }

  private static func failure(_ status: OSStatus, _ key: String) -> NSError {
    let reason = SecCopyErrorMessageString(status, nil) as String? ?? "status \(status)"
    return NSError(domain: "Mewla", code: Int(status), userInfo: [NSLocalizedDescriptionKey: "Keychain \(key): \(reason)"])
  }
}
