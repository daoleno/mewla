package com.daoleno.mewla.terminal

/**
 * The word a long press selects in a terminal row, as the app's chat selects one (NSH's selection model) and a
 * browser does: a run of letters, digits and `_`; a hostname, path, URL, version or id whole (words joined by
 * `- . / : @ ~ %`, a URL's `? = & #` after a `/`, a path's leading `/ ~ . - @` and a directory's last `/`); each CJK
 * character, and each other mark, a word of its own. Punctuation around a word is not part of it, and a row of
 * Chinese without spaces is not one word.
 *
 * `cells` holds each cell's code point: 0 (or a space) for a blank, [WIDE] for the second cell of a wide character.
 * The result is the first and last cell of the word at `col`, null on a blank.
 */
object TerminalWords {
  const val WIDE = -1

  fun at(cells: IntArray, col: Int): Pair<Int, Int>? {
    if (col !in cells.indices) return null
    // The row's characters: each with its first and last cell.
    val chars = ArrayList<Int>()
    val first = ArrayList<Int>()
    val last = ArrayList<Int>()
    for (c in cells.indices) {
      if (cells[c] == WIDE && chars.isNotEmpty()) {
        last[last.size - 1] = c
        continue
      }
      chars.add(if (cells[c] == WIDE) 0 else cells[c])
      first.add(c)
      last.add(c)
    }
    val index = first.indexOfLast { it <= col }
    if (index < 0 || kind(chars[index]) == Kind.SPACE) return null
    val (a, b) = tokenAt(chars, index) ?: run {
      var a = index
      var b = index
      val k = kind(chars[index])
      if (k != Kind.SINGLE) {
        while (a > 0 && kind(chars[a - 1]) == k) a--
        while (b < chars.size - 1 && kind(chars[b + 1]) == k) b++
      }
      a to b
    }
    return first[a] to last[b]
  }

  private enum class Kind { WORD, SPACE, SINGLE }

  private fun kind(cp: Int): Kind = when {
    cp == 0 || cp == ' '.code || cp == '\t'.code -> Kind.SPACE
    Character.isWhitespace(cp) || Character.isSpaceChar(cp) -> Kind.SPACE
    spaceless(cp) -> Kind.SINGLE
    Character.isLetterOrDigit(cp) || cp == '_'.code || combining(cp) -> Kind.WORD
    else -> Kind.SINGLE
  }

  /** Scripts written without spaces between words (CJK ideographs, kana): each character is a word. */
  private fun spaceless(cp: Int): Boolean =
    cp in 0x3040..0x30FF || cp in 0x31F0..0x31FF || cp in 0x3400..0x4DBF || cp in 0x4E00..0x9FFF ||
      cp in 0xF900..0xFAFF || cp in 0xFF66..0xFF9F || cp in 0x20000..0x3FFFF

  /** Marks that belong to the word before them (accents written apart). */
  private fun combining(cp: Int): Boolean =
    cp in 0x0300..0x036F || cp in 0x1AB0..0x1AFF || cp in 0x1DC0..0x1DFF || cp in 0x20D0..0x20FF ||
      cp in 0xFE20..0xFE2F || cp == 0x200D || cp == 0xFE0F

  private fun query(cp: Int) = cp == '?'.code || cp == '='.code || cp == '&'.code || cp == '#'.code

  private fun joiner(cp: Int, withQuery: Boolean) = "-./:@~%".indexOf(cp.toChar()) >= 0 && cp < 0x80 || (withQuery && query(cp))

  private fun leading(cp: Int) = cp < 0x80 && "/~.-@".indexOf(cp.toChar()) >= 0

  /** The token around `index`: words joined by runs of joiners, as [at] says; null when there is none. */
  private fun tokenAt(chars: List<Int>, index: Int): Pair<Int, Int>? {
    val (a, b) = grow(chars, index, true) ?: return null
    val q = (a..b).firstOrNull { query(chars[it]) }
    return if (q != null && (a until q).none { chars[it] == '/'.code }) grow(chars, index, false) else a to b
  }

  private fun grow(chars: List<Int>, index: Int, withQuery: Boolean): Pair<Int, Int>? {
    val n = chars.size
    fun word(i: Int) = kind(chars[i]) == Kind.WORD
    fun join(i: Int) = joiner(chars[i], withQuery)
    val core = when {
      word(index) -> index
      join(index) -> {
        var a = index
        var b = index
        while (a > 0 && join(a - 1)) a--
        while (b + 1 < n && join(b + 1)) b++
        val before = a > 0 && word(a - 1)
        val after = b + 1 < n && word(b + 1)
        when {
          before && after -> a - 1
          after && !before && (a..b).all { leading(chars[it]) } -> b + 1
          else -> return null
        }
      }
      else -> return null
    }
    var first = core
    var last = core
    while (true) {
      while (last + 1 < n && word(last + 1)) last++
      var j = last + 1
      while (j < n && join(j)) j++
      if (j > last + 1 && j < n && word(j)) last = j else break
    }
    while (true) {
      while (first > 0 && word(first - 1)) first--
      var j = first
      while (j > 0 && join(j - 1)) j--
      if (j < first && j > 0 && word(j - 1)) first = j - 1 else break
    }
    while (first > 0 && leading(chars[first - 1])) first--
    if (last + 1 < n && chars[last + 1] == '/'.code) last++
    return first to last
  }
}
