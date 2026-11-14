package com.daoleno.mewla.terminal

import android.text.InputType
import android.view.inputmethod.EditorInfo

/**
 * What the terminal tells the keyboard it is. A terminal needs each key as typed: no autocorrect, no
 * suggestions rewriting a word, no automatic capitals. It must still be an ordinary text field, so the
 * person's own keyboard comes up with its own layout, and an input method that composes (pinyin, voice)
 * composes and commits as in any field.
 *
 * - TYPE_CLASS_TEXT: a text field. Not TYPE_NULL (Termux's default): some input methods treat a null
 *   field as raw keys and drop composing, which Chinese and Japanese input need.
 * - TYPE_TEXT_FLAG_NO_SUGGESTIONS: no suggestion strip and no word composing for Latin keys on keyboards
 *   that honour it (AOSP's and Gboard's commit each key).
 * - no variation: a password variation (VISIBLE_PASSWORD, which terminals often declare to switch
 *   suggestions off on keyboards that ignore the flag above) makes vendor keyboards bring up their secure
 *   keyboard (OnePlus/ColorOS).
 * - no TYPE_TEXT_FLAG_CAP_* and no TYPE_TEXT_FLAG_AUTO_CORRECT: no capitals or corrections of the keyboard's
 *   own accord.
 * - IME_FLAG_NO_FULLSCREEN, IME_FLAG_NO_EXTRACT_UI: in landscape the keyboard never covers the terminal
 *   with its own edit box.
 */
internal object TerminalKeyboard {
  const val INPUT_TYPE = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS
  const val IME_OPTIONS = EditorInfo.IME_FLAG_NO_FULLSCREEN or EditorInfo.IME_FLAG_NO_EXTRACT_UI
}
