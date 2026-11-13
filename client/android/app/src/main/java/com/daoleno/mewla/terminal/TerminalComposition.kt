package com.daoleno.mewla.terminal

/**
 * What the keyboard is composing in the terminal, and what of it reaches the program. Only committed text
 * goes out (`send`), once; the composition is drawn at the cursor and changes as the input method changes it.
 *
 * The terminal's field holds nothing but the composition, and the input method is told so: the text before
 * the cursor is the composition, the cursor at its end, the composing region the whole of it ([field]). A
 * field that read empty while the input method composed (the view kept the composition to itself) made an
 * input method that checks the field before each update take its word for gone: it committed it and
 * composed the word again, so every prefix reached the program ("t", "to", "tou", "touc", then "touch"
 * composing: `ttotoutouctouch` on the owner's phone, 11-13).
 */
internal class TerminalComposition(private val send: (String) -> Unit) {
  var text = ""
    private set

  /** The field as the input method sees it: its text, the cursor, the composing region (-1, -1 without one). */
  data class Field(val text: String, val cursor: Int, val composingStart: Int, val composingEnd: Int)

  val field: Field
    get() = if (text.isEmpty()) Field("", 0, -1, -1) else Field(text, text.length, 0, text.length)

  /** The input method's composition now; an empty one is a cancelled composition, and nothing goes out. */
  fun compose(next: String) {
    text = next
  }

  /** Text the input method commits (a word, the chosen characters): it replaces the composition and goes out once. */
  fun commit(committed: String) {
    text = ""
    if (committed.isNotEmpty()) send(committed)
  }

  /** The composition is final as it stands (the input method finished it, or a key or a paste comes after it). */
  fun finish() {
    val done = text
    text = ""
    if (done.isNotEmpty()) send(done)
  }

  /** A key (Enter, Tab, Esc, ^C, an arrow): the composition goes out first, as committed, then the key. */
  fun key(data: String) {
    finish()
    if (data.isNotEmpty()) send(data)
  }

  /** Backspace: inside a composition it edits the composition; outside it is the program's. */
  fun backspace(count: Int = 1) {
    if (text.isNotEmpty()) {
      text = text.dropLast(count.coerceAtLeast(1))
      return
    }
    repeat(count.coerceAtLeast(1)) { send("\u007f") }
  }
}
