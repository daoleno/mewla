import NativeShell
import UIKit
import UserNotifications

/// `push.*`: whether notifications are allowed, the system prompt, and the device's APNs token
/// with the Expo installation id, which the app exchanges for its Expo token (core/push).
/// A tapped notification reaches the app as `push.opened {data}` (PushCenter).
enum PushHost {
  @MainActor
  static func register(_ app: NshAppView) {
    app.handle("push.permission") { _ -> Any? in
      ["granted": await allowed()] as [String: Any]
    }
    app.handle("push.requestPermission") { _ -> Any? in
      let granted = try await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge])
      if granted { UIApplication.shared.registerForRemoteNotifications() }
      return granted
    }
    app.handle("push.deviceToken") { _ -> Any? in
      let token = try await PushCenter.shared.deviceToken()
      return [
        "type": "apns",
        "token": PushCenter.hex(token),
        "deviceId": try InstallationId.get(),
        "appId": Bundle.main.bundleIdentifier ?? "",
        "development": ApsEnvironment.isDevelopment,
      ] as [String: Any]
    }
  }

  static func allowed() async -> Bool {
    switch await UNUserNotificationCenter.current().notificationSettings().authorizationStatus {
    case .authorized, .provisional, .ephemeral: return true
    default: return false
    }
  }
}
