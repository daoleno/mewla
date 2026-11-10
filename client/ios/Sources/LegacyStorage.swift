import CryptoKit
import Foundation

/// What the Expo app kept in AsyncStorage (`Application Support/<bundle id>/RCTAsyncLocalStorage_V1`):
/// the computers, the current one, drafts, preferences. The app copies it into its own storage
/// once, on the first start after the upgrade, and says when it has; the files stay as they were.
struct LegacyStorage {
  private static let copiedKey = "mewla.legacyStorageCopied"
  private let directory = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
    .appendingPathComponent(Bundle.main.bundleIdentifier ?? "", isDirectory: true)
    .appendingPathComponent("RCTAsyncLocalStorage_V1", isDirectory: true)

  /// The Mewla entries, or nil once they were copied or when there never was an Expo app.
  func entries() -> [String: String]? {
    if UserDefaults.standard.bool(forKey: Self.copiedKey) { return nil }
    guard let data = try? Data(contentsOf: directory.appendingPathComponent("manifest.json")),
          let manifest = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return nil }
    var entries: [String: String] = [:]
    for (key, value) in manifest where key.hasPrefix("mewla") {
      // A value over 1 KB is a file of its own, named by the MD5 of the key; the manifest holds null.
      if let inline = value as? String {
        entries[key] = inline
      } else if let stored = try? String(contentsOf: directory.appendingPathComponent(Self.md5(key)), encoding: .utf8) {
        entries[key] = stored
      }
    }
    return entries
  }

  func copied() {
    UserDefaults.standard.set(true, forKey: Self.copiedKey)
  }

  private static func md5(_ key: String) -> String {
    Insecure.MD5.hash(data: Data(key.utf8)).map { String(format: "%02x", $0) }.joined()
  }
}
