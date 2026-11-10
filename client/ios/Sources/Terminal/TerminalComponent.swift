import NativeShell
import UIKit

/// `mewla.terminal` (client/ui/terminal/contract.ts): the native terminal view as an NSH component.
/// The VT is freed with the view (NSH's iOS components have no destroy).
final class TerminalComponent: NshComponent {
  private let terminal = TerminalView(frame: .zero)
  var view: UIView { terminal }

  init(_ events: NshComponentContext) {
    terminal.onSize = { cols, rows in events.emit("size", ["cols": cols, "rows": rows]) }
    terminal.onInput = { data in events.emit("input", ["data": data]) }
    terminal.onScroll = { lines in events.emit("scroll", ["lines": lines]) }
    terminal.onTapCell = { col, row in events.emit("tapCell", ["col": col, "row": row]) }
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
    case "focus":
      terminal.showKeyboard()
    case "blur":
      terminal.hideKeyboard()
    default:
      break
    }
  }
}
