import NativeShell
import UIKit
import UserNotifications

/// What the system tells the app about notifications, from launch on (before any scene exists):
/// the APNs device token, a notification arriving with the app in front, and a tap, which reaches
/// the app as `push.opened {data}`. A tap that started the app is held until the app view is here.
@MainActor
final class PushCenter: NSObject, UNUserNotificationCenterDelegate {
  static let shared = PushCenter()

  private weak var app: NshAppView?
  private var held: [[String: String]] = []
  private var waiting: [UUID: CheckedContinuation<Data, Error>] = [:]

  /// From `application(_:didFinishLaunchingWithOptions:)`: the tap that launched the app comes right after.
  func start() {
    UNUserNotificationCenter.current().delegate = self
  }

  /// The app view, once its bundle is loaded: taps held so far reach it now.
  func attach(_ app: NshAppView) {
    self.app = app
    let taps = held
    held = []
    taps.forEach { app.send("push.opened", ["data": $0]) }
  }

  // MARK: Device token

  /// Registers with APNs and waits for the answer `AppDelegate` passes on; every call registers
  /// again (iOS hands out the current token each time). Gives up after `timeout` seconds.
  func deviceToken(timeout: TimeInterval = 55) async throws -> Data {
    let id = UUID()
    return try await withCheckedThrowingContinuation { continuation in
      waiting[id] = continuation
      UIApplication.shared.registerForRemoteNotifications()
      Task { @MainActor [weak self] in
        try? await Task.sleep(nanoseconds: UInt64(timeout * 1_000_000_000))
        self?.waiting.removeValue(forKey: id)?.resume(throwing: NSError(
          domain: "Mewla", code: 3, userInfo: [NSLocalizedDescriptionKey: "APNs gave no device token."]))
      }
    }
  }

  func registered(_ token: Data) {
    let all = waiting
    waiting = [:]
    all.values.forEach { $0.resume(returning: token) }
  }

  func failed(_ error: Error) {
    NSLog("Mewla push: APNs registration failed: %@", error.localizedDescription)
    let all = waiting
    waiting = [:]
    all.values.forEach { $0.resume(throwing: error) }
  }

  static func hex(_ token: Data) -> String {
    token.map { String(format: "%02x", $0) }.joined()
  }

  // MARK: UNUserNotificationCenterDelegate

  /// In front: a banner, its sound and a place in the list, except a calendar alert this phone
  /// armed, which the app shows as its own prompt.
  nonisolated func userNotificationCenter(
    _ center: UNUserNotificationCenter,
    willPresent notification: UNNotification,
    withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
  ) {
    let data = PushData.of(notification.request.content.userInfo)
    let hidden = PushData.isCalendarAlert(data)
    // Never the text: a test reads this line to see what was decided.
    NSLog("Mewla push: in front, screen=%@ alert_key=%@: %@", data["screen"] ?? "-", data["alert_key"] == nil ? "no" : "yes", hidden ? "hidden" : "banner")
    completionHandler(hidden ? [] : [.banner, .sound, .list])
  }

  nonisolated func userNotificationCenter(
    _ center: UNUserNotificationCenter,
    didReceive response: UNNotificationResponse,
    withCompletionHandler completionHandler: @escaping () -> Void
  ) {
    if response.actionIdentifier == UNNotificationDefaultActionIdentifier {
      let data = PushData.of(response.notification.request.content.userInfo)
      NSLog("Mewla push: opened, screen=%@", data["screen"] ?? "-")
      Task { @MainActor in PushCenter.shared.opened(data) }
    }
    completionHandler()
  }

  private func opened(_ data: [String: String]) {
    if let app {
      app.send("push.opened", ["data": data])
    } else {
      held.append(data)
    }
  }
}
