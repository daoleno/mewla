import NativeShell
import UIKit

/// `mewla.terminal` (client/ui/terminal/contract.ts): the native terminal view as an NSH component.
/// The VT is freed with the view (NSH's iOS components have no destroy).
final class TerminalComponent: NshComponent {
  private let terminal = TerminalView(frame: .zero)
  var view: UIView { terminal }

  init(_ events: NshComponentContext) {
    terminal.onSize = { cols, rows in events.emit("size", ["cols": cols, "rows": rows]) }
    terminal.onFailed = { message in events.emit("failed", ["message": message]) }
    terminal.onInput = { data in events.emit("input", ["data": data]) }
    terminal.onScroll = { lines in events.emit("scroll", ["lines": lines]) }
    terminal.onViewport = { top, atBottom in events.emit("viewport", ["top": top, "atBottom": atBottom] as [String: Any]) }
    terminal.onTapCell = { col, row, text, index in
      events.emit("tapCell", ["col": col, "row": row, "text": text, "index": index] as [String: Any])
    }
    terminal.onCopy = { text in events.emit("copy", ["text": text]) }
  }

  func setProp(_ name: String, _ value: Any?) {
    switch name {
    case "fontSize":
      if let size = value as? NSNumber { terminal.setFontSize(CGFloat(size.doubleValue)) }
    case "theme":
      guard let theme = value as? [String: Any],
            let background = theme["background"] as? String, let foreground = theme["foreground"] as? String,
            let cursor = theme["cursor"] as? String, let selection = theme["selection"] as? String else { return }
      let palette = (theme["palette"] as? [Any])?.compactMap { $0 as? String } ?? []
      terminal.setTheme(background: background, foreground: foreground, cursor: cursor, selection: selection, palette: palette)
    default:
      break
    }
  }

  func command(_ name: String, _ args: Any?) {
    let args = args as? [String: Any]
    switch name {
    case "write":
      if let data = args?["data"] as? String { terminal.write(data) }
    case "reset":
      terminal.reset()
    case "history":
      terminal.applyHistory(
        reset: (args?["reset"] as? Bool) ?? false,
        removed: (args?["removed"] as? NSNumber)?.intValue ?? 0,
        rows: (args?["rows"] as? [Any])?.map { ($0 as? String) ?? "" } ?? [],
        cols: (args?["cols"] as? NSNumber)?.intValue ?? 0,
        alternate: (args?["alternate"] as? Bool) ?? false)
    case "scrollTo":
      if let row = args?["row"] as? NSNumber { terminal.scrollTo(row: row.intValue) }
    case "scrollToBottom":
      terminal.scrollToBottom()
    case "mark":
      terminal.mark(row: (args?["row"] as? NSNumber)?.intValue ?? -1)
    case "focus":
      terminal.showKeyboard()
    case "blur":
      terminal.hideKeyboard()
    case "retry":
      terminal.retry()
    case "paste":
      if let text = args?["text"] as? String { terminal.paste(text) }
    default:
      break
    }
  }
}
