import Foundation

/// What a widget shows, and where it comes from: a server a widget can fetch is read from its
/// read-only snapshot endpoint; a Mewla Link server shows the state the app and pushes cached.
struct MewlaWidgetSnapshot {
  var state: MewlaLiveState?
  /// True when there is nothing trustworthy to draw: never configured, revoked, or too old.
  var needsApp: Bool
}

enum MewlaWidgetData {
  /// A Link-paired widget only shows what was pushed; past this age it asks to open the app.
  static let staleAfter: TimeInterval = 12 * 3600

  private struct Config: Decodable {
    var mode: String
    var base_url: String?
    var token: String?
  }

  static func load() async -> MewlaWidgetSnapshot {
    guard let configURL = MewlaLiveShared.widgetConfigURL,
      let data = try? Data(contentsOf: configURL),
      let config = try? JSONDecoder().decode(Config.self, from: data)
    else {
      return MewlaWidgetSnapshot(state: nil, needsApp: true)
    }
    if config.mode == "fetch", let base = config.base_url, let token = config.token {
      switch await fetch(baseURL: base, token: token) {
      case .state(let state):
        store(state)
        return MewlaWidgetSnapshot(state: state, needsApp: false)
      case .revoked:
        // The daemon refused the token for good: clear everything and say so. The app does not
        // issue a new one until the server is switched.
        revoke()
        return MewlaWidgetSnapshot(state: nil, needsApp: true)
      case .unreachable:
        break
      }
    }
    return cached()
  }

  private enum FetchResult {
    case state(MewlaLiveState)
    case revoked
    case unreachable
  }

  private static func fetch(baseURL: String, token: String) async -> FetchResult {
    var base = baseURL
    while base.hasSuffix("/") { base.removeLast() }
    guard let url = URL(string: base + "/widget/snapshot") else { return .unreachable }
    var request = URLRequest(url: url, timeoutInterval: 10)
    request.setValue("Bearer " + token, forHTTPHeaderField: "Authorization")
    request.cachePolicy = .reloadIgnoringLocalCacheData
    do {
      let (data, response) = try await URLSession.shared.data(for: request)
      guard let http = response as? HTTPURLResponse else { return .unreachable }
      if http.statusCode == 401 { return .revoked }
      guard http.statusCode == 200, let state = MewlaLiveState.parseSnapshot(data) else {
        return .unreachable
      }
      return .state(state)
    } catch {
      return .unreachable
    }
  }

  /// Keep the newest state the app, a push or this fetch has seen, so a late fetch never
  /// overwrites a newer pushed state.
  private static func store(_ state: MewlaLiveState) {
    if let last = MewlaLiveShared.readState(), !state.isNewer(than: last) { return }
    if let data = try? JSONEncoder().encode(state) {
      MewlaLiveShared.write(data, to: MewlaLiveShared.stateURL)
    }
  }

  private static func revoke() {
    let manager = FileManager.default
    for url in [MewlaLiveShared.widgetConfigURL, MewlaLiveShared.stateURL] {
      if let url { try? manager.removeItem(at: url) }
    }
    MewlaLiveShared.write(Data(), to: MewlaLiveShared.revokedMarkerURL)
  }

  private static func cached() -> MewlaWidgetSnapshot {
    guard let url = MewlaLiveShared.stateURL, let state = MewlaLiveShared.readState() else {
      return MewlaWidgetSnapshot(state: nil, needsApp: true)
    }
    let modified =
      (try? url.resourceValues(forKeys: [.contentModificationDateKey]))?.contentModificationDate
    if let modified, Date().timeIntervalSince(modified) > staleAfter {
      return MewlaWidgetSnapshot(state: nil, needsApp: true)
    }
    return MewlaWidgetSnapshot(state: state, needsApp: false)
  }
}
