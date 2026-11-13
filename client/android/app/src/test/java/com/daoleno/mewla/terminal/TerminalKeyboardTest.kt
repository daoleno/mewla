package com.daoleno.mewla.terminal

import android.text.InputType
import android.view.inputmethod.EditorInfo
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class TerminalKeyboardTest {
  private val type = TerminalKeyboard.INPUT_TYPE

  @Test fun anOrdinaryTextFieldSoThePersonsOwnKeyboardAndItsComposingComeUp() {
    assertEquals(InputType.TYPE_CLASS_TEXT, type and InputType.TYPE_MASK_CLASS)
  }

  @Test fun noPasswordVariationSoNoVendorSecureKeyboard() {
    // Any variation at all: the password ones (PASSWORD, VISIBLE_PASSWORD, WEB_PASSWORD) are the trigger.
    assertEquals(0, type and InputType.TYPE_MASK_VARIATION)
  }

  @Test fun eachKeyAsTypedNoSuggestionsCorrectionsOrCapitals() {
    assertTrue(type and InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS != 0)
    val unwanted = InputType.TYPE_TEXT_FLAG_AUTO_CORRECT or InputType.TYPE_TEXT_FLAG_AUTO_COMPLETE or
      InputType.TYPE_TEXT_FLAG_CAP_CHARACTERS or InputType.TYPE_TEXT_FLAG_CAP_WORDS or InputType.TYPE_TEXT_FLAG_CAP_SENTENCES
    assertEquals(0, type and unwanted)
  }

  @Test fun theKeyboardNeverCoversTheTerminalWithItsOwnEditBox() {
    val want = EditorInfo.IME_FLAG_NO_FULLSCREEN or EditorInfo.IME_FLAG_NO_EXTRACT_UI
    assertEquals(want, TerminalKeyboard.IME_OPTIONS and want)
  }
}
