import ActivityKit
import ExpoModulesCore
import Foundation
import UIKit
import WidgetKit

/// iOS counterpart of the Android MewlaLive module: the same calls, drawn as one Live Activity
/// (Dynamic Island and Lock Screen) and the App Group files the WidgetKit extension reads.
public final class MewlaLiveModule: Module {
  public func definition() -> ModuleDefinition {
    Name("MewlaLive")

    Events("onLiveActivityPushToken")

    /// False in a build without the Live Activity extension: nothing to draw on, nothing to register.
    Function("isSupported") { () -> Bool in
      MewlaLiveShared.isEnabled
    }

    Function("apply") { (raw: String) -> Bool in
      guard MewlaLiveShared.isEnabled, let state = MewlaLiveState.parse(raw) else { return false }
      return MewlaLiveStore.shared.apply(state, onToken: { token in
        self.sendEvent("onLiveActivityPushToken", ["token": token])
      })
    }

    Function("setCurrentServer") { (serverRef: String) in
      MewlaLiveStore.shared.setCurrentServer(serverRef)
    }

    Function("configureWidget") { (baseUrl: String, token: String) in
      MewlaLiveStore.shared.configureWidget(baseUrl: baseUrl, token: token)
    }

    Function("configureWidgetPushed") {
      MewlaLiveStore.shared.configureWidgetPushed()
    }

    Function("clearWidget") {
      MewlaLiveStore.shared.clearWidget()
    }

    Function("refreshWidgets") {
      WidgetCenter.shared.reloadAllTimelines()
    }

    Function("widgetState") { () -> String in
      MewlaLiveStore.shared.widgetState()
    }

    Function("diagnostics") { () -> [String: Any?] in
      let enabled = ActivityAuthorizationInfo().areActivitiesEnabled
      return [
        "sdk": ProcessInfo.processInfo.operatingSystemVersion.majorVersion,
        "notificationsEnabled": enabled,
        "promotionAllowed": enabled,
      ]
    }
  }
}

/// All state the module keeps: the last applied state and widget credentials live in the App
/// Group container (the extension reads them); the current server and the dismissed run live in
/// the app's defaults.
final class MewlaLiveStore {
  static let shared = MewlaLiveStore()

  private let lock = NSLock()
  private let defaults = UserDefaults.standard
  private let serverKey = "mewla.live.currentServer"
  private let dismissedKey = "mewla.live.dismissedRun"
  /// The last state this app put on the Live Activity. Kept apart from the shared widget cache:
  /// the widget extension writes that file when it fetches, and a fetch must not make the app
  /// think the Activity already shows the state.
  private let appliedKey = "mewla.live.lastApplied"
  private var observed = Set<String>()

  // MARK: Server binding

  /// A server switch (or none) drops the old server's activity and widget cache. Not before the
  /// first call: the stored value is "unknown", which must not clear a surface on a cold start.
  func setCurrentServer(_ serverRef: String) {
    lock.lock()
    let previous = defaults.string(forKey: serverKey)
    defaults.set(serverRef, forKey: serverKey)
    lock.unlock()
    if previous == serverRef { return }
    endActivities()
    defaults.removeObject(forKey: dismissedKey)
    defaults.removeObject(forKey: appliedKey)
    clearWidget()
    removeState()
  }

  // MARK: Applying a state

  /// `done` runs once the Live Activity has been updated: a background push must not tell iOS it
  /// is finished before then, or the process is suspended mid-update.
  func apply(
    _ state: MewlaLiveState,
    onToken: @escaping (String) -> Void,
    done: (() -> Void)? = nil
  ) -> Bool {
    lock.lock()
    // (epoch, seq) orders states from the socket and from pushes; a late one never wins.
    // Against what this app last showed, not the shared widget file: the extension writes that
    // file when it fetches. Same rule as the widget's store() — a state is shown wherever it is
    // newer than that surface's own last state — so a fetch that won the race cannot leave the
    // Live Activity on an older state.
    if let last = lastApplied, !state.isNewer(thanEpoch: last.epoch, seq: last.seq) {
      lock.unlock()
      done?()
      return false
    }
    if let data = try? JSONEncoder().encode(state) {
      MewlaLiveShared.write(data, to: MewlaLiveShared.stateURL)
    }
    defaults.set("\(state.epoch):\(state.seq)", forKey: appliedKey)
    lock.unlock()
    WidgetCenter.shared.reloadAllTimelines()
    if #available(iOS 17.0, *) {
      Task {
        await self.show(state, onToken: onToken)
        done?()
      }
    } else {
      done?()
    }
    return true
  }

  /// The "epoch:seq" pair this app last showed on the Live Activity.
  private var lastApplied: (epoch: Int64, seq: Int64)? {
    guard let raw = defaults.string(forKey: appliedKey) else { return nil }
    let parts = raw.split(separator: ":", maxSplits: 1).compactMap { Int64($0) }
    guard parts.count == 2 else { return nil }
    return (parts[0], parts[1])
  }

  /// The state of a server that is not the current one is dropped, like on Android.
  var currentServer: String? { defaults.string(forKey: serverKey) }

  @available(iOS 17.0, *)
  private func show(_ state: MewlaLiveState, onToken: @escaping (String) -> Void) async {
    let content = ActivityContent(state: state, staleDate: nil)
    let existing = Activity<MewlaLiveAttributes>.activities

    if state.isIdle {
      for activity in existing { await activity.end(content, dismissalPolicy: .immediate) }
      if !existing.isEmpty { MewlaLiveLog.update("ended (idle)") }
      return
    }
    // A swiped-away run stays away until a new run key (a new Work or a new question).
    if defaults.string(forKey: dismissedKey) == state.runKey {
      MewlaLiveLog.update("skipped: this run was dismissed")
      return
    }

    if let activity = existing.first(where: { $0.activityState == .active }) {
      await activity.update(content)
      MewlaLiveLog.update("updated \(state.epoch):\(state.seq) kind=\(state.kind)")
      return
    }
    guard ActivityAuthorizationInfo().areActivitiesEnabled else {
      MewlaLiveLog.update("not started: Live Activities are off for Mewla")
      return
    }
    do {
      // The system only starts an activity while the app is in the foreground; from the
      // background this throws and the next foreground apply starts it.
      let activity = try Activity.request(
        attributes: MewlaLiveAttributes(runKey: state.runKey),
        content: content,
        pushType: .token
      )
      observe(activity, onToken: onToken)
    } catch {
      // `visibility` means the app was in the background: iOS only starts an activity in the
      // foreground, so this arrives with the next foreground apply.
      MewlaLiveLog.update("not started: \(error)")
    }
  }

  @available(iOS 17.0, *)
  private func observe(_ activity: Activity<MewlaLiveAttributes>, onToken: @escaping (String) -> Void) {
    lock.lock()
    let isNew = observed.insert(activity.id).inserted
    lock.unlock()
    guard isNew else { return }
    Task {
      for await data in activity.pushTokenUpdates {
        onToken(data.map { String(format: "%02x", $0) }.joined())
      }
    }
    Task { [weak self] in
      for await update in activity.activityStateUpdates {
        if update == .dismissed {
          // The user removed it: remember which run, so only a new run key brings it back.
          self?.defaults.set(activity.content.state.runKey, forKey: "mewla.live.dismissedRun")
        }
        if update == .dismissed || update == .ended {
          self?.lock.lock()
          self?.observed.remove(activity.id)
          self?.lock.unlock()
          break
        }
      }
    }
  }

  private func endActivities() {
    guard #available(iOS 17.0, *) else { return }
    Task {
      for activity in Activity<MewlaLiveAttributes>.activities {
        await activity.end(nil, dismissalPolicy: .immediate)
      }
    }
  }

  // MARK: Widgets

  /// A plain address a widget can fetch: the extension reads this file and GETs /widget/snapshot.
  func configureWidget(baseUrl: String, token: String) {
    let config: [String: String] = ["mode": "fetch", "base_url": baseUrl, "token": token]
    guard let data = try? JSONEncoder().encode(config) else { return }
    MewlaLiveShared.write(data, to: MewlaLiveShared.widgetConfigURL)
    removeRevokedMarker()
    WidgetCenter.shared.reloadAllTimelines()
  }

  /// Mewla Link has no address a widget can fetch: the widgets show what this app and pushes apply.
  func configureWidgetPushed() {
    guard let data = try? JSONEncoder().encode(["mode": "pushed"]) else { return }
    MewlaLiveShared.write(data, to: MewlaLiveShared.widgetConfigURL)
    removeRevokedMarker()
    WidgetCenter.shared.reloadAllTimelines()
  }

  func clearWidget() {
    if let url = MewlaLiveShared.widgetConfigURL { try? FileManager.default.removeItem(at: url) }
    removeRevokedMarker()
    WidgetCenter.shared.reloadAllTimelines()
  }

  /// "none" until provisioned, "ok" while the token works, "revoked" after the daemon refused it.
  func widgetState() -> String {
    if let marker = MewlaLiveShared.revokedMarkerURL, FileManager.default.fileExists(atPath: marker.path) {
      return "revoked"
    }
    guard let url = MewlaLiveShared.widgetConfigURL, FileManager.default.fileExists(atPath: url.path) else {
      return "none"
    }
    return "ok"
  }

  private func removeState() {
    if let url = MewlaLiveShared.stateURL { try? FileManager.default.removeItem(at: url) }
  }

  private func removeRevokedMarker() {
    if let url = MewlaLiveShared.revokedMarkerURL { try? FileManager.default.removeItem(at: url) }
  }
}
