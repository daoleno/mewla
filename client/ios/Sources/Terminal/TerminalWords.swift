/// The word a long press selects in a terminal row, as the app's chat selects one (NSH's selection model) and a
/// browser does: a run of letters, digits and `_`; a hostname, path, URL, version or id whole (words joined by
/// `- . / : @ ~ %`, a URL's `? = & #` after a `/`, a path's leading `/ ~ . - @` and a directory's last `/`); each CJK
/// character, and each other mark, a word of its own. Punctuation around a word is not part of it, and a row of
/// Chinese without spaces is not one word. The same rule as Android's TerminalWords.kt.
///
/// `cells` holds each cell's code point: 0 (or a space) for a blank, `wide` for the second cell of a wide character.
/// The result is the first and last cell of the word at `col`, nil on a blank.
enum TerminalWords {
  static let wide = -1

  static func at(_ cells: [Int], _ col: Int) -> (Int, Int)? {
    guard cells.indices.contains(col) else { return nil }
    // The row's characters: each with its first and last cell.
    var chars: [Int] = [], first: [Int] = [], last: [Int] = []
    for c in cells.indices {
      if cells[c] == wide, !chars.isEmpty {
        last[last.count - 1] = c
        continue
      }
      chars.append(cells[c] == wide ? 0 : cells[c])
      first.append(c)
      last.append(c)
    }
    guard let index = first.lastIndex(where: { $0 <= col }), kind(chars[index]) != .space else { return nil }
    if let (a, b) = tokenAt(chars, index) { return (first[a], last[b]) }
    var a = index, b = index
    let k = kind(chars[index])
    if k != .single {
      while a > 0, kind(chars[a - 1]) == k { a -= 1 }
      while b < chars.count - 1, kind(chars[b + 1]) == k { b += 1 }
    }
    return (first[a], last[b])
  }

  private enum Kind { case word, space, single }

  private static func kind(_ cp: Int) -> Kind {
    guard cp > 0, let scalar = Unicode.Scalar(UInt32(cp)) else { return .space }
    if scalar.properties.isWhitespace { return .space }
    if spaceless(cp) { return .single }
    switch scalar.properties.generalCategory {
    case .uppercaseLetter, .lowercaseLetter, .titlecaseLetter, .modifierLetter, .otherLetter, .decimalNumber:
      return .word
    default:
      return cp == 0x5F || combining(cp) ? .word : .single
    }
  }

  /// Scripts written without spaces between words (CJK ideographs, kana): each character is a word.
  private static func spaceless(_ cp: Int) -> Bool {
    (0x3040...0x30FF).contains(cp) || (0x31F0...0x31FF).contains(cp) || (0x3400...0x4DBF).contains(cp) ||
      (0x4E00...0x9FFF).contains(cp) || (0xF900...0xFAFF).contains(cp) || (0xFF66...0xFF9F).contains(cp) ||
      (0x20000...0x3FFFF).contains(cp)
  }

  /// Marks that belong to the word before them (accents written apart).
  private static func combining(_ cp: Int) -> Bool {
    (0x0300...0x036F).contains(cp) || (0x1AB0...0x1AFF).contains(cp) || (0x1DC0...0x1DFF).contains(cp) ||
      (0x20D0...0x20FF).contains(cp) || (0xFE20...0xFE2F).contains(cp) || cp == 0x200D || cp == 0xFE0F
  }

  private static func ascii(_ cp: Int, in set: String) -> Bool {
    cp < 0x80 && set.unicodeScalars.contains { Int($0.value) == cp }
  }

  private static func query(_ cp: Int) -> Bool { ascii(cp, in: "?=&#") }

  private static func joiner(_ cp: Int, withQuery: Bool) -> Bool { ascii(cp, in: "-./:@~%") || (withQuery && query(cp)) }

  private static func leading(_ cp: Int) -> Bool { ascii(cp, in: "/~.-@") }

  /// The token around `index`: words joined by runs of joiners, as `at` says; nil when there is none.
  private static func tokenAt(_ chars: [Int], _ index: Int) -> (Int, Int)? {
    guard let (a, b) = grow(chars, index, withQuery: true) else { return nil }
    if let q = (a...b).first(where: { query(chars[$0]) }), !(a..<q).contains(where: { chars[$0] == 0x2F }) {
      return grow(chars, index, withQuery: false)
    }
    return (a, b)
  }

  private static func grow(_ chars: [Int], _ index: Int, withQuery: Bool) -> (Int, Int)? {
    let n = chars.count
    func word(_ i: Int) -> Bool { kind(chars[i]) == .word }
    func join(_ i: Int) -> Bool { joiner(chars[i], withQuery: withQuery) }
    let core: Int
    if word(index) {
      core = index
    } else if join(index) {
      var a = index, b = index
      while a > 0, join(a - 1) { a -= 1 }
      while b + 1 < n, join(b + 1) { b += 1 }
      let before = a > 0 && word(a - 1)
      let after = b + 1 < n && word(b + 1)
      if before && after {
        core = a - 1
      } else if after, !before, (a...b).allSatisfy({ leading(chars[$0]) }) {
        core = b + 1
      } else {
        return nil
      }
    } else {
      return nil
    }
    var first = core, last = core
    while true {
      while last + 1 < n, word(last + 1) { last += 1 }
      var j = last + 1
      while j < n, join(j) { j += 1 }
      if j > last + 1, j < n, word(j) { last = j } else { break }
    }
    while true {
      while first > 0, word(first - 1) { first -= 1 }
      var j = first
      while j > 0, join(j - 1) { j -= 1 }
      if j < first, j > 0, word(j - 1) { first = j - 1 } else { break }
    }
    while first > 0, leading(chars[first - 1]) { first -= 1 }
    if last + 1 < n, chars[last + 1] == 0x2F { last += 1 }
    return (first, last)
  }
}
