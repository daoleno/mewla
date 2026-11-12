import CoreText
import UIKit

/// A terminal drawn with CoreText from libghostty-vt's cells (client/native/terminal; the iOS twin
/// of client/android/.../terminal/TerminalView.kt), in the bundled Maple Mono NF on an exact grid of
/// whole-pixel cells: each cell's glyph at its column, CJK and emoji centred in their two cells, box
/// drawing, blocks, braille and Powerline drawn from TerminalGlyphs so lines meet, every attribute
/// (faint, italic, five underline styles in their own color, strikethrough, overline, inverse) and
/// the program's cursor shape.
///
/// The system keyboard types into it through UITextInput (composition drawn at the cursor, only
/// committed text reported). Above the live screen it draws the history the page hands it
/// (`applyHistory`, styled rows rendered by a second VT) and a drag scrolls through it point by
/// point, with deceleration; with no history (or on the alternate screen) the drag is reported (the
/// computer's tmux copy mode moves). A tap reports the cell and the text under it (the page opens
/// links and raises the keyboard); a long press selects the word under the finger, with handles to
/// stretch it over any rows, history included, and offers Copy, Paste and Select All. Text a program
/// copies (OSC 52) is reported.
final class TerminalView: UIView, UITextInput, UIEditMenuInteractionDelegate {
  var onSize: (_ cols: Int, _ rows: Int) -> Void = { _, _ in }
  var onFailed: (_ message: String) -> Void = { _ in }
  var onInput: (_ data: String) -> Void = { _ in }
  var onScroll: (_ lines: Int) -> Void = { _ in }
  /// The history row at the top (the history's length at the bottom).
  var onViewport: (_ top: Int, _ atBottom: Bool) -> Void = { _, _ in }
  /// `row` is the live screen's, -1 on a history row; `index` is the tapped cell's UTF-16 index in `text`, -1 past it.
  var onTapCell: (_ col: Int, _ row: Int, _ text: String, _ index: Int) -> Void = { _, _, _, _ in }
  var onCopy: (_ text: String) -> Void = { _ in }

  /// What a run draws, laid out once for its color.
  private enum Item {
    case text(CTLine, x: CGFloat)
    case fitted(CTLine, x: CGFloat, width: CGFloat, box: CGFloat)
    case glyph(UInt32, x: CGFloat)
  }

  /// A run and its items, made on first draw.
  private final class Cell {
    let run: TerminalRun
    var items: [Item]?
    init(_ run: TerminalRun) { self.run = run }
  }

  private struct Line {
    var cells: [Cell]
    var wrapped: Bool
    static let empty = Line(cells: [], wrapped: false)
  }

  private var vt: Vt?
  /// Output that arrived before the first layout made the VT.
  private var pending: [String] = []
  private(set) var cols = 0
  private(set) var rows = 0
  private var grid: [Line] = []
  private(set) var cursorCol = 0
  private(set) var cursorRow = 0
  private var cursorVisible = true
  private var cursorStyle = 1
  private var cursorBlinks = false
  private var blinkOn = true
  private var blinkTimer: Timer?
  private var needsPull = false
  private var needsFull = false

  // The font: cells are whole pixels and the text size is set so one advance is exactly one cell.
  private var points: CGFloat = 8
  private var pixelScale: CGFloat = 2
  private var ctFont: CTFont = TerminalFonts.regular(8)
  private var ctBold: CTFont = TerminalFonts.bold(8)
  /// Maple's real italic (Latin, Greek, Cyrillic, punctuation); a character it lacks is drawn from the
  /// upright face slanted (`ctSlant`, `ctBoldSlant`).
  private var ctItalic: CTFont = TerminalFonts.regular(8)
  private var ctBoldItalic: CTFont = TerminalFonts.bold(8)
  private var ctSlant: CTFont = TerminalFonts.regular(8)
  private var ctBoldSlant: CTFont = TerminalFonts.bold(8)
  /// The regular face as a UIFont (the composition's width).
  private var font: UIFont { ctFont as UIFont }
  private(set) var cellW: CGFloat = 0
  private(set) var cellH: CGFloat = 0
  private var baseline: CGFloat = 0
  /// The one line weight (box drawing, underline, strikethrough, cursor), in points.
  private var thickness: CGFloat = 1
  /// Whether the bundled face has each BMP character (then its advance is one cell), per face:
  /// regular, bold, italic, bold italic.
  private var inFont: [[UInt16: Bool]] = [[:], [:], [:], [:]]
  private var glyphOps: [UInt32: [MewlaGlyphOp]] = [:]

  private var background = CssColor(rgb: 0x141210)
  private var foreground = CssColor(rgb: 0xF4F0EA)
  private var cursorColor = CssColor(rgb: 0xF4F0EA)
  private var selectionColor = CssColor(rgb: 0x3D7EFF, alpha: 0.33)
  private var vtPalette: [UInt32]?

  private var marked = ""
  private var markedSelection = NSRange(location: 0, length: 0)
  private lazy var editMenu = UIEditMenuInteraction(delegate: self)
  private var failed = false

  // The history above the live screen: its styled rows as the page sent them and, as long, the ones
  // drawn as lines (nil until first needed: historyLine), oldest first. Content line i is history
  // row i below H, live row i - H above.
  private var historyAnsi: [String] = []
  private var history: [Line?] = []
  private var historyCols = 0
  private var alternate = false
  /// The VT that renders history rows, `page` rows tall at the history's width.
  private var scratch: Vt?
  /// Points the content is scrolled down from the live screen (0 = the live screen at the bottom).
  private var scrollY: CGFloat = 0
  private var velocity: CGFloat = 0
  private var decelerator: CADisplayLink?
  private var dragRemainder: CGFloat = 0
  private var markRow = -1
  private var reportedTop = -1
  private var reportedBottom = true
  private static let page = 64

  // Selection in content lines and columns, inclusive.
  private var selFrom: (line: Int, col: Int)?
  private var selTo: (line: Int, col: Int)?
  /// The handle being dragged: 0 none, 1 start, 2 end.
  private var dragging = 0
  private var menuPoint = CGPoint.zero

  override init(frame: CGRect) {
    super.init(frame: frame)
    isOpaque = true
    contentMode = .redraw
    backgroundColor = background.color
    addGestureRecognizer(UITapGestureRecognizer(target: self, action: #selector(tapped(_:))))
    let pan = UIPanGestureRecognizer(target: self, action: #selector(panned(_:)))
    pan.maximumNumberOfTouches = 1
    addGestureRecognizer(pan)
    addGestureRecognizer(UILongPressGestureRecognizer(target: self, action: #selector(pressed(_:))))
    addInteraction(editMenu)
    applyFont(8) // the grid comes with the first layout, once the callbacks are set
  }

  @available(*, unavailable)
  required init?(coder: NSCoder) { fatalError("not from a storyboard") }

  deinit {
    blinkTimer?.invalidate()
    decelerator?.invalidate()
  }

  // MARK: Props and commands

  /// Text size in points (the shipped app's 8 CSS px is the default): the grid follows from it.
  func setFontSize(_ points: CGFloat) {
    applyFont(points)
    needsFull = true
    schedulePull()
    relayout()
  }

  private var screenScale: CGFloat {
    let s = traitCollection.displayScale
    return s > 0 ? s : max(window?.screen.scale ?? UIScreen.main.scale, 1)
  }

  private func applyFont(_ size: CGFloat) {
    points = max(size, 4)
    pixelScale = screenScale
    let probe = TerminalFonts.regular(points)
    let advance = Self.advance(of: 0x4D, in: probe) // "M"
    let cellPx = max(1, (advance * pixelScale).rounded())
    let snapped = points * (cellPx / pixelScale) / max(advance, 0.1)
    ctFont = TerminalFonts.regular(snapped)
    ctBold = TerminalFonts.bold(snapped)
    ctSlant = Self.slanted(ctFont)
    ctBoldSlant = Self.slanted(ctBold)
    ctItalic = TerminalFonts.italic(snapped, bold: false) ?? ctSlant
    ctBoldItalic = TerminalFonts.italic(snapped, bold: true) ?? ctBoldSlant
    cellW = cellPx / pixelScale
    let ascent = CTFontGetAscent(ctFont), descent = CTFontGetDescent(ctFont)
    let cellHPx = max(1, ((ascent + descent) * pixelScale).rounded())
    cellH = cellHPx / pixelScale
    baseline = (((cellH - ascent - descent) / 2 + ascent) * pixelScale).rounded() / pixelScale
    thickness = CGFloat(mewla_glyph_thickness(Int32(cellHPx))) / pixelScale
    inFont = [[:], [:], [:], [:]]
    glyphOps.removeAll()
    for line in grid { line.cells.forEach { $0.items = nil } }
    for line in history { line?.cells.forEach { $0.items = nil } }
  }

  private static func advance(of unit: UniChar, in font: CTFont) -> CGFloat {
    var u = unit
    var glyph = CGGlyph(0)
    CTFontGetGlyphsForCharacters(font, &u, &glyph, 1)
    let a = CGFloat(CTFontGetAdvancesForGlyphs(font, .horizontal, &glyph, nil, 1))
    return a > 0 ? a : CTFontGetSize(font) * 0.6
  }

  /// A synthetic italic (the bundle carries upright faces): the glyphs slanted 12°.
  private static func slanted(_ font: CTFont) -> CTFont {
    var skew = CGAffineTransform(a: 1, b: 0, c: 0.21, d: 1, tx: 0, ty: 0)
    return CTFontCreateCopyWithAttributes(font, CTFontGetSize(font), &skew, nil)
  }

  override func didMoveToWindow() {
    super.didMoveToWindow()
    if window != nil, screenScale != pixelScale { setFontSize(points) }
    restartBlink()
  }

  /// Theme colors are CSS hex (`#rrggbb` or `#rrggbbaa`); the VT gets them opaque.
  func setTheme(background: String, foreground: String, cursor: String, selection: String, palette: [String]) {
    guard let bg = CssColor(background), let fg = CssColor(foreground), let cur = CssColor(cursor) else { return }
    self.background = CssColor(rgb: bg.rgb)
    self.foreground = fg
    cursorColor = cur
    selectionColor = CssColor(selection) ?? selectionColor
    vtPalette = palette256(palette.map { CssColor($0)?.rgb ?? 0x808080 })
    backgroundColor = self.background.color
    let r = CGFloat((bg.rgb >> 16) & 0xff), g = CGFloat((bg.rgb >> 8) & 0xff), b = CGFloat(bg.rgb & 0xff)
    keyboardAppearance = (0.299 * r + 0.587 * g + 0.114 * b) / 255 < 0.5 ? .dark : .light
    applyTheme()
    // The history's colors are the VT's: its rows are drawn again when next needed.
    scratch = nil
    history = Array(repeating: nil, count: historyAnsi.count)
    setNeedsDisplay()
  }

  private func applyTheme() {
    guard let vt, let vtPalette else { return }
    vt.setTheme(foreground: foreground.rgb, background: background.rgb, cursor: cursorColor.rgb, palette: vtPalette)
    needsFull = true
    schedulePull()
  }

  func write(_ data: String) {
    guard let vt else { return pending.append(data) }
    vt.write(data)
    if let copied = vt.takeClipboard() { onCopy(copied) }
    schedulePull()
    restartBlink()
  }

  /// After `onFailed`: try to make the VT again.
  func retry() {
    guard failed else { return }
    failed = false
    relayout()
    setNeedsDisplay()
  }

  // MARK: The history

  /// The `history` command: void (`reset`), drop `removed` rows from the front, append
  /// `lines`. The rows on screen stay put.
  func applyHistory(reset: Bool, removed: Int, rows lines: [String], cols: Int, alternate: Bool) {
    if reset {
      historyAnsi.removeAll()
      history.removeAll()
      markRow = -1
      clearSelection()
      scratch = nil
    } else if removed > 0 {
      let n = min(removed, history.count)
      historyAnsi.removeFirst(min(n, historyAnsi.count))
      history.removeFirst(n)
      markRow = markRow >= n ? markRow - n : -1
      shiftSelection(removed: n)
    }
    if cols > 0, cols != historyCols {
      historyCols = cols
      scratch = nil
      history = Array(repeating: nil, count: historyAnsi.count)
    }
    self.alternate = alternate
    // Kept as sent; drawn when first needed (historyLine).
    historyAnsi.append(contentsOf: lines)
    history.append(contentsOf: Array(repeating: nil, count: lines.count))
    // Scrolled up, the rows above the screen grew: the reader's rows stay where they are.
    if scrollY > 0, !reset { scrollY += CGFloat(lines.count) * cellH }
    if alternate { scrollY = 0 }
    scrollY = min(max(scrollY, 0), maxScroll)
    reportViewport()
    setNeedsDisplay()
  }

  /// Show history row `row` at the top.
  func scrollTo(row: Int) {
    guard !alternate, !history.isEmpty else { return }
    stopDecelerating()
    setScroll(CGFloat(history.count - min(max(row, 0), history.count)) * cellH)
  }

  func scrollToBottom() {
    stopDecelerating()
    setScroll(0)
  }

  /// Highlight one history row (the search's match); -1 none.
  func mark(row: Int) {
    markRow = row >= 0 && row < history.count ? row : -1
    setNeedsDisplay()
  }

  private var maxScroll: CGFloat { alternate ? 0 : CGFloat(history.count) * cellH }

  private func setScroll(_ next: CGFloat) {
    let clamped = min(max(next, 0), maxScroll)
    guard clamped != scrollY else { return }
    scrollY = clamped
    reportViewport()
    setNeedsDisplay()
  }

  private func reportViewport() {
    let up = cellH > 0 ? Int((scrollY / cellH - 0.01).rounded(.up)) : 0
    let top = history.count - max(up, 0)
    let atBottom = scrollY < 0.5
    guard top != reportedTop || atBottom != reportedBottom else { return }
    reportedTop = top
    reportedBottom = atBottom
    onViewport(top, atBottom)
  }

  /// History row `i` as a line, drawn through the scratch VT with the rows of its page when it is
  /// first needed (drawn, selected, tapped): history arrives in thousands of rows while output
  /// floods, and nobody looks at most of them. Rows stand alone (core/terminal/history.ts), so a page
  /// starts from a reset.
  private func historyLine(_ i: Int) -> Line {
    if let line = history[i] { return line }
    let start = i - i % Self.page
    let end = min(start + Self.page, historyAnsi.count)
    if scratch == nil, let made = Vt(cols: min(max(historyCols, 2), 1000), rows: Self.page) {
      if let vtPalette {
        made.setTheme(foreground: foreground.rgb, background: background.rgb, cursor: cursorColor.rgb, palette: vtPalette)
      }
      scratch = made
    }
    var rendered = [Line](repeating: .empty, count: Self.page)
    if let scratch {
      scratch.write("\u{1b}[0m\u{1b}[2J\u{1b}[H" + historyAnsi[start..<end].joined(separator: "\r\n"))
      if let frame = scratch.frame(full: true) {
        // A history row is a whole line (the computer joined wrapped rows).
        for (row, _, runs) in frame.dirty where row >= 0 && row < Self.page {
          rendered[row] = Line(cells: runs.map(Cell.init), wrapped: false)
        }
      }
    }
    for r in start..<end { history[r] = rendered[r - start] }
    return rendered[i - start]
  }

  // MARK: Content lines

  private var lineCount: Int { history.count + rows }

  private func line(_ i: Int) -> Line {
    // Output written since the last draw is read before anything looks at a row.
    if needsPull { pull() }
    if i < history.count { return i >= 0 ? historyLine(i) : .empty }
    let live = i - history.count
    return live >= 0 && live < grid.count ? grid[live] : .empty
  }

  /// The scroll offset on the pixel grid.
  private var snappedScroll: CGFloat { (scrollY * pixelScale).rounded() / pixelScale }

  /// The y of content line `i`.
  private func lineY(_ i: Int) -> CGFloat { CGFloat(i - history.count) * cellH + snappedScroll }

  /// The content line under y (may be past either end).
  private func lineUnder(_ y: CGFloat) -> Int { history.count + Int(((y - snappedScroll) / max(cellH, 1)).rounded(.down)) }

  private func colUnder(_ x: CGFloat) -> Int { min(max(Int(x / max(cellW, 1)), 0), max(cols - 1, 0)) }

  /// A line's text, blank cells as spaces, and the UTF-16 index of cell `col` in it: -1 past its
  /// end; a wide character or a grapheme maps to its start.
  private func rowText(_ line: Line, col: Int) -> (text: String, index: Int) {
    var units: [UInt16] = []
    var at = 0
    var index = -1
    for cell in line.cells {
      let run = cell.run
      if run.col > at {
        if col >= at, col < run.col { index = units.count + (col - at) }
        units.append(contentsOf: repeatElement(0x20, count: run.col - at))
      }
      if col >= run.col, col < run.col + run.cells { index = run.plain ? units.count + (col - run.col) : units.count }
      units.append(contentsOf: run.units)
      at = run.col + run.cells
    }
    return (String(decoding: units, as: UTF16.self), index)
  }

  /// The text of cells c0...c1 of a line (blanks as spaces; a wide character once, from its first cell).
  private func cellsText(_ line: Line, _ c0: Int, _ c1: Int) -> String {
    var units: [UInt16] = []
    var at = c0
    for cell in line.cells {
      let run = cell.run
      if run.col + run.cells <= c0 { continue }
      if run.col > c1 { break }
      while at < run.col, at <= c1 { units.append(0x20); at += 1 }
      if run.plain {
        let a = max(c0, run.col) - run.col, b = min(c1, run.col + run.cells - 1) - run.col
        if b >= a { units.append(contentsOf: run.units[a...b]) }
      } else if run.col >= c0 {
        units.append(contentsOf: run.units)
      }
      at = run.col + run.cells
    }
    return String(decoding: units, as: UTF16.self)
  }

  /// A new attachment: the screen starts empty. The history is the page's (`applyHistory`).
  func reset() {
    vt = nil
    pending.removeAll()
    cols = 0
    rows = 0
    grid = []
    clearSelection()
    relayout()
    setNeedsDisplay()
  }

  func showKeyboard() { _ = becomeFirstResponder() }

  func hideKeyboard() { _ = resignFirstResponder() }

  // MARK: Layout and the grid

  override func layoutSubviews() {
    super.layoutSubviews()
    relayout()
  }

  private func relayout() {
    let w = bounds.width, h = bounds.height
    guard w > 0, h > 0, cellW > 0, cellH > 0 else { return }
    let c = max(2, Int((w + 0.001) / cellW))
    let r = max(2, Int((h + 0.001) / cellH))
    if let vt {
      guard c != cols || r != rows else { return }
      vt.resize(cols: c, rows: r, cellWidth: cellW * pixelScale, cellHeight: cellH * pixelScale)
    } else {
      guard !failed else { return }
      guard let made = Vt(cols: c, rows: r) else {
        // Said once; `retry` tries again.
        failed = true
        onFailed("libghostty-vt could not make a \(c)×\(r) terminal.")
        return
      }
      vt = made
      applyTheme()
      for data in pending { made.write(data) }
      pending.removeAll()
      if let copied = made.takeClipboard() { onCopy(copied) }
    }
    cols = c
    rows = r
    grid = Array(repeating: .empty, count: r)
    needsFull = true
    pull()
    setNeedsDisplay()
    onSize(c, r)
    scrollY = min(max(scrollY, 0), maxScroll)
    reportViewport()
  }

  private func schedulePull() {
    needsPull = true
    setNeedsDisplay()
  }

  /// The rows that changed since the last pull; every row after a resize, a theme or a font.
  private func pull() {
    guard let vt, let frame = vt.frame(full: needsFull) else { return }
    needsPull = false
    needsFull = false
    if frame.rows != grid.count {
      grid = (0..<frame.rows).map { $0 < grid.count ? grid[$0] : .empty }
    }
    rows = frame.rows
    cols = frame.cols
    cursorCol = frame.cursorCol
    cursorRow = frame.cursorRow
    cursorVisible = frame.cursorVisible
    cursorStyle = frame.cursorStyle
    if frame.cursorBlinking != cursorBlinks {
      cursorBlinks = frame.cursorBlinking
      restartBlink()
    }
    for (row, wrapped, runs) in frame.dirty where row < grid.count {
      grid[row] = Line(cells: runs.map(Cell.init), wrapped: wrapped)
    }
  }

  // MARK: Drawing

  override func draw(_ rect: CGRect) {
    if needsPull { pull() }
    guard let ctx = UIGraphicsGetCurrentContext(), cellH > 0 else { return }
    ctx.setShouldAntialias(false)
    ctx.setFillColor(background.color.cgColor)
    ctx.fill(bounds)
    ctx.textMatrix = CGAffineTransform(scaleX: 1, y: -1)
    let first = max(0, lineUnder(max(rect.minY, 0)))
    let last = min(lineCount - 1, lineUnder(min(rect.maxY, bounds.height) - 0.001))
    guard first <= last else { return drawHandles(in: ctx) }
    for i in first...last {
      let y = lineY(i)
      let content = line(i)
      ctx.setShouldAntialias(false)
      for cell in content.cells where cell.run.bg != 0 {
        ctx.setFillColor(UIColor(argb: cell.run.bg).cgColor)
        ctx.fill(CGRect(x: CGFloat(cell.run.col) * cellW, y: y, width: CGFloat(cell.run.cells) * cellW, height: cellH))
      }
      if i < history.count, i == markRow {
        ctx.setFillColor(selectionColor.color.cgColor)
        ctx.fill(CGRect(x: 0, y: y, width: bounds.width, height: cellH))
      }
      if let from = selFrom, let to = selTo, i >= from.line, i <= to.line {
        let c0 = i == from.line ? from.col : 0, c1 = i == to.line ? to.col : cols - 1
        if c1 >= c0 {
          ctx.setFillColor(selectionColor.color.cgColor)
          ctx.fill(CGRect(x: CGFloat(c0) * cellW, y: y, width: CGFloat(c1 - c0 + 1) * cellW, height: cellH))
        }
      }
      for (k, cell) in content.cells.enumerated() {
        draw(cell, y: y, in: ctx, next: k + 1 < content.cells.count ? content.cells[k + 1].run : nil)
      }
    }
    drawCursor(in: ctx)
    drawHandles(in: ctx)
  }

  private func faceIndex(_ run: TerminalRun) -> Int { (run.bold ? 1 : 0) + (run.italic ? 2 : 0) }

  private func face(_ index: Int) -> CTFont { [ctFont, ctBold, ctItalic, ctBoldItalic][index] }

  /// What draws a character the run's face lacks: the upright face (slanted in an italic run) and its fallbacks.
  private func uprightFace(_ run: TerminalRun) -> CTFont {
    run.italic ? (run.bold ? ctBoldSlant : ctSlant) : (run.bold ? ctBold : ctFont)
  }

  private func isDrawn(_ cp: UInt32) -> Bool { mewla_glyph_is_drawn(cp) }

  /// Whether bundled face `index` draws this character itself (its advance is then exactly one cell).
  private func hasGlyph(_ unit: UInt16, face index: Int) -> Bool {
    if let known = inFont[index][unit] { return known }
    var u = unit
    var glyph = CGGlyph(0)
    let found = CTFontGetGlyphsForCharacters(face(index), &u, &glyph, 1) && glyph != 0
    inFont[index][unit] = found
    return found
  }

  private func ctLine(_ units: ArraySlice<UInt16>, font: CTFont, color: CGColor) -> CTLine {
    let attributes: [NSAttributedString.Key: Any] = [
      NSAttributedString.Key(kCTFontAttributeName as String): font,
      NSAttributedString.Key(kCTForegroundColorAttributeName as String): color,
      NSAttributedString.Key(kCTLigatureAttributeName as String): 0,
    ]
    return CTLineCreateWithAttributedString(NSAttributedString(string: String(decoding: units, as: UTF16.self), attributes: attributes))
  }

  /// What a run draws in `color`: in-face text in one line per stretch, each other glyph on its own.
  /// `next` is the run after it in its row (a flag's second regional indicator).
  private func items(for run: TerminalRun, color: UInt32, next: TerminalRun?) -> [Item] {
    let index = faceIndex(run)
    let primary = face(index)
    let upright = uprightFace(run)
    let cg = UIColor(argb: color).cgColor
    var out: [Item] = []
    let x0 = CGFloat(run.col) * cellW
    let fitted = { (units: ArraySlice<UInt16>, x: CGFloat, cells: Int) -> Item in
      let line = self.ctLine(units, font: upright, color: cg)
      return .fitted(line, x: x, width: CGFloat(CTLineGetTypographicBounds(line, nil, nil, nil)), box: CGFloat(cells) * self.cellW)
    }
    if run.flags & VtLayout.flagTail != 0 {
      // A flag's second regional indicator: the first draws the flag across both.
      return []
    }
    if run.plain {
      // Stretches drawn in one face: 0 the run's own, 1 the upright face (an italic run's other characters).
      var seg = -1
      var segFace = 0
      for k in 0...run.units.count {
        let u: UInt16 = k < run.units.count ? run.units[k] : 0x20
        let drawn = k < run.units.count && isDrawn(UInt32(u))
        var one = -1
        if u != 0x20, !drawn {
          if hasGlyph(u, face: index) { one = 0 } else if run.italic, hasGlyph(u, face: index - 2) { one = 1 }
        }
        if seg >= 0, one != segFace {
          out.append(.text(ctLine(run.units[seg..<k], font: segFace == 0 ? primary : upright, color: cg), x: x0 + CGFloat(seg) * cellW))
          seg = -1
        }
        guard k < run.units.count, u != 0x20 else { continue }
        if drawn {
          out.append(.glyph(UInt32(u), x: x0 + CGFloat(k) * cellW))
        } else if one < 0 {
          out.append(fitted(run.units[k..<(k + 1)], x0 + CGFloat(k) * cellW, 1))
        } else if seg < 0 {
          seg = k
          segFace = one
        }
      }
    } else if !run.units.isEmpty {
      let scalars = Array(String(decoding: run.units, as: UTF16.self).unicodeScalars)
      if scalars.count == 1, isDrawn(scalars[0].value) {
        out.append(.glyph(scalars[0].value, x: x0))
      } else if run.flags & VtLayout.flagHead != 0, let next {
        // Two regional indicators: the one flag, centred across both runs' cells.
        out.append(fitted((run.units + next.units)[...], x0, run.cells + next.cells))
      } else if run.flags & VtLayout.emojiWide != 0 {
        // An emoji the VT gives one cell, with a blank cell after it: full size across both.
        out.append(fitted(run.units[...], x0, 2))
      } else {
        out.append(fitted(run.units[...], x0, run.cells))
      }
    }
    return out
  }

  private func draw(_ cell: Cell, y: CGFloat, in ctx: CGContext, color: UInt32? = nil, next: TerminalRun? = nil) {
    let run = cell.run
    let items: [Item]
    if let color {
      items = self.items(for: run, color: color, next: next)
    } else if let made = cell.items {
      items = made
    } else {
      items = self.items(for: run, color: run.fg, next: next)
      cell.items = items
    }
    let ink = color ?? run.fg
    for item in items {
      switch item {
      case let .text(line, x):
        ctx.setShouldAntialias(true)
        ctx.textPosition = CGPoint(x: x, y: y + baseline)
        CTLineDraw(line, ctx)
      case let .fitted(line, x, width, box):
        // Not one cell wide (CJK, emoji, a symbol from a fallback face): centred in its cells, scaled down to fit.
        ctx.setShouldAntialias(true)
        if width <= box + 0.5 || width <= 0 {
          ctx.textPosition = CGPoint(x: x + (box - width) / 2, y: y + baseline)
          CTLineDraw(line, ctx)
        } else {
          let s = box / width
          ctx.saveGState()
          ctx.translateBy(x: x + box / 2, y: y + cellH / 2)
          ctx.scaleBy(x: s, y: s)
          ctx.textPosition = CGPoint(x: -width / 2, y: baseline - cellH / 2)
          CTLineDraw(line, ctx)
          ctx.restoreGState()
        }
      case let .glyph(cp, x):
        drawGlyph(cp, x: x, y: y, color: ink, in: ctx)
      }
    }
    decorate(run, y: y, color: ink, in: ctx)
  }

  /// A box-drawing, block, braille or Powerline glyph from TerminalGlyphs, exactly in its cell (in pixels).
  private func drawGlyph(_ cp: UInt32, x: CGFloat, y: CGFloat, color: UInt32, in ctx: CGContext) {
    let ops: [MewlaGlyphOp]
    if let cached = glyphOps[cp] {
      ops = cached
    } else {
      var buffer = [MewlaGlyphOp](repeating: MewlaGlyphOp(), count: 64)
      let n = Int(mewla_glyph_ops(cp, Int32((cellW * pixelScale).rounded()), Int32((cellH * pixelScale).rounded()), &buffer, 64))
      ops = Array(buffer.prefix(max(n, 0)))
      glyphOps[cp] = ops
    }
    ctx.saveGState()
    ctx.translateBy(x: x, y: y)
    ctx.scaleBy(x: 1 / pixelScale, y: 1 / pixelScale)
    for op in ops {
      let p = withUnsafeBytes(of: op.p) { Array($0.bindMemory(to: Float.self)).map { CGFloat($0) } }
      let ink = UIColor(argb: color, alpha: CGFloat(op.alpha)).cgColor
      ctx.setFillColor(ink)
      ctx.setStrokeColor(ink)
      ctx.setLineCap(.butt)
      ctx.setLineWidth(CGFloat(op.stroke))
      switch op.kind {
      case 1:
        ctx.setShouldAntialias(false)
        ctx.fill(CGRect(x: p[0], y: p[1], width: p[2] - p[0], height: p[3] - p[1]))
      case 2:
        ctx.setShouldAntialias(true)
        ctx.beginPath()
        ctx.move(to: CGPoint(x: p[0], y: p[1]))
        for k in 1..<max(Int(op.points), 1) { ctx.addLine(to: CGPoint(x: p[2 * k], y: p[2 * k + 1])) }
        ctx.closePath()
        ctx.fillPath()
      case 3:
        ctx.setShouldAntialias(true)
        ctx.beginPath()
        ctx.move(to: CGPoint(x: p[0], y: p[1]))
        ctx.addLine(to: CGPoint(x: p[2], y: p[3]))
        ctx.strokePath()
      case 4, 5:
        ctx.setShouldAntialias(true)
        ctx.beginPath()
        ctx.move(to: CGPoint(x: p[0], y: p[1]))
        ctx.addCurve(to: CGPoint(x: p[6], y: p[7]), control1: CGPoint(x: p[2], y: p[3]), control2: CGPoint(x: p[4], y: p[5]))
        if op.kind == 5 {
          ctx.closePath()
          ctx.fillPath()
        } else {
          ctx.strokePath()
        }
      case 6:
        ctx.setShouldAntialias(true)
        ctx.fillEllipse(in: CGRect(x: p[0], y: p[1], width: p[2] - p[0], height: p[3] - p[1]))
      default:
        break
      }
    }
    ctx.restoreGState()
  }

  /// Underline (its style and color), strikethrough and overline across the run's cells, one line weight.
  private func decorate(_ run: TerminalRun, y: CGFloat, color: UInt32, in ctx: CGContext) {
    let x0 = CGFloat(run.col) * cellW, x1 = x0 + CGFloat(run.cells) * cellW
    let t = thickness
    let snap = { (v: CGFloat) in (v * self.pixelScale).rounded() / self.pixelScale }
    if run.underline != 0 {
      let ink = UIColor(argb: run.underlineColor != 0 ? run.underlineColor : color).cgColor
      ctx.setFillColor(ink)
      ctx.setStrokeColor(ink)
      let under = snap(min(y + baseline + max(t, points * 0.12), y + cellH - 3 * t))
      switch run.underline {
      case 2:
        ctx.setShouldAntialias(false)
        ctx.fill(CGRect(x: x0, y: under, width: x1 - x0, height: t))
        ctx.fill(CGRect(x: x0, y: under + 2 * t, width: x1 - x0, height: t))
      case 3:
        ctx.setShouldAntialias(true)
        ctx.setLineWidth(t)
        ctx.beginPath()
        ctx.move(to: CGPoint(x: x0, y: under + t))
        var x = x0
        var up = true
        let half = cellW / 2
        while x < x1 {
          ctx.addQuadCurve(to: CGPoint(x: min(x + half, x1), y: under + t), control: CGPoint(x: x + half / 2, y: up ? under - t : under + 3 * t))
          x += half
          up.toggle()
        }
        ctx.strokePath()
      case 4:
        ctx.setShouldAntialias(false)
        var x = x0
        while x < x1 {
          ctx.fill(CGRect(x: x, y: under, width: min(t, x1 - x), height: t))
          x += 2 * t
        }
      case 5:
        ctx.setShouldAntialias(false)
        let dash = max(2 * t, snap(cellW * 0.4))
        var x = x0
        while x < x1 {
          ctx.fill(CGRect(x: x, y: under, width: min(dash, x1 - x), height: t))
          x += 2 * dash
        }
      default:
        ctx.setShouldAntialias(false)
        ctx.fill(CGRect(x: x0, y: under, width: x1 - x0, height: t))
      }
    }
    ctx.setShouldAntialias(false)
    ctx.setFillColor(UIColor(argb: color).cgColor)
    if run.strike { ctx.fill(CGRect(x: x0, y: snap(y + baseline - points * 0.3), width: x1 - x0, height: t)) }
    if run.overline { ctx.fill(CGRect(x: x0, y: y, width: x1 - x0, height: t)) }
  }

  private func drawCursor(in ctx: CGContext) {
    guard cursorVisible, vt != nil, cursorRow < rows else { return }
    let y = lineY(history.count + cursorRow)
    guard y < bounds.height, y + cellH > 0 else { return }
    let x = CGFloat(cursorCol) * cellW
    if !marked.isEmpty {
      // The composition (IME) at the cursor, underlined; only the committed text is sent.
      let line = ctLine(Array(marked.utf16)[...], font: ctFont, color: foreground.color.cgColor)
      let w = CGFloat(CTLineGetTypographicBounds(line, nil, nil, nil))
      ctx.setShouldAntialias(false)
      ctx.setFillColor(background.color.cgColor)
      ctx.fill(CGRect(x: x, y: y, width: w, height: cellH))
      ctx.setShouldAntialias(true)
      ctx.textPosition = CGPoint(x: x, y: y + baseline)
      CTLineDraw(line, ctx)
      ctx.setShouldAntialias(false)
      ctx.setFillColor(foreground.color.cgColor)
      ctx.fill(CGRect(x: x, y: y + cellH - thickness, width: w, height: thickness))
      return
    }
    if cursorBlinks, isFirstResponder, !blinkOn { return }
    // The cell under the cursor (a wide character takes its two cells).
    let under = cursorRow < grid.count ? grid[cursorRow].cells.first(where: { cursorCol >= $0.run.col && cursorCol < $0.run.col + $0.run.cells }) : nil
    let wide = under.map { !$0.run.plain && $0.run.cells == 2 } ?? false
    let cx = wide ? CGFloat(under!.run.col) * cellW : x
    let w = wide ? 2 * cellW : cellW
    let t = thickness
    ctx.setShouldAntialias(false)
    ctx.setFillColor(cursorColor.color.cgColor)
    if !isFirstResponder || cursorStyle == 3 {
      // Unfocused (no keyboard): a hollow block, the text stays readable.
      ctx.fill(CGRect(x: cx, y: y, width: w, height: t))
      ctx.fill(CGRect(x: cx, y: y + cellH - t, width: w, height: t))
      ctx.fill(CGRect(x: cx, y: y, width: t, height: cellH))
      ctx.fill(CGRect(x: cx + w - t, y: y, width: t, height: cellH))
    } else if cursorStyle == 0 {
      ctx.fill(CGRect(x: cx, y: y, width: max(t, (cellW / 8 * pixelScale).rounded() / pixelScale), height: cellH))
    } else if cursorStyle == 2 {
      ctx.fill(CGRect(x: cx, y: y + cellH - 2 * t, width: w, height: 2 * t))
    } else {
      ctx.fill(CGRect(x: cx, y: y, width: w, height: cellH))
      // The character under a block cursor, in the background color.
      if let under {
        ctx.saveGState()
        ctx.clip(to: CGRect(x: cx, y: y, width: w, height: cellH))
        let cells = grid[cursorRow].cells
        let next = cells.firstIndex { $0 === under }.flatMap { $0 + 1 < cells.count ? cells[$0 + 1].run : nil }
        draw(under, y: y, in: ctx, color: background.rgb | 0xff000000, next: next)
        ctx.restoreGState()
      }
    }
  }

  private func restartBlink() {
    blinkTimer?.invalidate()
    blinkTimer = nil
    blinkOn = true
    guard cursorBlinks, window != nil else { return }
    blinkTimer = Timer.scheduledTimer(withTimeInterval: 0.6, repeats: true) { [weak self] _ in
      guard let self else { return }
      self.blinkOn.toggle()
      self.setNeedsDisplay()
    }
  }

  // MARK: Selection

  private var hasSelection: Bool { selFrom != nil && selTo != nil }

  private func clearSelection() {
    selFrom = nil
    selTo = nil
    dragging = 0
    editMenu.dismissMenu()
    setNeedsDisplay()
  }

  /// History rows removed from the front pull every content line up. (Rows appended to the history
  /// are the live rows that scrolled off: the selected text keeps its content line.)
  private func shiftSelection(removed: Int) {
    guard var from = selFrom, var to = selTo else { return }
    from.line -= removed
    to.line -= removed
    if to.line < 0 { return clearSelection() }
    if from.line < 0 { from = (0, 0) }
    selFrom = from
    selTo = to
  }

  /// The word under (line, col): the run of non-blank cells around it; nil on a blank.
  private func word(at i: Int, col: Int) -> (Int, Int)? {
    guard col >= 0, col < cols else { return nil }
    var blank = [Bool](repeating: true, count: cols)
    for cell in line(i).cells {
      let run = cell.run
      for k in 0..<run.cells where run.col + k < cols {
        blank[run.col + k] = run.plain && run.units[k] == 0x20
      }
    }
    guard !blank[col] else { return nil }
    var a = col, b = col
    while a > 0, !blank[a - 1] { a -= 1 }
    while b < cols - 1, !blank[b + 1] { b += 1 }
    return (a, b)
  }

  /// The selected text: rows joined by newlines, except where a row wraps onto the next.
  func selectedText() -> String {
    guard let from = selFrom, let to = selTo else { return "" }
    var out = ""
    for i in from.line...to.line {
      let content = line(i)
      var part = cellsText(content, i == from.line ? from.col : 0, i == to.line ? to.col : cols - 1)
      if content.wrapped, i != to.line {
        out += part
      } else {
        while part.last == " " { part.removeLast() }
        out += part
        if i != to.line { out += "\n" }
      }
    }
    // Empty rows at the end (Select All over a screen not yet full) are not text.
    while out.hasSuffix("\n") { out.removeLast() }
    return out
  }

  /// Where a handle sits: the start's at the top of its first cell, the end's at the bottom of its last.
  private func handlePoint(_ which: Int) -> CGPoint? {
    guard let from = selFrom, let to = selTo else { return nil }
    return which == 1 ? CGPoint(x: CGFloat(from.col) * cellW, y: lineY(from.line))
      : CGPoint(x: CGFloat(to.col + 1) * cellW, y: lineY(to.line) + cellH)
  }

  /// iOS's selection handles: a bar the height of the row with a knob above (start) or below (end).
  private func drawHandles(in ctx: CGContext) {
    guard hasSelection else { return }
    let ink = tintColor.cgColor
    ctx.setShouldAntialias(true)
    ctx.setFillColor(ink)
    for which in 1...2 {
      guard let p = handlePoint(which) else { continue }
      let top = which == 1 ? p.y : p.y - cellH
      ctx.fill(CGRect(x: p.x - 1, y: top, width: 2, height: cellH))
      let knob: CGFloat = 5
      let cy = which == 1 ? top - knob : p.y + knob
      ctx.fillEllipse(in: CGRect(x: p.x - knob, y: cy - knob, width: 2 * knob, height: 2 * knob))
    }
  }

  private func handle(at point: CGPoint) -> Int {
    for which in [2, 1] {
      guard let p = handlePoint(which) else { continue }
      let centre = CGPoint(x: p.x, y: which == 1 ? p.y - cellH / 2 : p.y + cellH / 2)
      if abs(point.x - centre.x) <= 22, abs(point.y - centre.y) <= max(22, cellH) { return which }
    }
    return 0
  }

  /// A handle follows the finger; near an edge the content scrolls.
  private func moveHandle(to point: CGPoint) {
    guard var from = selFrom, var to = selTo else { return }
    if point.y < cellH { setScroll(scrollY + cellH / 2) } else if point.y > bounds.height - cellH { setScroll(scrollY - cellH / 2) }
    let i = min(max(lineUnder(point.y), 0), max(lineCount - 1, 0))
    let col = colUnder(point.x + (dragging == 2 ? -cellW / 2 : cellW / 2))
    if dragging == 1 { from = (i, col) } else { to = (i, col) }
    // Dragged past each other: swap ends (and handles).
    if from.line > to.line || (from.line == to.line && from.col > to.col) {
      swap(&from, &to)
      dragging = 3 - dragging
    }
    selFrom = from
    selTo = to
    setNeedsDisplay()
  }

  /// The edit menu (Copy, Paste, Select All) at `point`, as after a long press.
  func showSelectionMenu(at point: CGPoint) { showMenu(at: point) }

  private func showMenu(at point: CGPoint) {
    menuPoint = point
    editMenu.presentEditMenu(with: UIEditMenuConfiguration(identifier: nil, sourcePoint: point))
  }

  /// Pasted text to the program, framed for bracketed paste when it asked for it.
  func paste(_ text: String) {
    guard let vt, let bytes = vt.paste(text) else { return }
    onInput(bytes)
    scrollToBottom()
  }

  func editMenuInteraction(_ interaction: UIEditMenuInteraction, menuFor configuration: UIEditMenuConfiguration,
                           suggestedActions: [UIMenuElement]) -> UIMenu? {
    var actions: [UIMenuElement] = []
    if hasSelection {
      actions.append(UIAction(title: "Copy", image: UIImage(systemName: "doc.on.doc")) { [weak self] _ in
        guard let self else { return }
        UIPasteboard.general.string = self.selectedText()
        self.clearSelection()
      })
    }
    if UIPasteboard.general.hasStrings {
      actions.append(UIAction(title: "Paste", image: UIImage(systemName: "doc.on.clipboard")) { [weak self] _ in
        if let text = UIPasteboard.general.string { self?.paste(text) }
        self?.clearSelection()
      })
    }
    actions.append(UIAction(title: "Select All", image: UIImage(systemName: "selection.pin.in.out")) { [weak self] _ in
      guard let self else { return }
      self.selectAll()
      DispatchQueue.main.async { self.showMenu(at: self.menuPoint) }
    })
    return UIMenu(children: actions)
  }

  func editMenuInteraction(_ interaction: UIEditMenuInteraction, targetRectFor configuration: UIEditMenuConfiguration) -> CGRect {
    guard let from = selFrom, let to = selTo else { return CGRect(origin: menuPoint, size: .zero) }
    let top = min(max(lineY(from.line), 0), bounds.height), bottom = min(max(lineY(to.line) + cellH, 0), bounds.height)
    return CGRect(x: 0, y: top, width: bounds.width, height: max(bottom - top, 1))
  }

  // MARK: Touch: a tap names the cell and its text; a drag scrolls (with deceleration); a long press selects.

  @objc private func tapped(_ gesture: UITapGestureRecognizer) {
    if hasSelection { return clearSelection() }
    guard rows > 0, cellH > 0 else { return }
    let point = gesture.location(in: self)
    let col = colUnder(point.x)
    let i = min(max(lineUnder(point.y), 0), max(lineCount - 1, 0))
    let (text, index) = rowText(line(i), col: col)
    onTapCell(col, i < history.count ? -1 : i - history.count, text, index)
  }

  @objc private func panned(_ gesture: UIPanGestureRecognizer) {
    let point = gesture.location(in: self)
    let scrolls = !alternate && (!history.isEmpty || scrollY > 0)
    switch gesture.state {
    case .began:
      stopDecelerating()
      dragRemainder = 0
      dragging = handle(at: point)
      if dragging != 0 { editMenu.dismissMenu() }
    case .changed:
      if dragging != 0 { return moveHandle(to: point) }
      let dy = gesture.translation(in: self).y
      gesture.setTranslation(.zero, in: self)
      if scrolls {
        // A drag down moves back in time.
        setScroll(scrollY + dy)
      } else {
        dragRemainder += dy / max(cellH, 1)
        let lines = Int(dragRemainder)
        if lines != 0 {
          dragRemainder -= CGFloat(lines)
          onScroll(lines)
        }
      }
    case .ended, .cancelled:
      if dragging != 0 {
        dragging = 0
        showMenu(at: point)
      } else if scrolls, gesture.state == .ended {
        startDecelerating(gesture.velocity(in: self).y)
      }
    default:
      break
    }
  }

  private func startDecelerating(_ v: CGFloat) {
    guard abs(v) > 50 else { return }
    velocity = v
    let link = CADisplayLink(target: self, selector: #selector(decelerate(_:)))
    link.add(to: .main, forMode: .common)
    decelerator = link
  }

  private func stopDecelerating() {
    decelerator?.invalidate()
    decelerator = nil
    velocity = 0
  }

  /// UIScrollView's normal deceleration (0.998 per millisecond).
  @objc private func decelerate(_ link: CADisplayLink) {
    let dt = max(link.targetTimestamp - link.timestamp, 1.0 / 120)
    let before = scrollY
    setScroll(scrollY + velocity * CGFloat(dt))
    velocity *= CGFloat(pow(0.998, dt * 1000))
    if abs(velocity) < 10 || scrollY == before { stopDecelerating() }
  }

  @objc private func pressed(_ gesture: UILongPressGestureRecognizer) {
    guard gesture.state == .began, cellH > 0 else { return }
    let point = gesture.location(in: self)
    selectWord(at: point)
    UIImpactFeedbackGenerator(style: .medium).impactOccurred()
    showMenu(at: point)
  }

  /// A long press: select the word under `point` (nothing on a blank). Whether a word was selected.
  @discardableResult
  func selectWord(at point: CGPoint) -> Bool {
    let i = min(max(lineUnder(point.y), 0), max(lineCount - 1, 0))
    if let span = word(at: i, col: colUnder(point.x)) {
      selFrom = (i, span.0)
      selTo = (i, span.1)
    } else {
      selFrom = nil
      selTo = nil
    }
    setNeedsDisplay()
    return selFrom != nil
  }

  /// Select All: every row, the history's included.
  func selectAll() {
    selFrom = (0, 0)
    selTo = (max(lineCount - 1, 0), max(cols - 1, 0))
    setNeedsDisplay()
  }

  /// The text of screen row `row` (a history row while scrolled up), trailing blanks dropped.
  func lineText(_ row: Int) -> String {
    guard row >= 0, row < rows else { return "" }
    var text = rowText(line(lineUnder(CGFloat(row) * cellH + cellH / 2)), col: 0).text
    while text.last == " " { text.removeLast() }
    return text
  }

  // MARK: The keyboard

  override var canBecomeFirstResponder: Bool { true }

  override func resignFirstResponder() -> Bool {
    if !marked.isEmpty {
      inputDelegate?.textWillChange(self)
      marked = ""
      inputDelegate?.textDidChange(self)
      setNeedsDisplay()
    }
    return super.resignFirstResponder()
  }

  var autocorrectionType: UITextAutocorrectionType = .no
  var autocapitalizationType: UITextAutocapitalizationType = .none
  var spellCheckingType: UITextSpellCheckingType = .no
  var smartQuotesType: UITextSmartQuotesType = .no
  var smartDashesType: UITextSmartDashesType = .no
  var smartInsertDeleteType: UITextSmartInsertDeleteType = .no
  var keyboardType: UIKeyboardType = .default
  var keyboardAppearance: UIKeyboardAppearance = .dark
  var returnKeyType: UIReturnKeyType = .default

  @available(iOS 17.0, *)
  var inlinePredictionType: UITextInlinePredictionType {
    get { .no }
    set {}
  }

  private func send(_ text: String) {
    guard !text.isEmpty else { return }
    onInput(text.replacingOccurrences(of: "\n", with: "\r"))
  }

  /// Always true, so the keyboard's delete reaches deleteBackward on an empty line.
  var hasText: Bool { true }

  func insertText(_ text: String) {
    if !marked.isEmpty {
      marked = ""
      markedSelection = NSRange(location: 0, length: 0)
      setNeedsDisplay()
    }
    send(text)
  }

  func deleteBackward() {
    if marked.isEmpty { return onInput("\u{7f}") }
    marked = String(marked.dropLast())
    markedSelection = NSRange(location: marked.utf16.count, length: 0)
    setNeedsDisplay()
  }

  // Hardware keys the text system does not turn into text: arrows, Escape, Ctrl+letter.
  private var handledPresses = Set<UIPress>()

  override func pressesBegan(_ presses: Set<UIPress>, with event: UIPressesEvent?) {
    var others = Set<UIPress>()
    for press in presses {
      if marked.isEmpty, let key = press.key, let data = Self.keyData(key) {
        handledPresses.insert(press)
        onInput(data)
      } else {
        others.insert(press)
      }
    }
    if !others.isEmpty { super.pressesBegan(others, with: event) }
  }

  override func pressesEnded(_ presses: Set<UIPress>, with event: UIPressesEvent?) {
    let others = presses.subtracting(handledPresses)
    handledPresses.subtract(presses)
    if !others.isEmpty { super.pressesEnded(others, with: event) }
  }

  override func pressesCancelled(_ presses: Set<UIPress>, with event: UIPressesEvent?) {
    let others = presses.subtracting(handledPresses)
    handledPresses.subtract(presses)
    if !others.isEmpty { super.pressesCancelled(others, with: event) }
  }

  static func keyData(_ key: UIKey) -> String? {
    keyData(code: key.keyCode, control: key.modifierFlags.contains(.control), characters: key.charactersIgnoringModifiers)
  }

  static func keyData(code: UIKeyboardHIDUsage, control: Bool, characters: String) -> String? {
    switch code {
    case .keyboardUpArrow: return "\u{1b}[A"
    case .keyboardDownArrow: return "\u{1b}[B"
    case .keyboardRightArrow: return "\u{1b}[C"
    case .keyboardLeftArrow: return "\u{1b}[D"
    case .keyboardEscape: return "\u{1b}"
    case .keyboardDeleteForward: return "\u{1b}[3~"
    case .keyboardHome: return "\u{1b}[H"
    case .keyboardEnd: return "\u{1b}[F"
    case .keyboardPageUp: return "\u{1b}[5~"
    case .keyboardPageDown: return "\u{1b}[6~"
    default: break
    }
    guard control, characters.unicodeScalars.count == 1, let scalar = characters.lowercased().unicodeScalars.first else { return nil }
    switch scalar.value {
    case 0x61...0x7a: return String(Character(Unicode.Scalar(UInt8(scalar.value - 0x60))))
    case 0x5b...0x5f: return String(Character(Unicode.Scalar(UInt8(scalar.value - 0x40)))) // [ \ ] ^ _
    case 0x20: return "\u{0}"
    default: return nil
    }
  }

  // MARK: UITextInput: the document is the composition alone; everything committed is sent.

  final class TextPosition: UITextPosition {
    let offset: Int
    init(_ offset: Int) { self.offset = offset }
  }

  final class TextRange: UITextRange {
    let from: TextPosition
    let to: TextPosition
    init(_ from: Int, _ to: Int) {
      self.from = TextPosition(min(from, to))
      self.to = TextPosition(max(from, to))
    }
    override var start: UITextPosition { from }
    override var end: UITextPosition { to }
    override var isEmpty: Bool { from.offset >= to.offset }
  }

  weak var inputDelegate: UITextInputDelegate?
  var markedTextStyle: [NSAttributedString.Key: Any]?
  private var stringTokenizer: UITextInputStringTokenizer?
  var tokenizer: UITextInputTokenizer {
    if let stringTokenizer { return stringTokenizer }
    let made = UITextInputStringTokenizer(textInput: self)
    stringTokenizer = made
    return made
  }

  private var length: Int { marked.utf16.count }
  private func clamp(_ offset: Int) -> Int { min(max(offset, 0), length) }
  private func offset(_ position: UITextPosition) -> Int { clamp((position as? TextPosition)?.offset ?? 0) }

  var markedTextRange: UITextRange? { marked.isEmpty ? nil : TextRange(0, length) }

  var selectedTextRange: UITextRange? {
    get {
      let start = clamp(markedSelection.location)
      return TextRange(start, clamp(start + markedSelection.length))
    }
    set {
      guard let range = newValue else { return }
      let start = offset(range.start)
      markedSelection = NSRange(location: start, length: offset(range.end) - start)
    }
  }

  var beginningOfDocument: UITextPosition { TextPosition(0) }
  var endOfDocument: UITextPosition { TextPosition(length) }

  func setMarkedText(_ markedText: String?, selectedRange: NSRange) {
    marked = markedText ?? ""
    markedSelection = selectedRange
    setNeedsDisplay()
  }

  /// The composition ends as typed: it is committed.
  func unmarkText() {
    guard !marked.isEmpty else { return }
    let text = marked
    marked = ""
    markedSelection = NSRange(location: 0, length: 0)
    setNeedsDisplay()
    send(text)
  }

  func text(in range: UITextRange) -> String? {
    let units = Array(marked.utf16)
    let start = offset(range.start), end = offset(range.end)
    guard start < end else { return "" }
    return String(decoding: units[start..<end], as: UTF16.self)
  }

  func replace(_ range: UITextRange, withText text: String) { insertText(text) }

  func textRange(from fromPosition: UITextPosition, to toPosition: UITextPosition) -> UITextRange? {
    TextRange(offset(fromPosition), offset(toPosition))
  }

  func position(from position: UITextPosition, offset: Int) -> UITextPosition? {
    let target = self.offset(position) + offset
    return (0...length).contains(target) ? TextPosition(target) : nil
  }

  func position(from position: UITextPosition, in direction: UITextLayoutDirection, offset: Int) -> UITextPosition? {
    switch direction {
    case .left, .up: return self.position(from: position, offset: -offset)
    default: return self.position(from: position, offset: offset)
    }
  }

  func compare(_ position: UITextPosition, to other: UITextPosition) -> ComparisonResult {
    let a = offset(position), b = offset(other)
    return a < b ? .orderedAscending : a > b ? .orderedDescending : .orderedSame
  }

  func offset(from: UITextPosition, to toPosition: UITextPosition) -> Int { offset(toPosition) - offset(from) }

  func position(within range: UITextRange, farthestIn direction: UITextLayoutDirection) -> UITextPosition? {
    switch direction {
    case .left, .up: return range.start
    default: return range.end
    }
  }

  func characterRange(byExtending position: UITextPosition, in direction: UITextLayoutDirection) -> UITextRange? {
    switch direction {
    case .left, .up: return TextRange(0, offset(position))
    default: return TextRange(offset(position), length)
    }
  }

  func baseWritingDirection(for position: UITextPosition, in direction: UITextStorageDirection) -> NSWritingDirection { .leftToRight }

  func setBaseWritingDirection(_ writingDirection: NSWritingDirection, for range: UITextRange) {}

  /// The cursor's cell, where the system puts the candidates.
  private var cursorRect: CGRect {
    CGRect(x: CGFloat(cursorCol) * cellW, y: lineY(history.count + cursorRow), width: cellW, height: cellH)
  }

  private func markedWidth(upTo offset: Int) -> CGFloat {
    let prefix = String(decoding: Array(marked.utf16).prefix(offset), as: UTF16.self)
    return (prefix as NSString).size(withAttributes: [.font: font]).width
  }

  func firstRect(for range: UITextRange) -> CGRect {
    var rect = cursorRect
    let start = markedWidth(upTo: offset(range.start))
    rect.origin.x += start
    rect.size.width = max(cellW, markedWidth(upTo: offset(range.end)) - start)
    return rect
  }

  func caretRect(for position: UITextPosition) -> CGRect {
    var rect = cursorRect
    rect.origin.x += markedWidth(upTo: offset(position))
    rect.size.width = 2
    return rect
  }

  func selectionRects(for range: UITextRange) -> [UITextSelectionRect] { [] }

  func closestPosition(to point: CGPoint) -> UITextPosition? { TextPosition(length) }

  func closestPosition(to point: CGPoint, within range: UITextRange) -> UITextPosition? { range.end }

  func characterRange(at point: CGPoint) -> UITextRange? { nil }
}
