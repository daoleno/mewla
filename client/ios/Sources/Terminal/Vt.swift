import UIKit

/// One run of cells in one style, from libghostty-vt (TerminalVt.h).
struct TerminalRun {
  let col: Int
  let cells: Int
  /// 0xAARRGGBB; `bg` 0 is the theme's background.
  let fg: UInt32
  let bg: UInt32
  let flags: Int
  let text: String

  var bold: Bool { flags & 1 != 0 }
  var italic: Bool { flags & 2 != 0 }
  var underline: Bool { flags & 4 != 0 }
  var inverse: Bool { flags & 8 != 0 }
}

/// What changed since the last frame: the rows in `dirty` replace the view's rows.
struct TerminalFrame {
  let rows: Int
  let cols: Int
  let cursorCol: Int
  let cursorRow: Int
  let cursorVisible: Bool
  let full: Bool
  let dirty: [(row: Int, runs: [TerminalRun])]
}

/// libghostty-vt through TerminalVt.cpp. One per terminal view; freed with it.
final class Vt {
  private let handle: OpaquePointer

  init?(cols: Int, rows: Int) {
    guard (1...Int(UInt16.max)).contains(cols), (1...Int(UInt16.max)).contains(rows),
          let handle = mewla_vt_create(UInt16(cols), UInt16(rows)) else { return nil }
    self.handle = handle
  }

  deinit { mewla_vt_destroy(handle) }

  /// Cell sizes in pixels.
  func resize(cols: Int, rows: Int, cellWidth: CGFloat, cellHeight: CGFloat) {
    guard (1...Int(UInt16.max)).contains(cols), (1...Int(UInt16.max)).contains(rows) else { return }
    mewla_vt_resize(handle, UInt16(cols), UInt16(rows), Double(cellWidth), Double(cellHeight))
  }

  /// Opaque 0xRRGGBB colors; `palette` is the 256 of palette256(_:).
  @discardableResult
  func setTheme(foreground: UInt32, background: UInt32, cursor: UInt32, palette: [UInt32]) -> Bool {
    palette.withUnsafeBufferPointer { (buffer) -> Bool in
      guard let base = buffer.baseAddress else { return false }
      return mewla_vt_set_theme(handle, foreground, background, cursor, base, buffer.count)
    }
  }

  func write(_ data: String) {
    var data = data
    data.withUTF8 { bytes in mewla_vt_write(handle, bytes.baseAddress, bytes.count) }
  }

  func frame(full: Bool) -> TerminalFrame? {
    var out = MewlaVtFrame()
    guard mewla_vt_frame(handle, full, &out), let metaBase = out.meta, out.meta_count >= 7 else { return nil }
    let m = UnsafeBufferPointer(start: metaBase, count: out.meta_count)
    let text = UnsafeBufferPointer(start: out.text, count: out.text_count)
    var dirty: [(row: Int, runs: [TerminalRun])] = []
    dirty.reserveCapacity(Int(m[6]))
    var p = 7
    for _ in 0..<Int(m[6]) {
      guard p + 1 < m.count else { break }
      let row = Int(m[p])
      let count = Int(m[p + 1])
      p += 2
      var runs: [TerminalRun] = []
      runs.reserveCapacity(count)
      for _ in 0..<count {
        guard p + 6 < m.count else { break }
        let start = Int(m[p + 5])
        let length = Int(m[p + 6])
        let units: UnsafeBufferPointer<UInt16> = start >= 0 && length >= 0 && start + length <= text.count
          ? UnsafeBufferPointer(rebasing: text[start..<(start + length)]) : UnsafeBufferPointer(start: nil, count: 0)
        runs.append(TerminalRun(col: Int(m[p]), cells: Int(m[p + 1]), fg: UInt32(bitPattern: m[p + 2]),
                                bg: UInt32(bitPattern: m[p + 3]), flags: Int(m[p + 4]),
                                text: String(decoding: units, as: UTF16.self)))
        p += 7
      }
      dirty.append((row, runs))
    }
    return TerminalFrame(rows: Int(m[0]), cols: Int(m[1]), cursorCol: Int(m[2]), cursorRow: Int(m[3]),
                         cursorVisible: m[4] == 1, full: m[5] == 1, dirty: dirty)
  }
}

/// A theme color: CSS hex `#rrggbb` or `#rrggbbaa` (alpha last, what the app's `token()` gives).
struct CssColor: Equatable {
  /// 0xRRGGBB, what the VT takes (it drops the alpha).
  let rgb: UInt32
  let alpha: CGFloat

  init(rgb: UInt32, alpha: CGFloat = 1) {
    self.rgb = rgb
    self.alpha = alpha
  }

  init?(_ value: String) {
    let hex = value.trimmingCharacters(in: .whitespaces)
    guard hex.hasPrefix("#"), let v = UInt32(hex.dropFirst(), radix: 16) else { return nil }
    switch hex.count {
    case 7: self.init(rgb: v)
    case 9: self.init(rgb: v >> 8, alpha: CGFloat(v & 0xff) / 255)
    default: return nil
    }
  }

  var color: UIColor { UIColor(argb: rgb, alpha: alpha) }
}

extension UIColor {
  /// From 0x..RRGGBB (the top byte is ignored).
  convenience init(argb: UInt32, alpha: CGFloat = 1) {
    self.init(red: CGFloat((argb >> 16) & 0xff) / 255, green: CGFloat((argb >> 8) & 0xff) / 255,
              blue: CGFloat(argb & 0xff) / 255, alpha: alpha)
  }
}

/// The 256-color palette from the theme's 16: xterm's 6×6×6 cube and gray ramp after them.
func palette256(_ ansi: [UInt32]) -> [UInt32] {
  var out: [UInt32] = (0..<16).map { $0 < ansi.count ? ansi[$0] : 0x808080 }
  let steps: [UInt32] = [0, 95, 135, 175, 215, 255]
  for r in steps { for g in steps { for b in steps { out.append(r << 16 | g << 8 | b) } } }
  for i in 0..<24 {
    let v = UInt32(8 + i * 10)
    out.append(v << 16 | v << 8 | v)
  }
  return out
}
