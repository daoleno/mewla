import ActivityKit
import Foundation
import WidgetKit

/// The Live Activity (Dynamic Island and Lock Screen) and the App Group files the WidgetKit
/// extension reads, for the current computer. The last applied state and the widget credentials
/// live in the App Group container (the extension reads them); the current server and the
/// dismissed run live in the app's defaults, under the Expo app's keys.
final class LiveStore {
  static let shared = LiveStore()

  private let lock = NSLock()
  private let defaults = UserDefaults.standard
  private let serverKey = "mewla.live.currentServer"
  private let dismissedKey = "mewla.live.dismissedRun"
  /// The last state this app put on the Live Activity. Kept apart from the shared widget cache:
  /// the widget extension writes that file when it fetches, and a fetch must not make the app
  /// think the Activity already shows the state.
  private let appliedKey = "mewla.live.lastApplied"
  private var observed = Set<String>()

  /// The build ships the extension (Info.plist key, App Group, the embedded .appex) and the person
  /// lets Mewla show Live Activities.
  static var supported: Bool {
    guard MewlaLiveShared.isEnabled, extensionEmbedded else { return false }
    if #available(iOS 16.1, *) { return ActivityAuthorizationInfo().areActivitiesEnabled }
    return false
  }

  static var extensionEmbedded: Bool {
    guard let plugins = Bundle.main.builtInPlugInsURL,
          let found = try? FileManager.default.contentsOfDirectory(atPath: plugins.path) else { return false }
    return found.contains { $0.hasSuffix(".appex") }
  }

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

  /// The state of a server that is not the current one is dropped.
  var currentServer: String? { defaults.string(forKey: serverKey) }

  // MARK: Applying a state

  /// `done` runs once the Live Activity has been updated: a background push must not tell iOS it
  /// is finished before then, or the process is suspended mid-update.
  func apply(_ state: MewlaLiveState, done: (() -> Void)? = nil) -> Bool {
    lock.lock()
    // (epoch, seq) orders states from the socket and from pushes; a late one never wins.
    // Against what this app last showed, not the shared widget file: the extension writes that
    // file when it fetches. Same rule as the widget's store(): a state is shown wherever it is
    // newer than that surface's own last state.
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
        await self.show(state)
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

  @available(iOS 17.0, *)
  private func show(_ state: MewlaLiveState) async {
    let content = ActivityContent(state: state, staleDate: nil)
    let existing = Activity<MewlaLiveAttributes>.activities

    if state.isIdle {
      for activity in existing { await activity.end(content, dismissalPolicy: .immediate) }
      if !existing.isEmpty { LiveLog.update("ended (idle)") }
      return
    }
    // A swiped-away run stays away until a new run key (a new Work or a new question).
    if defaults.string(forKey: dismissedKey) == state.runKey {
      LiveLog.update("skipped: this run was dismissed")
      return
    }
    if let activity = existing.first(where: { $0.activityState == .active }) {
      await activity.update(content)
      LiveLog.update("updated \(state.epoch):\(state.seq) kind=\(state.kind)")
      return
    }
    guard ActivityAuthorizationInfo().areActivitiesEnabled else {
      LiveLog.update("not started: Live Activities are off for Mewla")
      return
    }
    do {
      // The system only starts an activity while the app is in the foreground; from the
      // background this throws (`visibility`) and the next foreground apply starts it.
      let activity = try Activity.request(
        attributes: MewlaLiveAttributes(runKey: state.runKey),
        content: content,
        pushType: .token
      )
      observe(activity)
    } catch {
      LiveLog.update("not started: \(error)")
    }
  }

  /// NSLock is not usable from async code, so the observed-set bookkeeping lives in these
  /// synchronous helpers and the Tasks below only call them.
  private func markObserved(_ id: String) -> Bool {
    lock.lock()
    defer { lock.unlock() }
    return observed.insert(id).inserted
  }

  private func forgetObserved(_ id: String) {
    lock.lock()
    defer { lock.unlock() }
    observed.remove(id)
  }

  @available(iOS 17.0, *)
  private func observe(_ activity: Activity<MewlaLiveAttributes>) {
    guard markObserved(activity.id) else { return }
    // The activity's own push token: the daemon updates Live Activities through the app's
    // content-available push (LivePush), so nothing takes this token yet. Logged, never the value.
    Task {
      for await _ in activity.pushTokenUpdates { LiveLog.update("activity push token issued") }
    }
    Task { [weak self] in
      for await update in activity.activityStateUpdates {
        if update == .dismissed {
          // The person removed it: remember which run, so only a new run key brings it back.
          self?.defaults.set(activity.content.state.runKey, forKey: "mewla.live.dismissedRun")
        }
        if update == .dismissed || update == .ended {
          self?.forgetObserved(activity.id)
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

/// NSLog-based so the lines show up in the device console with no configuration.
/// Never log the question text or choices.
enum LiveLog {
  static func push(_ message: String) { NSLog("MewlaLive push: %@", message) }
  static func update(_ message: String) { NSLog("MewlaLive activity: %@", message) }
}
