import CoreText
import UIKit

/// A terminal drawn with CoreText from libghostty-vt's cells (the iOS twin of
/// client/android/.../terminal/TerminalView.kt). The system keyboard types into it
/// through UITextInput (composition drawn at the cursor, only committed text
/// reported); a drag scrolls the history (reported: the computer's tmux copy mode
/// moves); a tap focuses and reports the cell; a long press selects a line and
/// offers Copy.
final class TerminalView: UIView, UITextInput, UIEditMenuInteractionDelegate {
  var onSize: (_ cols: Int, _ rows: Int) -> Void = { _, _ in }
  var onInput: (_ data: String) -> Void = { _ in }
  var onScroll: (_ lines: Int) -> Void = { _ in }
  var onTapCell: (_ col: Int, _ row: Int) -> Void = { _, _ in }

  /// A run with its CoreText line, made on first draw.
  private final class Cell {
    let run: TerminalRun
    var line: CTLine?
    var width: CGFloat = 0
    init(_ run: TerminalRun) { self.run = run }
  }

  private var vt: Vt?
  /// Output that arrived before the first layout made the VT.
  private var pending: [String] = []
  private(set) var cols = 0
  private(set) var rows = 0
  private var grid: [[Cell]] = []
  private(set) var cursorCol = 0
  private(set) var cursorRow = 0
  private var cursorVisible = true
  private var needsPull = false
  private var needsFull = false

  private var font = UIFont.monospacedSystemFont(ofSize: 13, weight: .regular)
  private var boldFont = UIFont.monospacedSystemFont(ofSize: 13, weight: .bold)
  private var italicFont = UIFont.monospacedSystemFont(ofSize: 13, weight: .regular)
  private var boldItalicFont = UIFont.monospacedSystemFont(ofSize: 13, weight: .bold)
  private(set) var cellW: CGFloat = 0
  private(set) var cellH: CGFloat = 0
  private var baseline: CGFloat = 0

  private var background = CssColor(rgb: 0x141210)
  private var foreground = CssColor(rgb: 0xF4F0EA)
  private var cursorColor = CssColor(rgb: 0xF4F0EA)
  private var selectionColor = CssColor(rgb: 0x3D7EFF, alpha: 0.33)
  private var vtPalette: [UInt32]?

  private var marked = ""
  private var markedSelection = NSRange(location: 0, length: 0)
  private var selRow = -1
  private var scrollRemainder: CGFloat = 0
  private lazy var editMenu = UIEditMenuInteraction(delegate: self)

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
    applyFont(13) // the grid comes with the first layout, once the callbacks are set
  }

  @available(*, unavailable)
  required init?(coder: NSCoder) { fatalError("not from a storyboard") }

  // MARK: Props and commands

  func setFontSize(_ points: CGFloat) {
    applyFont(points)
    needsFull = true
    schedulePull()
    relayout()
  }

  private func applyFont(_ points: CGFloat) {
    let size = max(points, 4)
    font = UIFont.monospacedSystemFont(ofSize: size, weight: .regular)
    boldFont = UIFont.monospacedSystemFont(ofSize: size, weight: .bold)
    italicFont = Self.italic(font)
    boldItalicFont = Self.italic(boldFont)
    let ct = font as CTFont
    var glyph = CGGlyph(0)
    var m: UniChar = 0x4D // "M"
    CTFontGetGlyphsForCharacters(ct, &m, &glyph, 1)
    cellW = CGFloat(CTFontGetAdvancesForGlyphs(ct, .horizontal, &glyph, nil, 1))
    if cellW <= 0 { cellW = size * 0.6 }
    let ascent = CTFontGetAscent(ct)
    let descent = CTFontGetDescent(ct)
    cellH = (ascent + descent) * 1.15
    baseline = ascent + (cellH - (ascent + descent)) / 2
  }

  private static func italic(_ font: UIFont) -> UIFont {
    guard let descriptor = font.fontDescriptor.withSymbolicTraits(font.fontDescriptor.symbolicTraits.union(.traitItalic)) else { return font }
    return UIFont(descriptor: descriptor, size: font.pointSize)
  }

  /// Theme colors are CSS hex (`#rrggbb` or `#rrggbbaa`); the VT gets them opaque.
  func setTheme(background: String, foreground: String, cursor: String, selection: String, palette: [String]) {
    guard let bg = CssColor(background), let fg = CssColor(foreground), let cur = CssColor(cursor) else { return }
    self.background = bg
    self.foreground = fg
    cursorColor = cur
    selectionColor = CssColor(selection) ?? selectionColor
    vtPalette = palette256(palette.map { CssColor($0)?.rgb ?? 0x808080 })
    backgroundColor = bg.color
    let r = CGFloat((bg.rgb >> 16) & 0xff), g = CGFloat((bg.rgb >> 8) & 0xff), b = CGFloat(bg.rgb & 0xff)
    keyboardAppearance = (0.299 * r + 0.587 * g + 0.114 * b) / 255 < 0.5 ? .dark : .light
    applyTheme()
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
    schedulePull()
  }

  /// A new attachment: the screen and history start empty.
  func reset() {
    vt = nil
    pending.removeAll()
    cols = 0
    rows = 0
    grid = []
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
    let c = max(2, Int(w / cellW))
    let r = max(2, Int(h / cellH))
    let scale = max(traitCollection.displayScale, 1)
    if let vt {
      guard c != cols || r != rows else { return }
      vt.resize(cols: c, rows: r, cellWidth: cellW * scale, cellHeight: cellH * scale)
    } else {
      guard let made = Vt(cols: c, rows: r) else { return }
      vt = made
      applyTheme()
      for data in pending { made.write(data) }
      pending.removeAll()
    }
    cols = c
    rows = r
    grid = Array(repeating: [], count: r)
    needsFull = true
    pull()
    setNeedsDisplay()
    onSize(c, r)
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
      grid = (0..<frame.rows).map { $0 < grid.count ? grid[$0] : [] }
    }
    rows = frame.rows
    cols = frame.cols
    cursorCol = frame.cursorCol
    cursorRow = frame.cursorRow
    cursorVisible = frame.cursorVisible
    for (row, runs) in frame.dirty where row < grid.count {
      grid[row] = runs.map(Cell.init)
    }
  }

  // MARK: Drawing

  override func draw(_ rect: CGRect) {
    if needsPull { pull() }
    guard let ctx = UIGraphicsGetCurrentContext() else { return }
    ctx.setShouldAntialias(false)
    ctx.setFillColor(background.color.cgColor)
    ctx.fill(bounds)
    ctx.textMatrix = CGAffineTransform(scaleX: 1, y: -1)
    for (r, row) in grid.enumerated() {
      let y = CGFloat(r) * cellH
      guard y < rect.maxY, y + cellH > rect.minY else { continue }
      if r == selRow {
        ctx.setShouldAntialias(false)
        ctx.setFillColor(selectionColor.color.cgColor)
        ctx.fill(CGRect(x: 0, y: y, width: bounds.width, height: cellH))
      }
      for cell in row { draw(cell, y: y, in: ctx) }
    }
    drawCursor(in: ctx)
  }

  private func draw(_ cell: Cell, y: CGFloat, in ctx: CGContext) {
    let run = cell.run
    let x = CGFloat(run.col) * cellW
    let width = CGFloat(run.cells) * cellW
    let bg: UIColor? = run.inverse ? UIColor(argb: run.fg) : run.bg != 0 ? UIColor(argb: run.bg) : nil
    let fg: UIColor = run.inverse ? (run.bg != 0 ? UIColor(argb: run.bg) : background.color) : UIColor(argb: run.fg)
    if let bg {
      ctx.setShouldAntialias(false)
      ctx.setFillColor(bg.cgColor)
      ctx.fill(CGRect(x: x, y: y, width: width, height: cellH))
    }
    let font = run.bold ? (run.italic ? boldItalicFont : boldFont) : (run.italic ? italicFont : self.font)
    if cell.line == nil {
      let attributes: [NSAttributedString.Key: Any] = [
        .font: font,
        NSAttributedString.Key(kCTForegroundColorAttributeName as String): fg.cgColor,
      ]
      let line = CTLineCreateWithAttributedString(NSAttributedString(string: run.text, attributes: attributes))
      cell.line = line
      cell.width = CGFloat(CTLineGetTypographicBounds(line, nil, nil, nil))
    }
    guard let line = cell.line else { return }
    ctx.setShouldAntialias(true)
    // A wide character or a grapheme wider than its cells is fitted to them.
    let fit = cell.width > width + 0.5 ? width / cell.width : 1
    if fit < 1 {
      ctx.saveGState()
      ctx.translateBy(x: x, y: 0)
      ctx.scaleBy(x: fit, y: 1)
      ctx.textPosition = CGPoint(x: 0, y: y + baseline)
      CTLineDraw(line, ctx)
      ctx.restoreGState()
    } else {
      ctx.textPosition = CGPoint(x: x, y: y + baseline)
      CTLineDraw(line, ctx)
    }
    if run.underline {
      ctx.setShouldAntialias(false)
      ctx.setFillColor(fg.cgColor)
      let thickness = max(CTFontGetUnderlineThickness(font as CTFont), 1 / max(contentScaleFactor, 1))
      ctx.fill(CGRect(x: x, y: y + baseline - CTFontGetUnderlinePosition(font as CTFont), width: width, height: thickness))
    }
  }

  private func drawCursor(in ctx: CGContext) {
    guard cursorVisible, vt != nil else { return }
    let x = CGFloat(cursorCol) * cellW
    let y = CGFloat(cursorRow) * cellH
    if !marked.isEmpty {
      // The composition (IME) at the cursor, underlined; only the committed text is sent.
      let attributes: [NSAttributedString.Key: Any] = [
        .font: font,
        NSAttributedString.Key(kCTForegroundColorAttributeName as String): foreground.color.cgColor,
      ]
      let line = CTLineCreateWithAttributedString(NSAttributedString(string: marked, attributes: attributes))
      let w = CGFloat(CTLineGetTypographicBounds(line, nil, nil, nil))
      ctx.setShouldAntialias(false)
      ctx.setFillColor(background.color.cgColor)
      ctx.fill(CGRect(x: x, y: y, width: w, height: cellH))
      ctx.setShouldAntialias(true)
      ctx.textPosition = CGPoint(x: x, y: y + baseline)
      CTLineDraw(line, ctx)
      ctx.setShouldAntialias(false)
      ctx.setFillColor(foreground.color.cgColor)
      ctx.fill(CGRect(x: x, y: y + baseline - CTFontGetUnderlinePosition(font as CTFont), width: w, height: 1))
    } else {
      ctx.setShouldAntialias(false)
      ctx.setFillColor(cursorColor.color.withAlphaComponent(0.8).cgColor)
      ctx.fill(CGRect(x: x, y: y, width: cellW, height: cellH))
    }
  }

  // MARK: Touch: a tap focuses and names the cell; a drag scrolls the history; a long press copies a line.

  private func cell(at point: CGPoint) -> (col: Int, row: Int) {
    let col = min(max(Int(point.x / max(cellW, 1)), 0), max(cols - 1, 0))
    let row = min(max(Int(point.y / max(cellH, 1)), 0), max(rows - 1, 0))
    return (col, row)
  }

  @objc private func tapped(_ gesture: UITapGestureRecognizer) {
    showKeyboard()
    let (col, row) = cell(at: gesture.location(in: self))
    onTapCell(col, row)
  }

  @objc private func panned(_ gesture: UIPanGestureRecognizer) {
    switch gesture.state {
    case .began:
      scrollRemainder = 0
    case .changed:
      // A drag down moves back in time: positive lines.
      scrollRemainder += gesture.translation(in: self).y / max(cellH, 1)
      gesture.setTranslation(.zero, in: self)
      let lines = Int(scrollRemainder)
      if lines != 0 {
        scrollRemainder -= CGFloat(lines)
        onScroll(lines)
      }
    default:
      break
    }
  }

  @objc private func pressed(_ gesture: UILongPressGestureRecognizer) {
    guard gesture.state == .began else { return }
    let point = gesture.location(in: self)
    selRow = cell(at: point).row
    setNeedsDisplay()
    editMenu.presentEditMenu(with: UIEditMenuConfiguration(identifier: nil, sourcePoint: point))
  }

  /// The selected line's text, trailing blanks dropped.
  func lineText(_ row: Int) -> String {
    guard row >= 0, row < grid.count else { return "" }
    var line = ""
    var col = 0
    for cell in grid[row] {
      if cell.run.col > col { line += String(repeating: " ", count: cell.run.col - col) }
      line += cell.run.text
      col = cell.run.col + cell.run.cells
    }
    while line.last == " " { line.removeLast() }
    return line
  }

  func editMenuInteraction(_ interaction: UIEditMenuInteraction, menuFor configuration: UIEditMenuConfiguration,
                           suggestedActions: [UIMenuElement]) -> UIMenu? {
    var actions: [UIMenuElement] = [
      UIAction(title: "Copy", image: UIImage(systemName: "doc.on.doc")) { [weak self] _ in
        guard let self else { return }
        UIPasteboard.general.string = self.lineText(self.selRow)
      },
    ]
    if UIPasteboard.general.hasStrings {
      actions.append(UIAction(title: "Paste", image: UIImage(systemName: "doc.on.clipboard")) { [weak self] _ in
        if let text = UIPasteboard.general.string { self?.send(text) }
      })
    }
    return UIMenu(children: actions)
  }

  func editMenuInteraction(_ interaction: UIEditMenuInteraction, willDismissMenuFor configuration: UIEditMenuConfiguration,
                           animator: UIEditMenuInteractionAnimating) {
    animator.addCompletion { [weak self] in
      self?.selRow = -1
      self?.setNeedsDisplay()
    }
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
    CGRect(x: CGFloat(cursorCol) * cellW, y: CGFloat(cursorRow) * cellH, width: cellW, height: cellH)
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
