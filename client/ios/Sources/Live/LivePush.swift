import UIKit

/// A live_state push (data-only, content-available) wakes the app in the background; this applies
/// it to the Live Activity and widgets with the app closed. Every branch logs, and nothing from the
/// payload: a device test reads these lines to tell "iOS never called us" from "dropped" from "applied".
struct LivePush {
  let state: MewlaLiveState
  let serverRef: String

  /// `mewla_live: "1"` with the state as a JSON string under `payload` (Expo puts the data under `body`).
  static func parse(_ userInfo: [AnyHashable: Any]) -> LivePush? {
    let data = PushData.of(userInfo)
    guard data["mewla_live"] == "1", let payload = data["payload"], let state = MewlaLiveState.parse(payload) else { return nil }
    return LivePush(state: state, serverRef: data["server_ref"] ?? "")
  }

  static func receive(_ userInfo: [AnyHashable: Any], state: UIApplication.State, done: @escaping (UIBackgroundFetchResult) -> Void) {
    LiveLog.push("received state=\(state.rawValue)")
    guard MewlaLiveShared.isEnabled else {
      LiveLog.push("ignored: this build has no Live Activity extension")
      return done(.noData)
    }
    guard let message = parse(userInfo) else {
      LiveLog.push("ignored: not a live_state push")
      return done(.noData)
    }
    let store = LiveStore.shared
    guard store.currentServer == message.serverRef else {
      LiveLog.push("dropped: another server")
      return done(.noData)
    }
    let pair = "\(message.state.epoch):\(message.state.seq)"
    // apply() runs `done` on every path (stale state included), so iOS always hears back.
    let applied = store.apply(message.state) { done(.newData) }
    LiveLog.push(applied ? "applied \(pair)" : "skipped \(pair): older than what is shown")
  }
}
