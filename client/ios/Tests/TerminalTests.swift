import UIKit
import XCTest
@testable import Mewla

/// `mewla.terminal` on the Simulator: libghostty-vt linked in, its cell runs, and the view's
/// grid, keys and drawing. With MEWLA_TEST_OUT set (TEST_RUNNER_MEWLA_TEST_OUT for
/// xcodebuild), the drawn terminal is saved there as terminal.png.
final class TerminalTests: XCTestCase {
  private let palette: [UInt32] = [0x1d1b19, 0xe0685a, 0x8fbf7f, 0xe3b55f, 0x7aa2f7, 0xc49bd8, 0x6fc1c1, 0xd8d2c8,
                                   0x57514a, 0xff8a7a, 0xa8d898, 0xf2cb7c, 0x9ab6ff, 0xd9b2ea, 0x8ed8d8, 0xf4f0ea]

  func testRunsCarryStylesAndWideCells() throws {
    let vt = try XCTUnwrap(Vt(cols: 20, rows: 4))
    XCTAssertTrue(vt.setTheme(foreground: 0xf4f0ea, background: 0x141210, cursor: 0xf4f0ea, palette: palette256(palette)))
    vt.write("a\u{1b}[31mb\u{1b}[0m\u{1b}[1;7mc\u{1b}[0m\r\n中x😀!")
    let frame = try XCTUnwrap(vt.frame(full: false))
    XCTAssertEqual(frame.rows, 4)
    XCTAssertEqual(frame.cols, 20)
    XCTAssertEqual(frame.cursorRow, 1)
    XCTAssertEqual(frame.cursorCol, 6)
    let row0 = try XCTUnwrap(frame.dirty.first { $0.row == 0 }).runs
    XCTAssertEqual(row0.map(\.text), ["a", "b", "c"])
    XCTAssertEqual(row0[1].fg, 0xffe0685a)
    XCTAssertTrue(row0[2].bold && row0[2].inverse)
    let row1 = try XCTUnwrap(frame.dirty.first { $0.row == 1 }).runs
    XCTAssertEqual(row1.map(\.col), [0, 2, 3, 5])
    XCTAssertEqual(row1.map(\.cells), [2, 1, 2, 15])
    XCTAssertEqual(row1[0].text, "中")
    XCTAssertEqual(row1[2].text, "😀")
    XCTAssertTrue(try XCTUnwrap(vt.frame(full: false)).dirty.isEmpty, "a second frame without output changes nothing")
  }

  func testConcealedTextIsNotDrawn() throws {
    // SGR 8: the daemon prints its "mewla-origin" marker concealed at the top of every new Session.
    let vt = try XCTUnwrap(Vt(cols: 20, rows: 4))
    XCTAssertTrue(vt.setTheme(foreground: 0xf4f0ea, background: 0x141210, cursor: 0xf4f0ea, palette: palette256(palette)))
    vt.write("A\u{1b}[8mHIDDEN\u{1b}[0mB\r\n\u{1b}[8;44mXY\u{1b}[0m")
    let frame = try XCTUnwrap(vt.frame(full: true))
    let texts = frame.dirty.flatMap(\.runs).map(\.text).joined()
    XCTAssertFalse(texts.contains("HIDDEN"))
    XCTAssertFalse(texts.contains("XY"))
    XCTAssertTrue(texts.contains("A") && texts.contains("B"))
    let row1 = try XCTUnwrap(frame.dirty.first { $0.row == 1 }).runs
    XCTAssertTrue(row1.contains { $0.bg != 0 && $0.underline == 0 }, "a concealed run keeps its background")
  }

  func testCssColors() {
    XCTAssertEqual(CssColor("#141210"), CssColor(rgb: 0x141210))
    XCTAssertEqual(CssColor("#3D7EFF55")?.rgb, 0x3D7EFF)
    XCTAssertEqual(CssColor("#3D7EFF55")?.alpha ?? 0, 0x55 / 255.0, accuracy: 0.001)
    XCTAssertNil(CssColor("red"))
    XCTAssertEqual(palette256(palette).count, 256)
  }

  @MainActor
  func testHardwareKeys() {
    XCTAssertEqual(TerminalView.keyData(code: .keyboardUpArrow, control: false, characters: ""), "\u{1b}[A")
    XCTAssertEqual(TerminalView.keyData(code: .keyboardLeftArrow, control: false, characters: ""), "\u{1b}[D")
    XCTAssertEqual(TerminalView.keyData(code: .keyboardEscape, control: false, characters: ""), "\u{1b}")
    XCTAssertEqual(TerminalView.keyData(code: .keyboardC, control: true, characters: "c"), "\u{3}")
    XCTAssertNil(TerminalView.keyData(code: .keyboardC, control: false, characters: "c"), "plain letters go to insertText")
  }

  @MainActor
  func testViewFitsGridSendsKeysAndDraws() throws {
    let view = TerminalView(frame: CGRect(x: 0, y: 0, width: 390, height: 300))
    var size: (cols: Int, rows: Int)?
    var input: [String] = []
    view.onSize = { size = ($0, $1) }
    view.onInput = { input.append($0) }
    view.setTheme(background: "#141210ff", foreground: "#f4f0ea", cursor: "#f4f0ea", selection: "#3D7EFF55",
                  palette: palette.map { String(format: "#%06x", $0) })
    view.write("\u{1b}[1;32muser@mewla\u{1b}[0m:\u{1b}[34m~\u{1b}[0m$ ls\r\n") // before the first layout: held
    view.setFontSize(13)
    view.setNeedsLayout()
    view.layoutIfNeeded()
    let fitted = try XCTUnwrap(size, "the first layout reports the grid")
    XCTAssertGreaterThan(fitted.cols, 30)
    XCTAssertGreaterThan(fitted.rows, 10)
    XCTAssertEqual(fitted.cols, Int(390 / view.cellW))
    XCTAssertEqual(view.lineText(0), "user@mewla:~$ ls")

    // Columns: 中文 0-3, emoji 5-9, 😀 11-12, inverse 14-20, under 22-26, italic 28-33, " red bg " 35-42.
    view.write("中文 emoji 😀 \u{1b}[7minverse\u{1b}[0m \u{1b}[4munder\u{1b}[0m \u{1b}[3mitalic\u{1b}[0m \u{1b}[41m red bg \u{1b}[0m\r\n$ ")
    view.insertText("ls\n")
    view.deleteBackward()
    view.setMarkedText("ni", selectedRange: NSRange(location: 2, length: 0))
    XCTAssertNotNil(view.markedTextRange)
    view.insertText("你")
    XCTAssertNil(view.markedTextRange)
    XCTAssertEqual(input, ["ls\r", "\u{7f}", "你"], "only committed text is sent, Return as \\r")

    let format = UIGraphicsImageRendererFormat()
    format.scale = 1
    format.preferredRange = .standard
    let image = UIGraphicsImageRenderer(bounds: view.bounds, format: format).image { _ in view.draw(view.bounds) }
    let pixels = try XCTUnwrap(Pixels(image))
    XCTAssertTrue(pixels.near(x: 385, y: 297, 0x141210), "the theme's background, opaque")
    XCTAssertTrue(pixels.near(x: Int(view.cellW * 39.5), y: Int(view.cellH * 1.5), 0xe0685a), "the red background run (palette 1)")
    // The cursor after "$ ": without the keyboard a hollow block in #f4f0ea, its cell left readable.
    let cursorEdge = (Int(view.cellW * 2) - 1...Int(view.cellW * 2) + 1).contains { pixels.near(x: $0, y: Int(view.cellH * 2.5), 0xf4f0ea, tolerance: 40) }
    XCTAssertTrue(cursorEdge, "the cursor's left edge after \"$ \"")
    XCTAssertTrue(pixels.near(x: Int(view.cellW * 2.5), y: Int(view.cellH * 2.5), 0x141210), "inside the hollow cursor")
    var lit = 0
    for x in 0..<Int(view.cellW * 10) where !pixels.near(x: x, y: Int(view.cellH * 0.5), 0x141210) { lit += 1 }
    XCTAssertGreaterThan(lit, 10, "the first line's text is drawn")
    XCTAssertEqual(view.lineText(1), "中文 emoji 😀 inverse under italic  red bg")

    if let dir = ProcessInfo.processInfo.environment["MEWLA_TEST_OUT"], let png = image.pngData() {
      try? png.write(to: URL(fileURLWithPath: dir).appendingPathComponent("terminal.png"))
    }
    let attachment = XCTAttachment(image: image)
    attachment.lifetime = .keepAlways
    add(attachment)

    view.reset()
    XCTAssertEqual(view.lineText(0), "", "reset starts an empty screen")
  }

  /// The render corpus (scripts/terminal-corpus, `MEWLA_CORPUS`): every screen drawn at its grid at
  /// 3x, saved as `MEWLA_TEST_OUT`/corpus/<name>.png for the side-by-side with the reference.
  @MainActor
  func testCorpusScreensDraw() throws {
    guard let corpus = ProcessInfo.processInfo.environment["MEWLA_CORPUS"] else { throw XCTSkip("MEWLA_CORPUS is not set") }
    let manifest = try JSONSerialization.jsonObject(with: Data(contentsOf: URL(fileURLWithPath: corpus).appendingPathComponent("corpus.json"))) as? [String: [String: Any]] ?? [:]
    let out = ProcessInfo.processInfo.environment["MEWLA_TEST_OUT"].map { URL(fileURLWithPath: $0).appendingPathComponent("corpus") }
    if let out { try FileManager.default.createDirectory(at: out, withIntermediateDirectories: true) }
    for name in manifest.keys.sorted() {
      let cols = manifest[name]?["cols"] as? Int ?? 52, rows = manifest[name]?["rows"] as? Int ?? 30
      let bytes = try String(contentsOf: URL(fileURLWithPath: corpus).appendingPathComponent("\(name).vt"), encoding: .utf8)
      let view = TerminalView(frame: CGRect(x: 0, y: 0, width: 100, height: 100))
      view.setTheme(background: "#141210", foreground: "#f4f0ea", cursor: "#f4f0ea", selection: "#3D7EFF55",
                    palette: palette.map { String(format: "#%06x", $0) })
      view.setFontSize(13)
      view.frame = CGRect(x: 0, y: 0, width: ceil(CGFloat(cols) * view.cellW), height: ceil(CGFloat(rows) * view.cellH))
      view.setNeedsLayout()
      view.layoutIfNeeded()
      view.write(bytes)
      let format = UIGraphicsImageRendererFormat()
      format.scale = 3
      format.preferredRange = .standard
      let image = UIGraphicsImageRenderer(bounds: view.bounds, format: format).image { _ in view.draw(view.bounds) }
      if let out, let png = image.pngData() { try png.write(to: out.appendingPathComponent("\(name).png")) }
    }
  }
}

/// An image's pixels, read as 0xRRGGBB at top-left-origin coordinates.
private struct Pixels {
  let width: Int
  let height: Int
  let data: [UInt8]

  init?(_ image: UIImage) {
    guard let cg = image.cgImage else { return nil }
    let width = cg.width, height = cg.height
    var data = [UInt8](repeating: 0, count: width * height * 4)
    let drawn = data.withUnsafeMutableBytes { (bytes) -> Bool in
      guard let ctx = CGContext(data: bytes.baseAddress, width: width, height: height, bitsPerComponent: 8,
                                bytesPerRow: width * 4, space: CGColorSpaceCreateDeviceRGB(),
                                bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue) else { return false }
      ctx.draw(cg, in: CGRect(x: 0, y: 0, width: width, height: height))
      return true
    }
    guard drawn else { return nil }
    self.width = width
    self.height = height
    self.data = data
  }

  func near(x: Int, y: Int, _ rgb: UInt32, tolerance: Int = 3) -> Bool {
    let i = (min(max(y, 0), height - 1) * width + min(max(x, 0), width - 1)) * 4
    let want = [Int(rgb >> 16 & 0xff), Int(rgb >> 8 & 0xff), Int(rgb & 0xff)]
    return (0..<3).allSatisfy { abs(Int(data[i + $0]) - want[$0]) <= tolerance }
  }
}
