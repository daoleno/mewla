import ExpoModulesCore
import UIKit

/// A live_state push (data-only, content-available) wakes the app in the background; this applies
/// it to the Live Activity and widgets with the app closed. Expo aggregates the completion handler
/// of every subscriber, so expo-notifications still gets the message too.
public class MewlaLiveAppDelegate: ExpoAppDelegateSubscriber {
  public func application(
    _ application: UIApplication,
    didReceiveRemoteNotification userInfo: [AnyHashable: Any],
    fetchCompletionHandler completionHandler: @escaping (UIBackgroundFetchResult) -> Void
  ) {
    guard MewlaLiveShared.isEnabled, let message = MewlaLivePush.parse(userInfo) else {
      completionHandler(.noData)
      return
    }
    // Pushes for a server that is not the current one are dropped.
    let store = MewlaLiveStore.shared
    guard store.currentServer == message.serverRef else {
      completionHandler(.noData)
      return
    }
    // apply() runs `done` on every path (stale state included), so iOS always hears back.
    _ = store.apply(message.state, onToken: { _ in }, done: { completionHandler(.newData) })
  }
}

struct MewlaLivePush {
  let state: MewlaLiveState
  let serverRef: String

  /// Expo delivers a push's `data` under `body`; values are strings (the state is a JSON string).
  static func parse(_ userInfo: [AnyHashable: Any]) -> MewlaLivePush? {
    let data = (userInfo["body"] as? [String: Any]) ?? (userInfo as? [String: Any]) ?? [:]
    guard (data["mewla_live"] as? String) == "1",
      let payload = data["payload"] as? String,
      let state = MewlaLiveState.parse(payload)
    else { return nil }
    return MewlaLivePush(state: state, serverRef: (data["server_ref"] as? String) ?? "")
  }
}
