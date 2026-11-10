import Security
import XCTest
@testable import Mewla

/// The in-place upgrade from the Expo app, as far as a simulator can prove it: what
/// expo-secure-store and AsyncStorage wrote, in their own formats, reads back here.
final class UpgradeTests: XCTestCase {
  private let key = "mewla.device.v3.seed"

  override func tearDown() {
    try? SecureStore().remove(key)
    UserDefaults.standard.removeObject(forKey: "mewla.legacyStorageCopied")
  }

  /// expo-secure-store's own write (SecureStoreModule.swift: service `app:no-auth`, key as Data).
  private func expoWrite(_ key: String, _ value: String, service: String = "app:no-auth") {
    let account = Data(key.utf8)
    let query: [String: Any] = [
      kSecClass as String: kSecClassGenericPassword,
      kSecAttrService as String: service,
      kSecAttrGeneric as String: account,
      kSecAttrAccount as String: account,
    ]
    SecItemDelete(query as CFDictionary)
    var add = query
    add[kSecValueData as String] = Data(value.utf8)
    add[kSecAttrAccessible as String] = kSecAttrAccessibleWhenUnlocked
    XCTAssertEqual(SecItemAdd(add as CFDictionary, nil), errSecSuccess, "the Keychain refused the write (unsigned build?)")
  }

  func testTheDeviceKeyTheExpoAppStoredReadsBack() throws {
    expoWrite(key, String(repeating: "1", count: 64))
    XCTAssertEqual(try SecureStore().get(key), String(repeating: "1", count: 64))
  }

  func testAKeyFromBeforeTheServiceSplitReadsBackAndIsReplacedInPlace() throws {
    try SecureStore().remove(key)
    expoWrite(key, "old", service: "app")
    XCTAssertEqual(try SecureStore().get(key), "old")
    try SecureStore().set(key, "new")
    XCTAssertEqual(try SecureStore().get(key), "new")
    expoWrite("unrelated", "x", service: "app")
    try SecureStore().remove("unrelated")
    XCTAssertNil(try SecureStore().get("unrelated"))
  }

  func testAsyncStorageEntriesComeOverOnceIncludingTheOnesKeptInFiles() throws {
    let directory = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
      .appendingPathComponent(Bundle.main.bundleIdentifier!, isDirectory: true)
      .appendingPathComponent("RCTAsyncLocalStorage_V1", isDirectory: true)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }
    let big = String(repeating: "d", count: 2000)
    let manifest: [String: Any] = ["mewla:v1:current_server_id": "s1", "mewla.draft.v1:s1:chat": NSNull(), "other:key": "x"]
    try JSONSerialization.data(withJSONObject: manifest).write(to: directory.appendingPathComponent("manifest.json"))
    // AsyncStorage names the file RCTMD5Hash(key), lowercase hex.
    try big.write(to: directory.appendingPathComponent("4960c743eda604ef054c23ee50586a3e"), atomically: true, encoding: .utf8)

    let legacy = LegacyStorage()
    XCTAssertEqual(legacy.entries(), ["mewla:v1:current_server_id": "s1", "mewla.draft.v1:s1:chat": big])
    legacy.copied()
    XCTAssertNil(legacy.entries())
  }
}

