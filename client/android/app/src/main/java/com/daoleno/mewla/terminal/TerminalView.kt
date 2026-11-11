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
 * committed text reported). Above the live screen it draws the history the
 * page hands it (`history`, styled rows rendered by a second VT) and a drag
 * scrolls through it here; with no history (or on the alternate screen) the
 * drag is reported (the computer's tmux copy mode moves). A tap reports the
 * cell and the text under it (the page opens links and raises the keyboard);
 * a long press selects a line and offers the system's Copy. Text a program
 * copies (OSC 52) is reported.
 */
class TerminalView(context: Context, private val events: Events) : View(context) {
  interface Events {
    fun size(cols: Int, rows: Int)
    fun failed(message: String)
    fun input(data: String)
    fun scroll(lines: Int)
    fun viewport(top: Int, atBottom: Boolean)
    fun tapCell(col: Int, row: Int, text: String, index: Int)
    fun copy(text: String)
  }

  private class Run(val col: Int, val cells: Int, val fg: Int, val bg: Int, val flags: Int, val text: String)

  private var handle = 0L
  private var failed = false
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

  // The history above the live screen: its styled rows as the page sent them
  // (drawn again on a theme change) and as runs, oldest first.
  private val historyAnsi = ArrayList<String>()
  private val history = ArrayList<List<Run>>()
  private var historyCols = 0
  private var alternate = false
  /** The VT that renders history rows, `PAGE` rows tall at the history's width; it keeps styling from one row to the next. */
  private var scratch = 0L
  /** History rows scrolled into view above the live screen; 0 is the live screen. */
  private var offset = 0
  private var markRow = -1
  private var reportedTop = -1
  private var reportedBottom = true

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

  /** Theme colors are CSS hex (`#rrggbb` or `#rrggbbaa`, what the app's `token()` gives); Android reads 8 digits as `#aarrggbb`. */
  private fun cssColor(value: String): Int {
    val hex = value.trim()
    return if (hex.length == 9 && hex.startsWith("#")) Color.parseColor("#" + hex.substring(7, 9) + hex.substring(1, 7)) else Color.parseColor(hex)
  }

  /** The VT takes opaque `#rrggbb`. */
  private fun opaque(value: String): String = value.trim().let { if (it.length == 9 && it.startsWith("#")) it.substring(0, 7) else it }

  fun setTheme(background: String, foreground: String, cursor: String, selection: String, palette: List<String>) {
    this.background = cssColor(background)
    this.foreground = cssColor(foreground)
    cursorColor = cssColor(cursor)
    selectionColor = cssColor(selection)
    theme = listOf(opaque(foreground), opaque(background), opaque(cursor)) to palette256(palette.map(::opaque))
    applyTheme()
    // The history's colors are the VT's: draw its rows again.
    destroyScratch()
    val again = renderHistory(historyAnsi)
    history.clear()
    history.addAll(again)
    invalidate()
  }

  private fun applyTheme() {
    if (handle == 0L) return
    themeVt(handle)
    pull(true)
  }

  private fun themeVt(vt: Long) {
    val (colors, palette) = theme ?: return
    Vt.nativeSetTheme(vt, colors[0], colors[1], colors[2], palette)
  }

  override fun onSizeChanged(w: Int, h: Int, ow: Int, oh: Int) = relayout(w, h)

  private fun relayout(w: Int, h: Int) {
    if (w <= 0 || h <= 0 || cellW <= 0f || failed) return
    val c = maxOf(2, (w / cellW).toInt())
    val r = maxOf(2, (h / cellH).toInt())
    if (handle == 0L) {
      // The library may not load (UnsatisfiedLinkError) or may refuse the grid: say so, never crash.
      val made = try {
        Vt.nativeCreateTerminal(c, r)
      } catch (e: Throwable) {
        fail("libghostty-vt did not load: ${e.message ?: e.javaClass.simpleName}")
        return
      }
      if (made == 0L) {
        fail("libghostty-vt could not make a ${c}×${r} terminal.")
        return
      }
      handle = made
      applyTheme()
    } else if (c != cols || r != rows) {
      Vt.nativeResize(handle, c, r, cellW, cellH)
    } else return
    cols = c
    rows = r
    grid = Array(r) { emptyList() }
    pull(true)
    events.size(c, r)
    reportViewport()
  }

  private fun fail(message: String) {
    failed = true
    events.failed(message)
  }

  /** After `failed`: try to make the VT again. */
  fun retry() {
    if (!failed) return
    failed = false
    relayout(width, height)
    invalidate()
  }

  fun write(data: String) {
    if (handle == 0L) return
    Vt.nativeWriteBytes(handle, data.toByteArray(Charsets.UTF_8))
    Vt.nativeTakeClipboard(handle)?.let(events::copy)
    pull(false)
  }

  /** A new attachment: the screen starts empty. The history is the page's (`history`). */
  fun reset() {
    if (handle != 0L) Vt.nativeDestroyTerminal(handle)
    handle = 0L
    cols = 0
    rows = 0
    relayout(width, height)
    invalidate()
  }

  /** The `history` command: void (`reset`), drop `removed` rows from the front, append `rows`. The rows on screen stay put. */
  fun applyHistory(reset: Boolean, removed: Int, rows: List<String>, cols: Int, alternate: Boolean) {
    if (reset) {
      historyAnsi.clear()
      history.clear()
      markRow = -1
      destroyScratch()
    } else if (removed > 0) {
      val n = minOf(removed, history.size)
      historyAnsi.subList(0, n).clear()
      history.subList(0, n).clear()
      markRow = if (markRow >= n) markRow - n else -1
    }
    if (cols > 0 && cols != historyCols) {
      historyCols = cols
      destroyScratch()
    }
    this.alternate = alternate
    historyAnsi.addAll(rows)
    history.addAll(renderHistory(rows))
    // Scrolled up, the rows above the screen grew: the reader's rows stay where they are.
    if (offset > 0 && !reset) offset += rows.size
    if (alternate) offset = 0
    offset = offset.coerceIn(0, history.size)
    reportViewport()
    invalidate()
  }

  /** Show history row `row` at the top. */
  fun scrollTo(row: Int) {
    if (alternate || history.isEmpty()) return
    setOffset(history.size - row.coerceIn(0, history.size))
  }

  fun scrollToBottom() = setOffset(0)

  /** Highlight one history row (the search's match); -1 none. */
  fun mark(row: Int) {
    markRow = if (row in 0 until history.size) row else -1
    invalidate()
  }

  private fun setOffset(next: Int) {
    val clamped = next.coerceIn(0, history.size)
    if (clamped == offset) return
    offset = clamped
    reportViewport()
    invalidate()
  }

  private fun reportViewport() {
    val top = history.size - offset
    val atBottom = offset == 0
    if (top == reportedTop && atBottom == reportedBottom) return
    reportedTop = top
    reportedBottom = atBottom
    events.viewport(top, atBottom)
  }

  private fun destroyScratch() {
    if (scratch != 0L) Vt.nativeDestroyTerminal(scratch)
    scratch = 0L
  }

  /** History rows as runs, a page at a time through the scratch VT (styling carries over from row to row, as tmux captures it). */
  private fun renderHistory(lines: List<String>): List<List<Run>> {
    if (lines.isEmpty()) return emptyList()
    if (scratch == 0L) {
      scratch = try { Vt.nativeCreateTerminal(historyCols.coerceIn(2, 1000), PAGE) } catch (e: Throwable) { 0L }
      if (scratch == 0L) return lines.map { emptyList<Run>() }
      themeVt(scratch)
    }
    val out = ArrayList<List<Run>>(lines.size)
    var at = 0
    while (at < lines.size) {
      val page = lines.subList(at, minOf(at + PAGE, lines.size))
      // Clear and home without resetting the styling the page before left.
      Vt.nativeWriteBytes(scratch, ("\u001b[2J\u001b[H" + page.joinToString("\r\n")).toByteArray(Charsets.UTF_8))
      val rendered = arrayOfNulls<List<Run>>(PAGE)
      decode(Vt.nativeRowRuns(scratch, true)) { row, runs -> if (row < PAGE) rendered[row] = runs }
      for (i in page.indices) out.add(rendered[i] ?: emptyList())
      at += PAGE
    }
    return out
  }

  /** Reads nativeRowRuns' result, row by row; its meta, or null. */
  private fun decode(res: Array<Any>?, row: (Int, List<Run>) -> Unit): IntArray? {
    if (res == null) return null
    val m = res[0] as IntArray
    @Suppress("UNCHECKED_CAST") val t = res[1] as Array<String>
    var p = 7
    var ti = 0
    repeat(m[6]) {
      val index = m[p]; val n = m[p + 1]; p += 2
      val list = ArrayList<Run>(n)
      repeat(n) { list.add(Run(m[p], m[p + 1], m[p + 2], m[p + 3], m[p + 4], t[ti++])); p += 5 }
      row(index, list)
    }
    return m
  }

  private fun pull(full: Boolean) {
    val res = Vt.nativeRowRuns(handle, full) ?: return
    val m = res[0] as IntArray
    if (m[0] != grid.size) grid = Array(m[0]) { i -> grid.getOrNull(i) ?: emptyList() }
    rows = m[0]; cols = m[1]; cursorCol = m[2]; cursorRow = m[3]; cursorVisible = m[4] == 1
    decode(res) { row, runs -> if (row < grid.size) grid[row] = runs }
    invalidate()
  }

  /** What screen row `r` shows: a history row (its index) or a live row (`index - history.size`). */
  private fun rowAt(r: Int): Int = history.size - offset + r

  private fun runsAt(r: Int): List<Run> {
    val i = rowAt(r)
    return if (i < history.size) history.getOrNull(i) ?: emptyList() else grid.getOrNull(i - history.size) ?: emptyList()
  }

  override fun onDraw(canvas: Canvas) {
    canvas.drawColor(background)
    for (r in 0 until rows) {
      val y = r * cellH
      val i = rowAt(r)
      if (r == selRow || (i < history.size && i == markRow)) {
        fill.color = selectionColor; canvas.drawRect(0f, y, width.toFloat(), y + cellH, fill)
      }
      for (run in runsAt(r)) {
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
    // The cursor moves down with the live screen, and leaves with it.
    val screenRow = cursorRow + offset
    if (!cursorVisible || screenRow >= rows) return
    val x = cursorCol * cellW
    val y = screenRow * cellH
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

  /**
   * A row's text, the gaps between its runs (blank cells) as spaces, and the
   * UTF-16 index of cell `col` in it: -1 past its end; a wide character or a
   * grapheme maps to its start.
   */
  private fun rowText(runs: List<Run>, col: Int): Pair<String, Int> {
    val sb = StringBuilder()
    var at = 0
    var index = -1
    for (run in runs) {
      if (run.col > at) {
        if (col >= at && col < run.col) index = sb.length + (col - at)
        repeat(run.col - at) { sb.append(' ') }
      }
      if (col >= run.col && col < run.col + run.cells) index = if (run.cells == run.text.length) sb.length + (col - run.col) else sb.length
      sb.append(run.text)
      at = run.col + run.cells
    }
    return sb.toString() to index
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

  // Touch: a tap names the cell and its text; a drag scrolls the history; a long press copies a line.
  private val gestures = GestureDetector(context, object : GestureDetector.SimpleOnGestureListener() {
    override fun onDown(e: MotionEvent): Boolean { scrollRemainder = 0f; return true }

    override fun onSingleTapUp(e: MotionEvent): Boolean {
      if (cellW <= 0f || cellH <= 0f || rows <= 0) return true
      val col = (e.x / cellW).toInt().coerceIn(0, maxOf(cols - 1, 0))
      val r = (e.y / cellH).toInt().coerceIn(0, rows - 1)
      val i = rowAt(r)
      val (line, index) = rowText(runsAt(r), col)
      events.tapCell(col, if (i < history.size) -1 else i - history.size, line, index)
      return true
    }

    override fun onScroll(e1: MotionEvent?, e2: MotionEvent, dx: Float, dy: Float): Boolean {
      // A drag down (dy < 0) moves back in time: positive lines.
      scrollRemainder += -dy / cellH
      val lines = scrollRemainder.toInt()
      if (lines == 0) return true
      scrollRemainder -= lines
      if (!alternate && (history.isNotEmpty() || offset > 0)) setOffset(offset + lines)
      else events.scroll(lines)
      return true
    }

    override fun onLongPress(e: MotionEvent) {
      selRow = (e.y / cellH).toInt()
      invalidate()
      startActionMode(object : ActionMode.Callback2() {
        override fun onCreateActionMode(m: ActionMode, menu: Menu): Boolean { menu.add(0, 1, 0, android.R.string.copy); return true }
        override fun onPrepareActionMode(m: ActionMode, menu: Menu) = false
        override fun onActionItemClicked(m: ActionMode, item: MenuItem): Boolean {
          val line = if (selRow in 0 until rows) rowText(runsAt(selRow), 0).first.trimEnd() else ""
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
    destroyScratch()
  }

  private companion object {
    /** Rows the scratch VT renders at a time. */
    const val PAGE = 64
  }
}
