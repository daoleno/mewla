// Compiled into both the app and the widget extension (project.yml lists Shared/ in each).
import Foundation

/// What the live surfaces show of a live state (the owner's pick 2026-11-13, option A of
/// worklog/mewla-live-widgets-1113): the Live Activity and the Dynamic Island show the one thing that
/// matters now, the widgets show the tray. The same rules as Android's `LiveView.kt`.
enum MewlaLiveView {
  /// The rows to draw: the daemon's items, or, from a daemon that predates them, its older fields.
  static func items(_ state: MewlaLiveState) -> [MewlaLiveItem] {
    if !state.items.isEmpty { return state.items }
    if state.needsYou, let question = state.question {
      return [MewlaLiveItem(key: "work:\(question.workId)", open: "work", id: question.workId, title: state.title.isEmpty ? "Brain" : state.title,
                            line: question.text, agent: "brain", needs: "answer", ring: nil, startedAt: 0)]
    }
    if state.kind == MewlaLiveState.running {
      return [MewlaLiveItem(key: "legacy", open: "brain", id: "", title: state.title.isEmpty ? "Brain" : state.title,
                            line: state.phase, agent: "brain", needs: "", ring: nil, startedAt: state.startedAt)]
    }
    return []
  }

  /// What needs you (newest) else what started last.
  static func focus(_ state: MewlaLiveState) -> MewlaLiveItem? { items(state).first }

  static func runningCount(_ state: MewlaLiveState) -> Int {
    if !state.items.isEmpty || state.running > 0 { return state.running }
    return state.kind == MewlaLiveState.running ? 1 : 0
  }

  /// The app header's words: "1 needs you", "3 need you", "2 running", "1 queued, 1 waiting", "All quiet".
  static func headerWords(_ state: MewlaLiveState) -> String {
    let needs = state.needsYou ? max(state.needsCount, 1) : state.needsCount
    if needs > 0 { return needs == 1 ? "1 needs you" : "\(needs) need you" }
    let running = runningCount(state)
    if running > 0 { return "\(running) running" }
    let quiet = [state.queued > 0 ? "\(state.queued) queued" : nil, state.waiting > 0 ? "\(state.waiting) waiting" : nil].compactMap { $0 }
    return quiet.isEmpty ? "All quiet" : quiet.joined(separator: ", ")
  }

  /// The row tag a request carries, as the tray's.
  static func tag(_ item: MewlaLiveItem) -> String { item.needs == "approve" ? "Approve" : "Answer" }

  /// The choices that answer from the surface: only the Work question the daemon takes an answer for.
  static func choices(_ state: MewlaLiveState, _ item: MewlaLiveItem) -> [String] {
    guard let question = state.question, item.asks, item.key == "work:\(question.workId)" else { return [] }
    return Array(question.choices.prefix(3))
  }

  /// "+2 running": the others the small widget does not show.
  static func othersRunning(_ state: MewlaLiveState, _ focus: MewlaLiveItem) -> String {
    let others = runningCount(state) - (focus.asks ? 0 : 1)
    return others > 0 ? "+\(others) running" : ""
  }

  /// Where a tap goes: the item's Session or Work, else Brain.
  static func link(_ item: MewlaLiveItem?) -> URL {
    guard let item, !item.id.isEmpty else { return URL(string: "mewla://brain")! }
    let id = item.id.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed.subtracting(CharacterSet(charactersIn: "/?#"))) ?? item.id
    switch item.open {
    case "session": return URL(string: "mewla://sessions/\(id)") ?? URL(string: "mewla://brain")!
    case "work": return URL(string: "mewla://brain/work/\(id)") ?? URL(string: "mewla://brain")!
    default: return URL(string: "mewla://brain")!
    }
  }

  /// The tray: the header opens it.
  static let trayURL = URL(string: "mewla://sessions")!

  /// mewla://live/answer?q=<question>&c=<choice index>: the app opens on the question and sends the answer once.
  static func answerURL(_ question: MewlaLiveQuestion, choice: Int) -> URL {
    var components = URLComponents()
    components.scheme = "mewla"
    components.host = "live"
    components.path = "/answer"
    components.queryItems = [URLQueryItem(name: "q", value: question.id), URLQueryItem(name: "c", value: String(choice))]
    return components.url ?? URL(string: "mewla://live")!
  }
}
