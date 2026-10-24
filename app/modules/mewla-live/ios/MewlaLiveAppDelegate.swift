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
    // Every branch logs, and logs nothing from the payload: a real-device test reads these lines
    // from the device console to tell "iOS never called us" from "we dropped it" from "applied".
    MewlaLiveLog.push("received state=\(application.applicationState.rawValue)")
    guard MewlaLiveShared.isEnabled else {
      MewlaLiveLog.push("ignored: this build has no Live Activity extension")
      completionHandler(.noData)
      return
    }
    guard let message = MewlaLivePush.parse(userInfo) else {
      MewlaLiveLog.push("ignored: not a live_state push")
      completionHandler(.noData)
      return
    }
    // Pushes for a server that is not the current one are dropped.
    let store = MewlaLiveStore.shared
    guard store.currentServer == message.serverRef else {
      MewlaLiveLog.push("dropped: another server")
      completionHandler(.noData)
      return
    }
    let pair = "\(message.state.epoch):\(message.state.seq)"
    // apply() runs `done` on every path (stale state included), so iOS always hears back.
    let applied = store.apply(message.state, onToken: { _ in }, done: {
      completionHandler(.newData)
    })
    MewlaLiveLog.push(
      applied ? "applied \(pair)" : "skipped \(pair): older than what is shown")
  }
}

/// NSLog-based so the lines show up in the device console (`xcrun devicectl ... console` or
/// Console.app) with no configuration. Never log the question text or choices.
enum MewlaLiveLog {
  static func push(_ message: String) {
    NSLog("MewlaLive push: %@", message)
  }

  static func update(_ message: String) {
    NSLog("MewlaLive activity: %@", message)
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
