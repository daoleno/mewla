import Foundation
import Security

/// The device key and the other secrets: Keychain generic passwords of service `app:no-auth`,
/// the key as account.
struct SecureStore {
  private static let service = "app:no-auth"

  func get(_ key: String) throws -> String? {
    var query = Self.query(key, Self.service)
    query[kSecMatchLimit as String] = kSecMatchLimitOne
    query[kSecReturnData as String] = true
    var item: CFTypeRef?
    let status = SecItemCopyMatching(query as CFDictionary, &item)
    if status == errSecItemNotFound { return nil }
    guard status == errSecSuccess, let data = item as? Data else { throw Self.failure(status, key) }
    return String(data: data, encoding: .utf8)
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
    let status = SecItemDelete(Self.query(key, Self.service) as CFDictionary)
    guard status == errSecSuccess || status == errSecItemNotFound else { throw Self.failure(status, key) }
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
