package com.daoleno.mewla.terminal

import org.junit.Assert.assertEquals
import org.junit.Test

class TerminalCompositionTest {
  private val sent = mutableListOf<String>()
  private val composition = TerminalComposition { sent += it }
  private fun program() = sent.joinToString("")

  /**
   * An input method that composes a word letter by letter and, before each update, reads the text before
   * the cursor: when its composition is not there, it takes the word for committed by the field and
   * finishes it before composing on (the owner's phone, 11-13: `touch` typed, `ttotoutouctouch` sent).
   * `before` is what the field tells it.
   */
  private fun checkingInputMethod(word: String, before: () -> String) {
    var composed = ""
    for (letter in word) {
      if (composed.isNotEmpty() && !before().endsWith(composed)) composition.finish()
      composed += letter
      composition.compose(composed)
    }
  }

  @Test fun aFieldThatReadsEmptyWhileComposingSendsEveryPrefix() {
    // The view before this change: its field read empty, whatever was composed.
    checkingInputMethod("touch") { "" }
    assertEquals("ttotoutouc", program())
    assertEquals("touch", composition.text)
  }

  @Test fun theFieldIsTheCompositionSoTheWordGoesOutOnceWhenCommitted() {
    checkingInputMethod("touch") { composition.field.text.substring(0, composition.field.cursor) }
    assertEquals("", program())
    assertEquals(TerminalComposition.Field("touch", 5, 0, 5), composition.field)
    composition.commit("touch")
    assertEquals("touch", program())
    assertEquals(TerminalComposition.Field("", 0, -1, -1), composition.field)
  }

  @Test fun composingTextNeverReachesTheProgramAndACancelledCompositionSendsNothing() {
    for (step in listOf("t", "to", "tou")) composition.compose(step)
    composition.compose("")
    composition.finish()
    assertEquals("", program())
  }

  @Test fun pinyinLettersNeverReachTheShellTheChosenCharactersDoOnce() {
    for (step in listOf("n", "ni", "ni h", "ni ha", "ni hao")) composition.compose(step)
    composition.commit("你好")
    assertEquals(listOf("你好"), sent)
  }

  @Test fun aKeyCommitsTheCompositionFirstThenGoes() {
    composition.compose("ls")
    composition.key("\r")
    assertEquals(listOf("ls", "\r"), sent)
    composition.compose("git")
    composition.key("\u0003")
    assertEquals(listOf("ls", "\r", "git", "\u0003"), sent)
  }

  @Test fun backspaceEditsTheCompositionNotTheProgram() {
    composition.compose("touc")
    composition.backspace()
    assertEquals("tou", composition.text)
    assertEquals("", program())
    composition.finish()
    composition.backspace(2)
    assertEquals(listOf("tou", "\u007f", "\u007f"), sent)
  }

  @Test fun finishingTwiceSendsOnce() {
    composition.compose("echo")
    composition.finish()
    composition.finish()
    assertEquals(listOf("echo"), sent)
  }
}
