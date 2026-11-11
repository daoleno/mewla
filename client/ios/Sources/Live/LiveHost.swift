import ActivityKit
import NativeShell
import UIKit
import WidgetKit

/// `live.*`: the Live Activity and the home-screen widgets of the current computer (LiveStore).
enum LiveHost {
  @MainActor
  static func register(_ app: NshAppView) {
    let store = LiveStore.shared
    app.handle("live.supported") { _ -> Any? in LiveStore.supported }
    app.handle("live.apply") { data -> Any? in
      guard MewlaLiveShared.isEnabled, let state = MewlaLiveState.parse(try field(data, "state")) else { return false }
      return store.apply(state)
    }
    app.handle("live.setCurrentServer") { data -> Any? in
      store.setCurrentServer(try field(data, "serverId"))
      return nil
    }
    app.handle("live.configureWidget") { data -> Any? in
      store.configureWidget(baseUrl: try field(data, "baseUrl").trimmingCharacters(in: .whitespaces), token: try field(data, "token"))
      return nil
    }
    // A computer reached through Mewla Relay has no address a widget can fetch: its widgets show the pushed state.
    app.handle("live.configureWidgetPushed") { _ -> Any? in
      store.configureWidgetPushed()
      return nil
    }
    app.handle("live.clearWidget") { _ -> Any? in
      store.clearWidget()
      return nil
    }
    app.handle("live.refreshWidgets") { _ -> Any? in
      WidgetCenter.shared.reloadAllTimelines()
      return nil
    }
    app.handle("live.widgetState") { _ -> Any? in store.widgetState() }
    app.handle("live.diagnostics") { _ -> Any? in
      var enabled = false
      if #available(iOS 16.1, *) { enabled = ActivityAuthorizationInfo().areActivitiesEnabled }
      return [
        "sdk": ProcessInfo.processInfo.operatingSystemVersion.majorVersion,
        "notificationsEnabled": await PushHost.allowed(),
        "promotionAllowed": enabled,
        "extension": LiveStore.extensionEmbedded,
        "appGroup": MewlaLiveShared.container != nil,
      ] as [String: Any]
    }
  }

  static func field(_ data: Any?, _ name: String) throws -> String {
    guard let value = (data as? [String: Any])?[name] as? String else {
      throw NSError(domain: "Mewla", code: 2, userInfo: [NSLocalizedDescriptionKey: "\(name) is required"])
    }
    return value
  }
}
