package com.daoleno.mewla.terminal

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class TerminalWordsTest {
  /** A row as TerminalView hands it over: one cell per character, a CJK character in two (its second WIDE). */
  private fun row(text: String): IntArray {
    val cells = ArrayList<Int>()
    var i = 0
    while (i < text.length) {
      val cp = text.codePointAt(i)
      cells.add(cp)
      if (cp in 0x2E80..0x9FFF || cp in 0xFF00..0xFF60 || cp in 0x3000..0x303F) cells.add(TerminalWords.WIDE)
      i += Character.charCount(cp)
    }
    return cells.toIntArray()
  }

  /** The text of the word a long press at the cell of `text`'s `at`-th occurrence of `on` selects. */
  private fun word(text: String, on: String, offset: Int = 0): String? {
    val cells = row(text)
    // The cell of the character `offset` into `on`.
    val chars = text.substring(0, text.indexOf(on) + offset)
    val col = row(chars).size
    val (a, b) = TerminalWords.at(cells, col) ?: return null
    val sb = StringBuilder()
    for (c in a..b) if (cells[c] != TerminalWords.WIDE) sb.appendCodePoint(cells[c])
    return sb.toString()
  }

  @Test fun chineseWithoutSpacesIsOneCharacterAWordNotTheLine() {
    // Owner 11-29: a long press on a Worker's Chinese answer selected the whole row.
    val line = "两行末尾都有换行符，文件里没有空行，也没有多余的空"
    assertEquals("文", word(line, "文件"))
    assertEquals("件", word(line, "文件", 1))
    assertEquals("，", word(line, "，文件"))
  }

  @Test fun aWordStopsAtPunctuation() {
    assertEquals("proxy", word("The 502s were the proxy, not the app.", "proxy"))
    assertEquals("app", word("The 502s were the proxy, not the app.", "app"))
    assertEquals("hello", word("echo \"hello\" world", "hello"))
  }

  @Test fun pathsHostnamesUrlsAndVersionsStayWhole() {
    assertEquals("notes.txt", word("notes.txt，用中文告诉我它有几行", "notes", 2))
    assertEquals("~/work/atlas-api", word("cd ~/work/atlas-api && go test", "atlas"))
    assertEquals("/tmp/window-edges.log", word("tee /tmp/window-edges.log", "edges"))
    assertEquals("https://example.com/logs?id=4&x=1", word("see https://example.com/logs?id=4&x=1.", "example"))
    assertEquals("v0.4.35", word("Mewla v0.4.35 is out", "0.4"))
    // A directory's last slash stays; the dots after it are punctuation.
    assertEquals("./ratelimit/", word("go test ./ratelimit/... -run", "ratelimit"))
  }

  @Test fun aBlankSelectsNothing() {
    assertNull(word("a  b", " ", 1))
    assertNull(TerminalWords.at(IntArray(4), 2))
    assertNull(TerminalWords.at(row("ab"), 5))
  }

  @Test fun theSecondCellOfAWideCharacterIsThatCharacter() {
    val cells = row("中文")
    assertEquals(2 to 3, TerminalWords.at(cells, 3))
    assertEquals(0 to 1, TerminalWords.at(cells, 1))
  }
}
