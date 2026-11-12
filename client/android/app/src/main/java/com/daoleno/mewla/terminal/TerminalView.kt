package com.daoleno.mewla.terminal

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.Path
import android.graphics.Rect
import android.graphics.Typeface
import android.graphics.drawable.Drawable
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
import android.widget.OverScroller
import kotlin.math.abs
import kotlin.math.ceil
import kotlin.math.floor
import kotlin.math.max
import kotlin.math.min
import kotlin.math.roundToInt

/**
 * A terminal drawn on a Canvas from libghostty-vt's cells (client/native/terminal), in the
 * bundled Maple Mono NF on an exact grid of whole-pixel cells: each cell's glyph at its column,
 * CJK and emoji centred in their two cells, box drawing, blocks, braille and Powerline drawn
 * from TerminalGlyphs so lines meet, every attribute (faint, italic, five underline styles in
 * their own color, strikethrough, overline, inverse) and the program's cursor shape.
 *
 * The system keyboard types into it (composition drawn at the cursor, only committed text
 * reported). Above the live screen it draws the history the page hands it (`history`, styled rows
 * rendered by a second VT) and a drag scrolls through it pixel by pixel, with fling; with no history
 * (or on the alternate screen) the drag is reported (the computer's tmux copy mode moves). A tap
 * reports the cell and the text under it (the page opens links and raises the keyboard); a long press
 * selects the word under the finger, with handles to stretch it over any rows, history included,
 * and offers Copy, Paste and Select all. Text a program copies (OSC 52) is reported.
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

  /** A run as the packer gives it (TerminalVt.h): its text is chars[start until start + len). */
  private class Run(val col: Int, val cells: Int, val fg: Int, val bg: Int, val flags: Int, val ul: Int, val chars: CharArray, val start: Int, val len: Int) {
    /** One UTF-16 unit per cell (else a single wide cell, grapheme or astral code point). */
    val plain get() = len == cells
  }

  private class Line(val runs: List<Run>, val wrapped: Boolean)

  private var handle = 0L
  private var failed = false
  private var rows = 0
  private var cols = 0
  private var grid: Array<Line> = emptyArray()
  private var cursorCol = 0
  private var cursorRow = 0
  private var cursorVisible = true
  private var cursorStyle = 1
  private var cursorBlinks = false
  private var blinkOn = true
  private var composing = ""

  // The font: the cell is whole pixels, and the text size is set so one advance is exactly one cell.
  private val paint = textPaint(Fonts.regular(context))
  private val boldPaint = textPaint(Fonts.bold(context))
  private val fill = Paint()
  private val shape = Paint(Paint.ANTI_ALIAS_FLAG)
  private val path = Path()
  private var fontPx = 0f
  private var cellW = 0
  private var cellH = 0
  private var baseline = 0
  private var thickness = 1
  /** Each BMP character's advance per paint, NaN until measured: a character whose advance is not one cell is drawn on its own, centred. */
  private val advances = arrayOf(FloatArray(0x10000) { Float.NaN }, FloatArray(0x10000) { Float.NaN })
  /** TerminalGlyphs' steps per code point, for the current cell size. */
  private val glyphOps = HashMap<Int, FloatArray>()

  private var background = Color.parseColor("#141210")
  private var foreground = Color.parseColor("#F4F0EA")
  private var cursorColor = Color.parseColor("#F4F0EA")
  private var selectionColor = Color.parseColor("#553D7EFF")
  /** [foreground, background, cursor] and the 256-color palette, 0xRRGGBB, for the VTs. */
  private var theme: Pair<IntArray, IntArray>? = null

  // The history above the live screen: its styled rows as the page sent them (drawn again on a
  // theme change) and as lines, oldest first. Content line i is history[i] below H, the live row i - H above.
  private val historyAnsi = ArrayList<String>()
  private val history = ArrayList<Line>()
  private var historyCols = 0
  private var alternate = false
  /** The VT that renders history rows, `PAGE` rows tall at the history's width; it keeps styling from one row to the next. */
  private var scratch = 0L
  /** Pixels the content is scrolled down from the live screen (0 = live screen at the bottom). */
  private var scrollPx = 0f
  private val scroller = OverScroller(context)
  private var markRow = -1
  private var reportedTop = -1
  private var reportedBottom = true

  // Selection, in content lines (history then live rows) and columns, inclusive; null none.
  private var selFrom: IntArray? = null
  private var selTo: IntArray? = null
  private var actionMode: ActionMode? = null
  private val handleLeft: Drawable? = themeDrawable(android.R.attr.textSelectHandleLeft)
  private val handleRight: Drawable? = themeDrawable(android.R.attr.textSelectHandleRight)
  /** The handle being dragged: 0 none, 1 start, 2 end. */
  private var dragging = 0

  private val blink = object : Runnable {
    override fun run() {
      blinkOn = !blinkOn
      invalidateCursor()
      postDelayed(this, BLINK_MS)
    }
  }

  init {
    isFocusable = true
    isFocusableInTouchMode = true
    setFontSize(8f)
  }

  private fun textPaint(face: Typeface) = Paint(Paint.ANTI_ALIAS_FLAG or Paint.SUBPIXEL_TEXT_FLAG).apply {
    typeface = face
    // The bundled face has no ligatures; a fallback face may.
    fontFeatureSettings = "'liga' 0, 'calt' 0, 'dlig' 0"
  }

  private fun themeDrawable(attr: Int): Drawable? = try {
    val a = context.obtainStyledAttributes(intArrayOf(attr))
    val d = a.getDrawable(0)
    a.recycle()
    d
  } catch (e: Exception) { null }

  /** Text size in dp (the shipped app's 8 CSS px is the default): the grid follows from it. */
  fun setFontSize(size: Float) {
    fontPx = size * resources.displayMetrics.density
    paint.textSize = fontPx
    val advance = paint.measureText("M")
    cellW = max(1, advance.roundToInt())
    paint.textSize = fontPx * cellW / advance
    boldPaint.textSize = paint.textSize
    val m = paint.fontMetrics
    val textH = m.descent - m.ascent
    cellH = max(1, textH.roundToInt())
    baseline = ((cellH - textH) / 2 - m.ascent).roundToInt()
    thickness = try { Vt.nativeGlyphThickness(cellH) } catch (e: Throwable) { max(1, (cellH / 14f).roundToInt()) }
    advances.forEach { it.fill(Float.NaN) }
    glyphOps.clear()
    relayout(width, height)
    invalidate()
  }

  /** Theme colors are CSS hex (`#rrggbb` or `#rrggbbaa`, what the app's `token()` gives); Android reads 8 digits as `#aarrggbb`. */
  private fun cssColor(value: String): Int {
    val hex = value.trim()
    return if (hex.length == 9 && hex.startsWith("#")) Color.parseColor("#" + hex.substring(7, 9) + hex.substring(1, 7)) else Color.parseColor(hex)
  }

  fun setTheme(background: String, foreground: String, cursor: String, selection: String, palette: List<String>) {
    this.background = cssColor(background) or 0xFF000000.toInt()
    this.foreground = cssColor(foreground)
    cursorColor = cssColor(cursor)
    selectionColor = cssColor(selection)
    theme = intArrayOf(this.foreground, this.background, cursorColor) to palette256(palette.map { cssColor(it) and 0xFFFFFF })
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
    if (w <= 0 || h <= 0 || cellW <= 0 || failed) return
    val c = max(2, w / cellW)
    val r = max(2, h / cellH)
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
      Vt.nativeResize(handle, c, r, cellW.toFloat(), cellH.toFloat())
    } else return
    cols = c
    rows = r
    grid = Array(r) { EMPTY }
    pull(true)
    events.size(c, r)
    clampScroll()
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
    restartBlink()
  }

  /** A new attachment: the screen starts empty. The history is the page's (`history`). */
  fun reset() {
    if (handle != 0L) Vt.nativeDestroyTerminal(handle)
    handle = 0L
    cols = 0
    rows = 0
    clearSelection()
    relayout(width, height)
    invalidate()
  }

  /** The `history` command: void (`reset`), drop `removed` rows from the front, append `rows`. The rows on screen stay put. */
  fun applyHistory(reset: Boolean, removed: Int, rows: List<String>, cols: Int, alternate: Boolean) {
    if (reset) {
      historyAnsi.clear()
      history.clear()
      markRow = -1
      clearSelection()
      destroyScratch()
    } else if (removed > 0) {
      val n = min(removed, history.size)
      historyAnsi.subList(0, n).clear()
      history.subList(0, n).clear()
      markRow = if (markRow >= n) markRow - n else -1
      shiftSelection(n)
    }
    if (cols > 0 && cols != historyCols) {
      historyCols = cols
      destroyScratch()
    }
    this.alternate = alternate
    historyAnsi.addAll(rows)
    history.addAll(renderHistory(rows))
    // Scrolled up, the rows above the screen grew: the reader's rows stay where they are.
    if (scrollPx > 0 && !reset) scrollPx += rows.size * cellH
    if (alternate) scrollPx = 0f
    clampScroll()
    reportViewport()
    invalidate()
  }

  /** Show history row `row` at the top. */
  fun scrollTo(row: Int) {
    if (alternate || history.isEmpty()) return
    scroller.forceFinished(true)
    setScroll((history.size - row.coerceIn(0, history.size)).toFloat() * cellH)
  }

  fun scrollToBottom() {
    scroller.forceFinished(true)
    setScroll(0f)
  }

  /** Highlight one history row (the search's match); -1 none. */
  fun mark(row: Int) {
    markRow = if (row in 0 until history.size) row else -1
    invalidate()
  }

  private fun maxScroll() = if (alternate) 0f else (history.size * cellH).toFloat()

  private fun clampScroll() {
    scrollPx = scrollPx.coerceIn(0f, maxScroll())
  }

  private fun setScroll(next: Float) {
    val clamped = next.coerceIn(0f, maxScroll())
    if (clamped == scrollPx) return
    scrollPx = clamped
    reportViewport()
    invalidate()
    actionMode?.invalidateContentRect()
  }

  private fun reportViewport() {
    val up = if (cellH > 0) ceil(scrollPx / cellH - 0.01f).toInt() else 0
    val top = history.size - up
    val atBottom = scrollPx < 0.5f
    if (top == reportedTop && atBottom == reportedBottom) return
    reportedTop = top
    reportedBottom = atBottom
    events.viewport(top, atBottom)
  }

  override fun computeScroll() {
    if (scroller.computeScrollOffset()) {
      setScroll(scroller.currY.toFloat())
      postInvalidateOnAnimation()
    }
  }

  private fun destroyScratch() {
    if (scratch != 0L) Vt.nativeDestroyTerminal(scratch)
    scratch = 0L
  }

  /** History rows as lines, a page at a time through the scratch VT (styling carries over from row to row, as tmux captures it). */
  private fun renderHistory(lines: List<String>): List<Line> {
    if (lines.isEmpty()) return emptyList()
    if (scratch == 0L) {
      scratch = try { Vt.nativeCreateTerminal(historyCols.coerceIn(2, 1000), PAGE) } catch (e: Throwable) { 0L }
      if (scratch == 0L) return lines.map { EMPTY }
      themeVt(scratch)
    }
    val out = ArrayList<Line>(lines.size)
    var at = 0
    while (at < lines.size) {
      val page = lines.subList(at, min(at + PAGE, lines.size))
      // Clear and home without resetting the styling the page before left.
      Vt.nativeWriteBytes(scratch, ("\u001b[2J\u001b[H" + page.joinToString("\r\n")).toByteArray(Charsets.UTF_8))
      val rendered = arrayOfNulls<Line>(PAGE)
      decode(Vt.nativeFrame(scratch, true)) { row, line -> if (row < PAGE) rendered[row] = line }
      // A history row is a whole line (the computer joined wrapped rows).
      for (i in page.indices) out.add(rendered[i]?.let { Line(it.runs, false) } ?: EMPTY)
      at += PAGE
    }
    return out
  }

  /** Reads nativeFrame's result row by row; its meta, or null. */
  private fun decode(res: Array<Any>?, row: (Int, Line) -> Unit): IntArray? {
    if (res == null) return null
    val m = res[0] as IntArray
    val text = res[1] as CharArray
    var p = Vt.HEADER
    repeat(m[6]) {
      val index = m[p]
      val n = m[p + 1]
      val wrapped = m[p + 2] == 1
      p += Vt.ROW
      val runs = ArrayList<Run>(n)
      repeat(n) {
        runs.add(Run(m[p], m[p + 1], m[p + 2], m[p + 3], m[p + 4], m[p + 5], text, m[p + 6], m[p + 7]))
        p += Vt.RUN
      }
      row(index, Line(runs, wrapped))
    }
    return m
  }

  private fun pull(full: Boolean) {
    val res = Vt.nativeFrame(handle, full) ?: return
    val m = res[0] as IntArray
    if (m[0] != grid.size) grid = Array(m[0]) { i -> grid.getOrNull(i) ?: EMPTY }
    rows = m[0]; cols = m[1]; cursorCol = m[2]; cursorRow = m[3]; cursorVisible = m[4] == 1
    cursorStyle = m[7]
    val blinks = m[8] == 1
    if (blinks != cursorBlinks) {
      cursorBlinks = blinks
      restartBlink()
    }
    decode(res) { row, line -> if (row < grid.size) grid[row] = line }
    invalidate()
  }

  // MARK: Content lines

  private fun lineCount() = history.size + rows

  private fun lineAt(i: Int): Line = if (i < history.size) history.getOrNull(i) ?: EMPTY else grid.getOrNull(i - history.size) ?: EMPTY

  /** The y of content line `i` (whole pixels). */
  private fun lineY(i: Int): Int = (i - history.size) * cellH + scrollPx.roundToInt()

  /** The content line under y (may be past either end). */
  private fun lineUnder(y: Float): Int = history.size + floor((y - scrollPx.roundToInt()) / cellH).toInt()

  private fun colUnder(x: Float): Int = (x / cellW).toInt().coerceIn(0, max(cols - 1, 0))

  // MARK: Drawing

  override fun onDraw(canvas: Canvas) {
    canvas.drawColor(background)
    if (cellH <= 0) return
    val first = max(0, lineUnder(0f))
    val last = min(lineCount() - 1, lineUnder(height.toFloat() - 1))
    val from = selFrom
    val to = selTo
    for (i in first..last) {
      val y = lineY(i).toFloat()
      val line = lineAt(i)
      for (run in line.runs) {
        if (run.bg != 0) {
          fill.color = run.bg
          canvas.drawRect((run.col * cellW).toFloat(), y, ((run.col + run.cells) * cellW).toFloat(), y + cellH, fill)
        }
      }
      if (i < history.size && i == markRow) {
        fill.color = selectionColor
        canvas.drawRect(0f, y, width.toFloat(), y + cellH, fill)
      }
      if (from != null && to != null && i >= from[0] && i <= to[0]) {
        val c0 = if (i == from[0]) from[1] else 0
        val c1 = if (i == to[0]) to[1] else cols - 1
        if (c1 >= c0) {
          fill.color = selectionColor
          canvas.drawRect((c0 * cellW).toFloat(), y, ((c1 + 1) * cellW).toFloat(), y + cellH, fill)
        }
      }
      for (run in line.runs) drawRun(canvas, run, y, run.fg)
    }
    drawCursor(canvas)
    drawHandles(canvas)
  }

  private fun drawRun(canvas: Canvas, run: Run, y: Float, color: Int) {
    val p = if (run.flags and Vt.BOLD != 0) boldPaint else paint
    p.color = color
    p.textSkewX = if (run.flags and Vt.ITALIC != 0) -0.2f else 0f
    val x0 = run.col * cellW
    val base = y + baseline
    if (run.plain) {
      val table = advances[if (p === boldPaint) 1 else 0]
      var seg = -1
      var k = 0
      while (k <= run.len) {
        val c = if (k < run.len) run.chars[run.start + k] else ' '
        val special = k == run.len || c == ' ' || isDrawn(c.code) || !fits(table, p, c)
        if (special && seg >= 0) {
          canvas.drawText(run.chars, run.start + seg, k - seg, (x0 + seg * cellW).toFloat(), base, p)
          seg = -1
        }
        if (k < run.len && c != ' ') {
          when {
            isDrawn(c.code) -> drawGlyph(canvas, c.code, x0 + k * cellW, y, color)
            special -> drawFitted(canvas, p, run.chars, run.start + k, 1, x0 + k * cellW, y, 1)
            seg < 0 -> seg = k
          }
        }
        k++
      }
    } else if (run.len > 0) {
      val cp = Character.codePointAt(run.chars, run.start)
      if (Character.charCount(cp) == run.len && isDrawn(cp)) drawGlyph(canvas, cp, x0, y, color)
      else drawFitted(canvas, p, run.chars, run.start, run.len, x0, y, run.cells)
    }
    decorate(canvas, run, x0.toFloat(), y, color)
  }

  private fun fits(table: FloatArray, p: Paint, c: Char): Boolean {
    var a = table[c.code]
    if (a.isNaN()) {
      a = p.measureText(charArrayOf(c), 0, 1)
      table[c.code] = a
    }
    return abs(a - cellW) < 0.05f
  }

  /** A glyph that is not one cell wide (CJK, emoji, a symbol from a fallback face): centred in its `cells`, scaled down to fit. */
  private fun drawFitted(canvas: Canvas, p: Paint, chars: CharArray, start: Int, len: Int, x0: Int, y: Float, cells: Int) {
    val box = (cells * cellW).toFloat()
    val w = p.measureText(chars, start, len)
    if (w <= box + 0.5f || w <= 0f) {
      canvas.drawText(chars, start, len, x0 + (box - w) / 2, y + baseline, p)
      return
    }
    val s = box / w
    canvas.save()
    canvas.scale(s, s, x0 + box / 2, y + cellH / 2f)
    canvas.drawText(chars, start, len, x0 + (box - w) / 2, y + baseline, p)
    canvas.restore()
  }

  private fun isDrawn(cp: Int) = (cp in 0x2500..0x259F) || (cp in 0x2800..0x28FF) || (cp in 0x23F4..0x23FA) || (cp in 0xE0B0..0xE0BF)

  /** A box-drawing, block, braille or Powerline glyph from TerminalGlyphs, exactly in its cell. */
  private fun drawGlyph(canvas: Canvas, cp: Int, x0: Int, y: Float, color: Int) {
    val ops = glyphOps.getOrPut(cp) { try { Vt.nativeGlyphOps(cp, cellW, cellH) } catch (e: Throwable) { FloatArray(0) } }
    var i = 0
    while (i + 12 <= ops.size) {
      val kind = ops[i].toInt()
      val points = ops[i + 1].toInt()
      val alpha = ops[i + 2]
      val stroke = ops[i + 3]
      val p = i + 4
      val x = x0.toFloat()
      shape.color = color
      if (alpha < 1f) shape.alpha = (Color.alpha(color) * alpha).roundToInt()
      when (kind) {
        1 -> { fill.color = shape.color; canvas.drawRect(x + ops[p], y + ops[p + 1], x + ops[p + 2], y + ops[p + 3], fill) }
        2 -> {
          path.reset()
          path.moveTo(x + ops[p], y + ops[p + 1])
          for (k in 1 until points) path.lineTo(x + ops[p + 2 * k], y + ops[p + 2 * k + 1])
          path.close()
          shape.style = Paint.Style.FILL
          canvas.drawPath(path, shape)
        }
        3 -> {
          shape.style = Paint.Style.STROKE
          shape.strokeWidth = stroke
          shape.strokeCap = Paint.Cap.BUTT
          canvas.drawLine(x + ops[p], y + ops[p + 1], x + ops[p + 2], y + ops[p + 3], shape)
        }
        4, 5 -> {
          path.reset()
          path.moveTo(x + ops[p], y + ops[p + 1])
          path.cubicTo(x + ops[p + 2], y + ops[p + 3], x + ops[p + 4], y + ops[p + 5], x + ops[p + 6], y + ops[p + 7])
          if (kind == 5) { path.close(); shape.style = Paint.Style.FILL } else {
            shape.style = Paint.Style.STROKE
            shape.strokeWidth = stroke
            shape.strokeCap = Paint.Cap.BUTT
          }
          canvas.drawPath(path, shape)
        }
        6 -> { shape.style = Paint.Style.FILL; canvas.drawOval(x + ops[p], y + ops[p + 1], x + ops[p + 2], y + ops[p + 3], shape) }
      }
      i += 12
    }
    shape.alpha = 255
  }

  /** Underline (its style and color), strikethrough and overline across the run's cells, one line weight. */
  private fun decorate(canvas: Canvas, run: Run, x0: Float, y: Float, color: Int) {
    val x1 = x0 + run.cells * cellW
    val t = thickness.toFloat()
    val style = (run.flags shr Vt.UNDERLINE_SHIFT) and 7
    if (style != 0) {
      fill.color = if (run.ul != 0) run.ul else color
      shape.color = fill.color
      // Below the baseline, kept inside the cell with room for a double or curly line.
      val under = min(y + baseline + max(t, (fontPx * 0.12f).roundToInt().toFloat()), y + cellH - 3 * t)
      when (style) {
        2 -> { canvas.drawRect(x0, under, x1, under + t, fill); canvas.drawRect(x0, under + 2 * t, x1, under + 3 * t, fill) }
        3 -> {
          val amp = t
          path.reset()
          path.moveTo(x0, under + amp)
          var x = x0
          val half = cellW / 2f
          var up = true
          while (x < x1) {
            path.quadTo(x + half / 2, if (up) under - amp else under + 3 * amp, min(x + half, x1), under + amp)
            x += half
            up = !up
          }
          shape.style = Paint.Style.STROKE
          shape.strokeWidth = t
          canvas.drawPath(path, shape)
        }
        4 -> { var x = x0; while (x < x1) { canvas.drawRect(x, under, min(x + t, x1), under + t, fill); x += 2 * t } }
        5 -> {
          val dash = max(2 * t, (cellW * 0.4f).roundToInt().toFloat())
          var x = x0
          while (x < x1) { canvas.drawRect(x, under, min(x + dash, x1), under + t, fill); x += 2 * dash }
        }
        else -> canvas.drawRect(x0, under, x1, under + t, fill)
      }
    }
    if (run.flags and Vt.STRIKE != 0) {
      fill.color = color
      val mid = (y + baseline - fontPx * 0.3f).roundToInt().toFloat()
      canvas.drawRect(x0, mid, x1, mid + t, fill)
    }
    if (run.flags and Vt.OVERLINE != 0) {
      fill.color = color
      canvas.drawRect(x0, y, x1, y + t, fill)
    }
  }

  private fun drawCursor(canvas: Canvas) {
    if (!cursorVisible || handle == 0L || cursorRow >= rows) return
    val y = lineY(history.size + cursorRow).toFloat()
    if (y >= height || y + cellH <= 0) return
    val x = (cursorCol * cellW).toFloat()
    if (composing.isNotEmpty()) {
      // The composition (IME) at the cursor, underlined; only the committed text is sent.
      val w = paint.measureText(composing)
      fill.color = background
      canvas.drawRect(x, y, x + w, y + cellH, fill)
      paint.color = foreground
      paint.textSkewX = 0f
      canvas.drawText(composing, x, y + baseline, paint)
      fill.color = foreground
      canvas.drawRect(x, y + cellH - thickness, x + w, y + cellH.toFloat(), fill)
      return
    }
    if (cursorBlinks && hasFocus() && !blinkOn) return
    // The cell under the cursor (a wide character takes its two cells).
    val run = grid.getOrNull(cursorRow)?.runs?.firstOrNull { cursorCol >= it.col && cursorCol < it.col + it.cells }
    val wide = run != null && !run.plain && run.cells == 2
    val cx = if (wide) (run!!.col * cellW).toFloat() else x
    val w = if (wide) 2f * cellW else cellW.toFloat()
    fill.color = cursorColor
    val t = thickness.toFloat()
    when {
      !hasFocus() || cursorStyle == 3 -> {
        // Unfocused (no keyboard): a hollow block, the text stays readable.
        canvas.drawRect(cx, y, cx + w, y + t, fill)
        canvas.drawRect(cx, y + cellH - t, cx + w, y + cellH, fill)
        canvas.drawRect(cx, y, cx + t, y + cellH, fill)
        canvas.drawRect(cx + w - t, y, cx + w, y + cellH, fill)
      }
      cursorStyle == 0 -> canvas.drawRect(cx, y, cx + max(t, (cellW / 8f).roundToInt().toFloat()), y + cellH, fill)
      cursorStyle == 2 -> canvas.drawRect(cx, y + cellH - 2 * t, cx + w, y + cellH, fill)
      else -> {
        canvas.drawRect(cx, y, cx + w, y + cellH, fill)
        // The character under a block cursor, in the background color.
        if (run != null) {
          canvas.save()
          canvas.clipRect(cx, y, cx + w, y + cellH)
          drawRun(canvas, run, y, background)
          canvas.restore()
        }
      }
    }
  }

  private fun invalidateCursor() = invalidate()

  private fun restartBlink() {
    removeCallbacks(blink)
    blinkOn = true
    if (cursorBlinks && isAttachedToWindow) postDelayed(blink, BLINK_MS)
  }

  override fun onAttachedToWindow() {
    super.onAttachedToWindow()
    restartBlink()
  }

  override fun onDetachedFromWindow() {
    removeCallbacks(blink)
    super.onDetachedFromWindow()
  }

  override fun onFocusChanged(gained: Boolean, direction: Int, previous: Rect?) {
    super.onFocusChanged(gained, direction, previous)
    restartBlink()
    invalidate()
  }

  // MARK: Text of the content

  /**
   * A line's text, blank cells as spaces, and the UTF-16 index of cell `col` in it: -1 past its
   * end; a wide character or a grapheme maps to its start.
   */
  private fun lineText(line: Line, col: Int): Pair<String, Int> {
    val sb = StringBuilder()
    var at = 0
    var index = -1
    for (run in line.runs) {
      if (run.col > at) {
        if (col >= at && col < run.col) index = sb.length + (col - at)
        repeat(run.col - at) { sb.append(' ') }
      }
      if (col >= run.col && col < run.col + run.cells) index = if (run.plain) sb.length + (col - run.col) else sb.length
      sb.append(run.chars, run.start, run.len)
      at = run.col + run.cells
    }
    return sb.toString() to index
  }

  /** The text of cells c0..c1 of a line (blanks as spaces; a wide character once, from its first cell). */
  private fun cellsText(line: Line, c0: Int, c1: Int): String {
    val sb = StringBuilder()
    var at = c0
    for (run in line.runs) {
      if (run.col + run.cells <= c0) continue
      if (run.col > c1) break
      while (at < run.col && at <= c1) { sb.append(' '); at++ }
      if (run.plain) {
        val a = max(c0, run.col) - run.col
        val b = min(c1, run.col + run.cells - 1) - run.col
        if (b >= a) sb.append(run.chars, run.start + a, b - a + 1)
      } else if (run.col >= c0) {
        sb.append(run.chars, run.start, run.len)
      }
      at = run.col + run.cells
    }
    return sb.toString()
  }

  /** The selected text: rows joined by newlines, except where a row wraps onto the next. */
  private fun selectedText(): String {
    val from = selFrom ?: return ""
    val to = selTo ?: return ""
    val sb = StringBuilder()
    for (i in from[0]..to[0]) {
      val line = lineAt(i)
      val c0 = if (i == from[0]) from[1] else 0
      val c1 = if (i == to[0]) to[1] else cols - 1
      val part = cellsText(line, c0, c1)
      if (line.wrapped && i != to[0]) sb.append(part) else {
        sb.append(part.trimEnd())
        if (i != to[0]) sb.append('\n')
      }
    }
    return sb.toString()
  }

  // MARK: Selection

  private fun hasSelection() = selFrom != null && selTo != null

  private fun clearSelection() {
    selFrom = null
    selTo = null
    dragging = 0
    actionMode?.finish()
    actionMode = null
    invalidate()
  }

  /**
   * History rows removed from the front pull every content line up by `removed`. (Rows appended to
   * the history are the live rows that scrolled off: the selected text keeps its content line.)
   */
  private fun shiftSelection(removed: Int) {
    val from = selFrom ?: return
    val to = selTo ?: return
    from[0] -= removed
    to[0] -= removed
    if (to[0] < 0) clearSelection() else if (from[0] < 0) { from[0] = 0; from[1] = 0 }
  }

  /** The word under (line, col): the run of non-blank cells around it; null on a blank. */
  private fun wordAt(i: Int, col: Int): Pair<Int, Int>? {
    if (col !in 0 until cols) return null
    val blank = BooleanArray(cols) { true }
    for (run in lineAt(i).runs) {
      for (k in 0 until run.cells) {
        val c = run.col + k
        if (c >= cols) break
        blank[c] = run.plain && run.chars[run.start + k] == ' '
      }
    }
    if (blank[col]) return null
    var a = col
    var b = col
    while (a > 0 && !blank[a - 1]) a--
    while (b < cols - 1 && !blank[b + 1]) b++
    return a to b
  }

  private fun select(i: Int, a: Int, b: Int) {
    selFrom = intArrayOf(i, a)
    selTo = intArrayOf(i, b)
    invalidate()
  }

  private fun handleBounds(which: Int): Rect? {
    val from = selFrom ?: return null
    val to = selTo ?: return null
    val d = (if (which == 1) handleLeft else handleRight) ?: return null
    val w = d.intrinsicWidth
    val h = d.intrinsicHeight
    return if (which == 1) {
      val x = from[1] * cellW
      val y = lineY(from[0]) + cellH
      Rect(x - w * 3 / 4, y, x + w / 4, y + h)
    } else {
      val x = (to[1] + 1) * cellW
      val y = lineY(to[0]) + cellH
      Rect(x - w / 4, y, x + w * 3 / 4, y + h)
    }
  }

  private fun drawHandles(canvas: Canvas) {
    if (!hasSelection()) return
    for (which in 1..2) {
      val d = (if (which == 1) handleLeft else handleRight) ?: continue
      val b = handleBounds(which) ?: continue
      if (b.top > height || b.bottom < 0) continue
      d.bounds = b
      d.draw(canvas)
    }
  }

  private fun showMenu() {
    if (actionMode != null) { actionMode?.invalidate(); actionMode?.invalidateContentRect(); return }
    actionMode = startActionMode(object : ActionMode.Callback2() {
      override fun onCreateActionMode(m: ActionMode, menu: Menu): Boolean = true
      override fun onPrepareActionMode(m: ActionMode, menu: Menu): Boolean {
        menu.clear()
        if (hasSelection()) menu.add(0, COPY, 0, android.R.string.copy)
        if (clipboardText() != null) menu.add(0, PASTE, 1, android.R.string.paste)
        menu.add(0, SELECT_ALL, 2, android.R.string.selectAll)
        return true
      }
      override fun onActionItemClicked(m: ActionMode, item: MenuItem): Boolean {
        when (item.itemId) {
          COPY -> {
            context.getSystemService(ClipboardManager::class.java).setPrimaryClip(ClipData.newPlainText("terminal", selectedText()))
            clearSelection()
          }
          PASTE -> {
            clipboardText()?.let(::paste)
            clearSelection()
          }
          SELECT_ALL -> {
            selFrom = intArrayOf(0, 0)
            selTo = intArrayOf(max(lineCount() - 1, 0), max(cols - 1, 0))
            invalidate()
            m.invalidate()
            m.invalidateContentRect()
          }
        }
        return true
      }
      override fun onDestroyActionMode(m: ActionMode) {
        if (actionMode === m) actionMode = null
      }
      override fun onGetContentRect(m: ActionMode, v: View, out: Rect) {
        val from = selFrom
        val to = selTo
        if (from == null || to == null) { out.set(menuAt[0] - 1, menuAt[1] - 1, menuAt[0] + 1, menuAt[1] + 1); return }
        val top = lineY(from[0]).coerceIn(0, height)
        val bottom = (lineY(to[0]) + cellH).coerceIn(0, height)
        out.set(0, top, width, max(bottom, top + 1))
      }
    }, ActionMode.TYPE_FLOATING)
  }

  private val menuAt = IntArray(2)

  private fun clipboardText(): String? {
    val clip = context.getSystemService(ClipboardManager::class.java).primaryClip ?: return null
    if (clip.itemCount == 0) return null
    return clip.getItemAt(0).coerceToText(context)?.toString()?.takeIf { it.isNotEmpty() }
  }

  /** Pasted text to the program, framed for bracketed paste when it asked for it. */
  private fun paste(text: String) {
    if (handle == 0L) return
    val bytes = Vt.nativePaste(handle, text.toByteArray(Charsets.UTF_8)) ?: return
    send(String(bytes, Charsets.UTF_8))
    scrollToBottom()
  }

  // MARK: Keyboard

  // The system keyboard, composition (IME) drawn at the cursor.
  override fun onCheckIsTextEditor() = true
  override fun onCreateInputConnection(out: EditorInfo): InputConnection {
    out.inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS or InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD
    out.imeOptions = EditorInfo.IME_FLAG_NO_FULLSCREEN or EditorInfo.IME_FLAG_NO_EXTRACT_UI
    return object : BaseInputConnection(this, false) {
      override fun commitText(t: CharSequence, pos: Int): Boolean { composing = ""; send(t.toString()); invalidate(); return true }
      override fun setComposingText(t: CharSequence, pos: Int): Boolean { composing = t.toString(); invalidate(); return true }
      override fun finishComposingText(): Boolean { if (composing.isNotEmpty()) { val c = composing; composing = ""; send(c) }; return true }
      override fun deleteSurroundingText(before: Int, after: Int): Boolean { repeat(max(before, 1)) { send("\u007f") }; return true }
      override fun sendKeyEvent(e: KeyEvent): Boolean { if (e.action == KeyEvent.ACTION_DOWN) keyToData(e)?.let(::send); return true }
      override fun performContextMenuAction(id: Int): Boolean {
        if (id == android.R.id.paste) { clipboardText()?.let(::paste); return true }
        return super.performContextMenuAction(id)
      }
    }
  }

  override fun onKeyDown(code: Int, e: KeyEvent): Boolean {
    if (e.isCtrlPressed && e.isShiftPressed && code == KeyEvent.KEYCODE_V) { clipboardText()?.let(::paste); return true }
    if (e.isCtrlPressed && e.isShiftPressed && code == KeyEvent.KEYCODE_C && hasSelection()) {
      context.getSystemService(ClipboardManager::class.java).setPrimaryClip(ClipData.newPlainText("terminal", selectedText()))
      clearSelection()
      return true
    }
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

  private fun send(s: String) {
    restartBlink()
    events.input(s)
  }

  fun showKeyboard() {
    requestFocus()
    context.getSystemService(InputMethodManager::class.java).showSoftInput(this, 0)
  }

  fun hideKeyboard() {
    context.getSystemService(InputMethodManager::class.java).hideSoftInputFromWindow(windowToken, 0)
    clearFocus()
  }

  // MARK: Touch

  // A tap names the cell and its text; a drag scrolls (fling included); a long press selects.
  private var dragRemainder = 0f

  private val gestures = GestureDetector(context, object : GestureDetector.SimpleOnGestureListener() {
    override fun onDown(e: MotionEvent): Boolean {
      dragRemainder = 0f
      scroller.forceFinished(true)
      return true
    }

    override fun onSingleTapUp(e: MotionEvent): Boolean {
      if (hasSelection() || actionMode != null) { clearSelection(); return true }
      if (cellW <= 0 || cellH <= 0 || rows <= 0) return true
      val col = colUnder(e.x)
      val i = lineUnder(e.y).coerceIn(0, max(lineCount() - 1, 0))
      val (text, index) = lineText(lineAt(i), col)
      events.tapCell(col, if (i < history.size) -1 else i - history.size, text, index)
      return true
    }

    override fun onScroll(e1: MotionEvent?, e2: MotionEvent, dx: Float, dy: Float): Boolean {
      if (!alternate && (history.isNotEmpty() || scrollPx > 0)) {
        // A drag down (dy < 0) moves back in time.
        setScroll(scrollPx - dy)
      } else {
        dragRemainder += -dy / cellH
        val lines = dragRemainder.toInt()
        if (lines != 0) {
          dragRemainder -= lines
          events.scroll(lines)
        }
      }
      return true
    }

    override fun onFling(e1: MotionEvent?, e2: MotionEvent, vx: Float, vy: Float): Boolean {
      if (alternate || history.isEmpty()) return false
      scroller.fling(0, scrollPx.roundToInt(), 0, vy.roundToInt(), 0, 0, 0, maxScroll().roundToInt())
      postInvalidateOnAnimation()
      return true
    }

    override fun onLongPress(e: MotionEvent) {
      if (cellW <= 0 || rows <= 0) return
      val i = lineUnder(e.y).coerceIn(0, max(lineCount() - 1, 0))
      val col = colUnder(e.x)
      menuAt[0] = e.x.roundToInt()
      menuAt[1] = e.y.roundToInt()
      val word = wordAt(i, col)
      if (word != null) select(i, word.first, word.second) else { selFrom = null; selTo = null; invalidate() }
      performHapticFeedback(android.view.HapticFeedbackConstants.LONG_PRESS)
      showMenu()
    }
  })

  override fun onTouchEvent(e: MotionEvent): Boolean {
    // A selection handle under the finger is dragged, the menu hidden meanwhile.
    when (e.actionMasked) {
      MotionEvent.ACTION_DOWN -> {
        val slop = (cellH / 2)
        dragging = when {
          handleBounds(2)?.let { Rect(it).apply { inset(-slop, -slop) }.contains(e.x.toInt(), e.y.toInt()) } == true -> 2
          handleBounds(1)?.let { Rect(it).apply { inset(-slop, -slop) }.contains(e.x.toInt(), e.y.toInt()) } == true -> 1
          else -> 0
        }
        if (dragging != 0) {
          actionMode?.hide(60_000L)
          parent?.requestDisallowInterceptTouchEvent(true)
          return true
        }
      }
      MotionEvent.ACTION_MOVE -> if (dragging != 0) {
        moveHandle(e.x, e.y)
        return true
      }
      MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> if (dragging != 0) {
        dragging = 0
        actionMode?.hide(0)
        showMenu()
        return true
      }
    }
    return gestures.onTouchEvent(e) || super.onTouchEvent(e)
  }

  /** A handle follows the finger (a little above it, so the finger does not hide the row); near an edge the content scrolls. */
  private fun moveHandle(x: Float, y: Float) {
    val from = selFrom ?: return
    val to = selTo ?: return
    val fy = y - cellH
    if (fy < cellH) setScroll(scrollPx + cellH / 2f) else if (fy > height - cellH) setScroll(scrollPx - cellH / 2f)
    val i = lineUnder(fy).coerceIn(0, max(lineCount() - 1, 0))
    val col = colUnder(x + if (dragging == 2) -cellW / 2f else cellW / 2f)
    if (dragging == 1) { from[0] = i; from[1] = col } else { to[0] = i; to[1] = col }
    // Dragged past each other: swap ends (and handles).
    if (from[0] > to[0] || (from[0] == to[0] && from[1] > to[1])) {
      val a = from.copyOf()
      from[0] = to[0]; from[1] = to[1]
      to[0] = a[0]; to[1] = a[1]
      dragging = 3 - dragging
    }
    invalidate()
  }

  fun destroy() {
    removeCallbacks(blink)
    actionMode?.finish()
    if (handle != 0L) Vt.nativeDestroyTerminal(handle)
    handle = 0L
    destroyScratch()
  }

  private companion object {
    /** Rows the scratch VT renders at a time. */
    const val PAGE = 64
    const val BLINK_MS = 600L
    const val COPY = 1
    const val PASTE = 2
    const val SELECT_ALL = 3
    val EMPTY = Line(emptyList(), false)
  }
}

/** The terminal's font (client/native/terminal/fonts, in the APK's assets), loaded once. */
internal object Fonts {
  private var regular: Typeface? = null
  private var bold: Typeface? = null

  fun regular(context: Context): Typeface = regular ?: load(context, "MapleMono-NF-Regular.ttf", Typeface.MONOSPACE).also { regular = it }

  fun bold(context: Context): Typeface = bold ?: load(context, "MapleMono-NF-SemiBold.ttf", Typeface.create(Typeface.MONOSPACE, Typeface.BOLD)).also { bold = it }

  private fun load(context: Context, name: String, fallback: Typeface): Typeface = try {
    Typeface.createFromAsset(context.assets, name)
  } catch (e: Exception) {
    fallback
  }
}
