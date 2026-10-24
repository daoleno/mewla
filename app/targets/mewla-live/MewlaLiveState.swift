// This file is compiled into both the app's MewlaLive module and the widget extension
// (app/targets/mewla-live/MewlaLiveState.swift). Keep the two byte-identical;
// app/modules/mewla-live/nativeContract.test.ts checks it.
import ActivityKit
import Foundation

struct MewlaLiveWorker: Codable, Hashable {
  var name: String
  var phase: String
}

struct MewlaLiveQuestion: Codable, Hashable {
  var id: String
  var kind: String
  var text: String
  var choices: [String]

  enum CodingKeys: String, CodingKey { case id, kind, text, choices }

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    id = try c.decodeIfPresent(String.self, forKey: .id) ?? ""
    kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? ""
    text = try c.decodeIfPresent(String.self, forKey: .text) ?? ""
    choices = try c.decodeIfPresent([String].self, forKey: .choices) ?? []
  }
}

/// The daemon's live_state projection (daemon/live), field for field. An ActivityKit push carries
/// this same object as its content-state, so the wire shape is the one shape on every surface.
struct MewlaLiveState: Codable, Hashable {
  static let idle = "idle"
  static let running = "running"
  static let needsYou = "needs_you"

  var kind: String
  var epoch: Int64
  var seq: Int64
  var runKey: String
  var title: String
  var phase: String
  /// Unix milliseconds of the earliest running Worker.
  var startedAt: Int64
  var done: Int
  var total: Int
  var workers: [MewlaLiveWorker]
  var workerCount: Int
  var needsCount: Int
  var question: MewlaLiveQuestion?

  enum CodingKeys: String, CodingKey {
    case kind, epoch, seq, title, phase, done, total, workers, question
    case runKey = "run_key"
    case startedAt = "started_at"
    case workerCount = "worker_count"
    case needsCount = "needs_count"
  }

  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    kind = try c.decode(String.self, forKey: .kind)
    epoch = try c.decodeIfPresent(Int64.self, forKey: .epoch) ?? 0
    seq = try c.decodeIfPresent(Int64.self, forKey: .seq) ?? 0
    runKey = try c.decodeIfPresent(String.self, forKey: .runKey) ?? ""
    title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
    phase = try c.decodeIfPresent(String.self, forKey: .phase) ?? ""
    startedAt = try c.decodeIfPresent(Int64.self, forKey: .startedAt) ?? 0
    done = try c.decodeIfPresent(Int.self, forKey: .done) ?? 0
    total = try c.decodeIfPresent(Int.self, forKey: .total) ?? 0
    workers = try c.decodeIfPresent([MewlaLiveWorker].self, forKey: .workers) ?? []
    workerCount = try c.decodeIfPresent(Int.self, forKey: .workerCount) ?? 0
    needsCount = try c.decodeIfPresent(Int.self, forKey: .needsCount) ?? 0
    question = try c.decodeIfPresent(MewlaLiveQuestion.self, forKey: .question)
  }

  var isIdle: Bool { kind == Self.idle }
  var needsYou: Bool { kind == Self.needsYou }
  var startedDate: Date? { startedAt > 0 ? Date(timeIntervalSince1970: Double(startedAt) / 1000) : nil }

  /// States are ordered by (epoch, seq); a restart bumps the epoch, so an old state never looks newer.
  func isNewer(than other: MewlaLiveState) -> Bool {
    epoch != other.epoch ? epoch > other.epoch : seq > other.seq
  }

  static func parse(_ raw: String) -> MewlaLiveState? {
    guard let data = raw.data(using: .utf8) else { return nil }
    return try? JSONDecoder().decode(MewlaLiveState.self, from: data)
  }

  /// The state inside the widget endpoint's {"live": <State>, "generated_at": ms}.
  static func parseSnapshot(_ data: Data) -> MewlaLiveState? {
    struct Envelope: Decodable { let live: MewlaLiveState }
    return try? JSONDecoder().decode(Envelope.self, from: data).live
  }
}

struct MewlaLiveAttributes: ActivityAttributes {
  typealias ContentState = MewlaLiveState
  /// One activity per run; the run key never changes for its lifetime.
  var runKey: String
}

/// App Group storage shared by the app and the widget extension. The extension's bundle id is the
/// app's plus ".live", and both use the group "group.<app bundle id>".
enum MewlaLiveShared {
  static let extensionSuffix = ".live"

  static var appBundleId: String {
    let id = Bundle.main.bundleIdentifier ?? ""
    return id.hasSuffix(extensionSuffix) ? String(id.dropLast(extensionSuffix.count)) : id
  }

  static var groupId: String { "group." + appBundleId }

  /// True only in a build that ships the Live Activity extension (its Info.plist key and App Group).
  static var isEnabled: Bool {
    (Bundle.main.object(forInfoDictionaryKey: "NSSupportsLiveActivities") as? Bool) == true
      && container != nil
  }

  static var container: URL? {
    FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: groupId)
  }

  /// The last applied state, as the JSON the widget reads.
  static var stateURL: URL? { container?.appendingPathComponent("live_state.json") }
  /// {"base_url": ..., "token": ...} for a server a widget can fetch; absent for Mewla Link.
  static var widgetConfigURL: URL? { container?.appendingPathComponent("widget_config.json") }
  /// Written by the extension when the daemon refuses the widget token; removed when a new one is configured.
  static var revokedMarkerURL: URL? { container?.appendingPathComponent("widget_revoked") }

  static func write(_ data: Data, to url: URL?) {
    guard let url else { return }
    // Readable after the first unlock so a widget refresh on a locked phone still works.
    try? data.write(to: url, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
  }

  static func readState() -> MewlaLiveState? {
    guard let url = stateURL, let data = try? Data(contentsOf: url) else { return nil }
    return try? JSONDecoder().decode(MewlaLiveState.self, from: data)
  }
}
