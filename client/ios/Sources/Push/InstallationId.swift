import Foundation
import Security

/// The id Expo's push service knows this install by (the `deviceId` of the Expo token exchange),
/// read where expo-notifications kept it (ServerRegistrationModule.swift) so the upgraded app keeps
/// its Expo token: a Keychain generic password of service = the bundle id, account and generic =
/// "EXDeviceInstallationUUIDKey"; before that, the same key in UserDefaults, moved into the
/// Keychain on first read. A fresh install makes one and keeps it the same way.
enum InstallationId {
  static let key = "EXDeviceInstallationUUIDKey"

  static func get() throws -> String {
    if let stored = try read() { return stored }
    if let legacy = UserDefaults.standard.string(forKey: key) {
      try write(legacy)
      UserDefaults.standard.removeObject(forKey: key)
      return legacy
    }
    let id = UUID().uuidString
    try write(id)
    return id
  }

  /// Expo's search query; reads and writes add to it.
  static func query() -> [String: Any] {
    let account = Data(key.utf8)
    return [
      kSecClass as String: kSecClassGenericPassword,
      kSecAttrService as String: Bundle.main.bundleIdentifier ?? "",
      kSecAttrGeneric as String: account,
      kSecAttrAccount as String: account,
    ]
  }

  private static func read() throws -> String? {
    var get = query()
    get[kSecMatchLimit as String] = kSecMatchLimitOne
    get[kSecReturnData as String] = true
    var item: CFTypeRef?
    let status = SecItemCopyMatching(get as CFDictionary, &item)
    if status == errSecItemNotFound { return nil }
    guard status == errSecSuccess else { throw failure(status) }
    guard let data = item as? Data, let id = String(data: data, encoding: .utf8), !id.isEmpty else { return nil }
    return id
  }

  /// As Expo stores it: delete, then add.
  private static func write(_ id: String) throws {
    SecItemDelete(query() as CFDictionary)
    var add = query()
    add[kSecValueData as String] = Data(id.utf8)
    add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
    let status = SecItemAdd(add as CFDictionary, nil)
    guard status == errSecSuccess else { throw failure(status) }
  }

  private static func failure(_ status: OSStatus) -> NSError {
    let reason = SecCopyErrorMessageString(status, nil) as String? ?? "status \(status)"
    return NSError(domain: "Mewla", code: Int(status), userInfo: [NSLocalizedDescriptionKey: "Keychain installation id: \(reason)"])
  }
}
