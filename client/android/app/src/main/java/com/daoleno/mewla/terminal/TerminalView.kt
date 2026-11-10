package com.daoleno.mewla.terminal

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.Typeface
import android.text.InputType
import android.view.ActionMode
import android.view.GestureDetector
import android.view.KeyEvent
import android.view.Menu
import android.view.MenuItem
import android.view.MotionEvent
import android.view.View
import android.view.inputmethod.BaseInputConnection
import android.view.inputmethod.EditorInfo
import android.view.inputmethod.InputConnection
import android.view.inputmethod.InputMethodManager

/**
 * A terminal drawn with Android's own text stack from libghostty-vt's cells.
 * The system keyboard types into it (composition drawn at the cursor, only
 * committed text reported); a drag scrolls the history (reported, the
 * computer's tmux copy mode moves); a tap focuses and reports the cell; a long
 * press selects a line and offers the system's Copy.
 */
class TerminalView(context: Context, private val events: Events) : View(context) {
  interface Events {
    fun size(cols: Int, rows: Int)
    fun input(data: String)
    fun scroll(lines: Int)
    fun tapCell(col: Int, row: Int)
  }

  private class Run(val col: Int, val cells: Int, val fg: Int, val bg: Int, val flags: Int, val text: String)

  private var handle = 0L
  private var rows = 0
  private var cols = 0
  private var grid: Array<List<Run>> = emptyArray()
  private var cursorCol = 0
  private var cursorRow = 0
  private var cursorVisible = true
  private var composing = ""
  private val text = Paint(Paint.ANTI_ALIAS_FLAG).apply { typeface = Typeface.MONOSPACE }
  private val bold = Paint(text).apply { typeface = Typeface.create(Typeface.MONOSPACE, Typeface.BOLD) }
  private val fill = Paint()
  private var cellW = 0f
  private var cellH = 0f
  private var baseline = 0f
  private var background = Color.parseColor("#141210")
  private var foreground = Color.parseColor("#F4F0EA")
  private var cursorColor = Color.parseColor("#F4F0EA")
  private var selectionColor = Color.parseColor("#553D7EFF")
  private var theme: Pair<List<String>, Array<String>>? = null
  private var selRow = -1
  private var scrollRemainder = 0f

  init {
    isFocusable = true
    isFocusableInTouchMode = true
    setFontSize(13f)
  }

  fun setFontSize(sp: Float) {
    text.textSize = sp * resources.displayMetrics.scaledDensity
    bold.textSize = text.textSize
    val m = text.fontMetrics
    cellW = text.measureText("M")
    cellH = (m.descent - m.ascent) * 1.15f
    baseline = -m.ascent + (cellH - (m.descent - m.ascent)) / 2
    relayout(width, height)
  }

  fun setTheme(background: String, foreground: String, cursor: String, selection: String, palette: List<String>) {
    this.background = Color.parseColor(background)
    this.foreground = Color.parseColor(foreground)
    cursorColor = Color.parseColor(cursor)
    selectionColor = Color.parseColor(selection)
    theme = listOf(foreground, background, cursor) to palette256(palette)
    applyTheme()
    invalidate()
  }

  private fun applyTheme() {
    val (colors, palette) = theme ?: return
    if (handle == 0L) return
    Vt.nativeSetTheme(handle, colors[0], colors[1], colors[2], palette)
    pull(true)
  }

  override fun onSizeChanged(w: Int, h: Int, ow: Int, oh: Int) = relayout(w, h)

  private fun relayout(w: Int, h: Int) {
    if (w <= 0 || h <= 0 || cellW <= 0f) return
    val c = maxOf(2, (w / cellW).toInt())
    val r = maxOf(2, (h / cellH).toInt())
    if (handle == 0L) {
      handle = Vt.nativeCreateTerminal(c, r)
      applyTheme()
    } else if (c != cols || r != rows) {
      Vt.nativeResize(handle, c, r, cellW, cellH)
    } else return
    cols = c
    rows = r
    grid = Array(r) { emptyList() }
    pull(true)
    events.size(c, r)
  }

  fun write(data: String) {
    if (handle == 0L) return
    Vt.nativeWriteBytes(handle, data.toByteArray(Charsets.UTF_8))
    pull(false)
  }

  /** A new attachment: the screen and history start empty. */
  fun reset() {
    if (handle != 0L) Vt.nativeDestroyTerminal(handle)
    handle = 0L
    cols = 0
    rows = 0
    relayout(width, height)
    invalidate()
  }

  private fun pull(full: Boolean) {
    val res = Vt.nativeRowRuns(handle, full) ?: return
    val m = res[0] as IntArray
    @Suppress("UNCHECKED_CAST") val t = res[1] as Array<String>
    if (m[0] != grid.size) grid = Array(m[0]) { i -> grid.getOrNull(i) ?: emptyList() }
    rows = m[0]; cols = m[1]; cursorCol = m[2]; cursorRow = m[3]; cursorVisible = m[4] == 1
    var p = 7
    var ti = 0
    repeat(m[6]) {
      val row = m[p]; val n = m[p + 1]; p += 2
      val list = ArrayList<Run>(n)
      repeat(n) { list.add(Run(m[p], m[p + 1], m[p + 2], m[p + 3], m[p + 4], t[ti++])); p += 5 }
      if (row < grid.size) grid[row] = list
    }
    invalidate()
  }

  override fun onDraw(canvas: Canvas) {
    canvas.drawColor(background)
    for (r in grid.indices) {
      val y = r * cellH
      if (r == selRow) { fill.color = selectionColor; canvas.drawRect(0f, y, width.toFloat(), y + cellH, fill) }
      for (run in grid[r]) {
        val inverse = run.flags and 8 != 0
        val bg = if (inverse) run.fg else run.bg
        val fg = if (inverse) (if (run.bg != 0) run.bg else background) else run.fg
        val x = run.col * cellW
        if (bg != 0) { fill.color = bg; canvas.drawRect(x, y, x + run.cells * cellW, y + cellH, fill) }
        val paint = if (run.flags and 1 != 0) bold else text
        paint.color = if (fg != 0) fg else foreground
        paint.isUnderlineText = run.flags and 4 != 0
        paint.textSkewX = if (run.flags and 2 != 0) -0.2f else 0f
        if (run.cells == run.text.length) canvas.drawText(run.text, x, y + baseline, paint)
        else { // a wide character or a grapheme: fit it to its cells
          val w = paint.measureText(run.text)
          val s = run.cells * cellW / maxOf(w, 1f)
          canvas.save(); canvas.scale(minOf(1f, s), 1f, x, 0f); canvas.drawText(run.text, x, y + baseline, paint); canvas.restore()
        }
      }
    }
    if (!cursorVisible) return
    val x = cursorCol * cellW
    val y = cursorRow * cellH
    if (composing.isNotEmpty()) {
      val w = text.measureText(composing)
      fill.color = background; canvas.drawRect(x, y, x + w, y + cellH, fill)
      text.color = foreground; text.isUnderlineText = true
      canvas.drawText(composing, x, y + baseline, text)
      text.isUnderlineText = false
    } else {
      fill.color = cursorColor; fill.alpha = 0xCC
      canvas.drawRect(x, y, x + cellW, y + cellH, fill)
      fill.alpha = 0xFF
    }
  }

  // The system keyboard, composition (IME) drawn at the cursor.
  override fun onCheckIsTextEditor() = true
  override fun onCreateInputConnection(out: EditorInfo): InputConnection {
    out.inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS or InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD
    out.imeOptions = EditorInfo.IME_FLAG_NO_FULLSCREEN or EditorInfo.IME_FLAG_NO_EXTRACT_UI
    return object : BaseInputConnection(this, false) {
      override fun commitText(t: CharSequence, pos: Int): Boolean { composing = ""; send(t.toString()); invalidate(); return true }
      override fun setComposingText(t: CharSequence, pos: Int): Boolean { composing = t.toString(); invalidate(); return true }
      override fun finishComposingText(): Boolean { if (composing.isNotEmpty()) { val c = composing; composing = ""; send(c) }; return true }
      override fun deleteSurroundingText(before: Int, after: Int): Boolean { repeat(maxOf(before, 1)) { send("\u007f") }; return true }
      override fun sendKeyEvent(e: KeyEvent): Boolean { if (e.action == KeyEvent.ACTION_DOWN) keyToData(e)?.let(::send); return true }
    }
  }

  override fun onKeyDown(code: Int, e: KeyEvent): Boolean {
    keyToData(e)?.let { send(it); return true }
    return super.onKeyDown(code, e)
  }

  private fun keyToData(e: KeyEvent): String? = when (e.keyCode) {
    KeyEvent.KEYCODE_ENTER -> "\r"
    KeyEvent.KEYCODE_DEL -> "\u007f"
    KeyEvent.KEYCODE_FORWARD_DEL -> "\u001b[3~"
    KeyEvent.KEYCODE_TAB -> "\t"
    KeyEvent.KEYCODE_ESCAPE -> "\u001b"
    KeyEvent.KEYCODE_DPAD_UP -> "\u001b[A"
    KeyEvent.KEYCODE_DPAD_DOWN -> "\u001b[B"
    KeyEvent.KEYCODE_DPAD_RIGHT -> "\u001b[C"
    KeyEvent.KEYCODE_DPAD_LEFT -> "\u001b[D"
    else -> {
      val ch = e.unicodeChar
      when {
        ch == 0 -> null
        e.isCtrlPressed && ch in 'a'.code..'z'.code -> (ch - 'a'.code + 1).toChar().toString()
        else -> String(Character.toChars(ch))
      }
    }
  }

  private fun send(s: String) = events.input(s)

  fun showKeyboard() {
    requestFocus()
    context.getSystemService(InputMethodManager::class.java).showSoftInput(this, 0)
  }

  fun hideKeyboard() {
    context.getSystemService(InputMethodManager::class.java).hideSoftInputFromWindow(windowToken, 0)
    clearFocus()
  }

  // Touch: a tap focuses (keyboard) and names the cell; a drag scrolls the history; a long press copies a line.
  private val gestures = GestureDetector(context, object : GestureDetector.SimpleOnGestureListener() {
    override fun onDown(e: MotionEvent): Boolean { scrollRemainder = 0f; return true }

    override fun onSingleTapUp(e: MotionEvent): Boolean {
      showKeyboard()
      if (cellW > 0f && cellH > 0f) events.tapCell((e.x / cellW).toInt().coerceIn(0, maxOf(cols - 1, 0)), (e.y / cellH).toInt().coerceIn(0, maxOf(rows - 1, 0)))
      return true
    }

    override fun onScroll(e1: MotionEvent?, e2: MotionEvent, dx: Float, dy: Float): Boolean {
      // A drag down (dy < 0) moves back in time: positive lines.
      scrollRemainder += -dy / cellH
      val lines = scrollRemainder.toInt()
      if (lines != 0) { scrollRemainder -= lines; events.scroll(lines) }
      return true
    }

    override fun onLongPress(e: MotionEvent) {
      selRow = (e.y / cellH).toInt()
      invalidate()
      startActionMode(object : ActionMode.Callback2() {
        override fun onCreateActionMode(m: ActionMode, menu: Menu): Boolean { menu.add(0, 1, 0, android.R.string.copy); return true }
        override fun onPrepareActionMode(m: ActionMode, menu: Menu) = false
        override fun onActionItemClicked(m: ActionMode, item: MenuItem): Boolean {
          val line = grid.getOrNull(selRow)?.joinToString("") { it.text }?.trimEnd() ?: ""
          context.getSystemService(ClipboardManager::class.java).setPrimaryClip(ClipData.newPlainText("terminal", line))
          m.finish()
          return true
        }
        override fun onDestroyActionMode(m: ActionMode) { selRow = -1; invalidate() }
      }, ActionMode.TYPE_FLOATING)
    }
  })

  override fun onTouchEvent(e: MotionEvent): Boolean = gestures.onTouchEvent(e) || super.onTouchEvent(e)

  fun destroy() {
    if (handle != 0L) Vt.nativeDestroyTerminal(handle)
    handle = 0L
  }
}
