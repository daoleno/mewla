package com.daoleno.mewla.terminal

import android.app.Activity
import android.graphics.Color
import android.os.Bundle
import android.util.Log
import android.view.ViewGroup
import android.widget.FrameLayout
import java.io.File
import kotlin.math.ceil

/**
 * Debug builds only: one render-corpus screen (scripts/terminal-corpus) replayed into the real
 * TerminalView at the screen's grid, for side-by-side shots and the corpus check
 * (scripts/terminal-corpus/android.sh). No daemon, no page: the bytes go straight in.
 *
 *   adb shell am start -n <app>/com.daoleno.mewla.terminal.TerminalReplayActivity \
 *     --es file corpus/sgr.vt --ei cols 52 --ei rows 30 [--ef font 8] [--ez light true] \
 *     [--es history corpus/history.txt --ei top 3000 [--ez retheme true]] \
 *     [--ei sessionCols 160 --ei sessionRows 45 [--ef panX 400]] [--ez group true]
 *
 * `file` is relative to the app's files dir. The view's rectangle on screen is logged
 * ("TerminalReplay: rect x y w h") once the bytes are drawn, and what the view sends ("input ...").
 * `history` is a file of styled rows, one a line, handed to the view as the page hands it history;
 * the view then shows row `top` at the top, `retheme` switches to the other theme after that
 * (voiding the rows drawn so far), and the time the view took is logged ("history ...").
 * `sessionCols`/`sessionRows` tell the view the Session is that size (another viewer holds it), as the
 * page does before a replay: the file is drawn at that grid, fitted into the view's `cols` x `rows`
 * area, and `panX` drags it that many pixels sideways. `group` gives the view the terminal group's padding
 * (14/12/20/12 dp, as the app's Session does) around the same `cols` x `rows` grid.
 */
class TerminalReplayActivity : Activity() {
  private val dark = listOf("#1d1b19", "#e0685a", "#8fbf7f", "#e3b55f", "#7aa2f7", "#c49bd8", "#6fc1c1", "#d8d2c8", "#57514a", "#ff8a7a", "#a8d898", "#f2cb7c", "#9ab6ff", "#d9b2ea", "#8ed8d8", "#f4f0ea")
  private val light = listOf("#161412", "#b3372b", "#3f7a33", "#946112", "#2c55c0", "#80428f", "#21787a", "#d8d2c8", "#57514a", "#d24a3c", "#4f9441", "#b37a1a", "#3d6be0", "#9b56ab", "#2a9396", "#fbfaf7")

  override fun onCreate(savedInstanceState: Bundle?) {
    super.onCreate(savedInstanceState)
    val file = File(filesDir, intent.getStringExtra("file") ?: "corpus/sgr.vt")
    val cols = intent.getIntExtra("cols", 52)
    val rows = intent.getIntExtra("rows", 30)
    val isLight = intent.getBooleanExtra("light", false)
    val root = FrameLayout(this).apply { setBackgroundColor(Color.BLACK) }
    val view = TerminalView(this, object : TerminalView.Events {
      override fun size(cols: Int, rows: Int) { Log.i(TAG, "size $cols $rows") }
      override fun failed(message: String) { Log.e(TAG, "failed $message") }
      // What the view sends the program (typed keys, pastes), escaped, for the selection and paste check.
      override fun input(data: String) { Log.i(TAG, "input " + data.replace("\u001b", "\\e").replace("\r", "\\r").replace("\n", "\\n")) }
      override fun scroll(lines: Int) {}
      override fun viewport(top: Int, atBottom: Boolean) {}
      override fun tapCell(col: Int, row: Int, text: String, index: Int) {}
      override fun copy(text: String) {}
    })
    if (intent.hasExtra("font")) view.setFontSize(intent.getFloatExtra("font", 8f))
    // The grid's pixel size: whole cells, exactly cols x rows, inside the group's padding when asked.
    val cellW = field(view, "cellW")
    val cellH = field(view, "cellH")
    val group = intent.getBooleanExtra("group", false)
    if (group) view.setContentPadding(14f, 12f, 20f, 12f)
    val d = resources.displayMetrics.density
    val padW = if (group) ((12 + 12) * d).toInt() else 0
    val padH = if (group) ((14 + 20) * d).toInt() else 0
    root.addView(view, FrameLayout.LayoutParams(ceil(cols * cellW).toInt() + padW + 2, ceil(rows * cellH).toInt() + padH + 2))
    setContentView(root, ViewGroup.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
    if (isLight) view.setTheme("#fbfaf7", "#161412", "#161412", "#2C55C055", light)
    else view.setTheme("#141210", "#F4F0EA", "#F4F0EA", "#3D7EFF55", dark)
    view.post {
      if (intent.hasExtra("sessionCols")) view.setSessionSize(intent.getIntExtra("sessionCols", cols), intent.getIntExtra("sessionRows", rows))
      view.write(file.readText(Charsets.UTF_8))
      if (intent.hasExtra("panX")) {
        val pan = intent.getFloatExtra("panX", 0f)
        val t = android.os.SystemClock.uptimeMillis()
        val y = view.height / 2f
        val x0 = view.width * 0.8f
        view.dispatchTouchEvent(android.view.MotionEvent.obtain(t, t, android.view.MotionEvent.ACTION_DOWN, x0, y, 0))
        for (k in 1..10) view.dispatchTouchEvent(android.view.MotionEvent.obtain(t, t + k * 16L, android.view.MotionEvent.ACTION_MOVE, x0 - pan * k / 10, y, 0))
        view.dispatchTouchEvent(android.view.MotionEvent.obtain(t, t + 200L, android.view.MotionEvent.ACTION_UP, x0 - pan, y, 0))
      }
      intent.getStringExtra("history")?.let { name ->
        val rows = File(filesDir, name).readLines(Charsets.UTF_8)
        val t0 = System.nanoTime()
        view.applyHistory(true, 0, rows, cols, false)
        val t1 = System.nanoTime()
        view.scrollTo(intent.getIntExtra("top", 0))
        // Drawing the rows in sight draws their pages first.
        val bitmap = android.graphics.Bitmap.createBitmap(view.width, view.height, android.graphics.Bitmap.Config.ARGB_8888)
        val t2 = System.nanoTime()
        view.draw(android.graphics.Canvas(bitmap))
        val t3 = System.nanoTime()
        if (intent.getBooleanExtra("retheme", false)) {
          if (isLight) view.setTheme("#141210", "#F4F0EA", "#F4F0EA", "#3D7EFF55", dark)
          else view.setTheme("#fbfaf7", "#161412", "#161412", "#2C55C055", light)
        }
        val t4 = System.nanoTime()
        Log.i(TAG, "history ${rows.size} rows: applyHistory %.2f ms, first draw at row %d %.2f ms, theme %.2f ms".format(
          (t1 - t0) / 1e6, intent.getIntExtra("top", 0), (t3 - t2) / 1e6, (t4 - t3) / 1e6))
        bitmap.recycle()
      }
      view.postDelayed({
        val at = IntArray(2)
        view.getLocationOnScreen(at)
        Log.i(TAG, "rect ${at[0]} ${at[1]} ${view.width} ${view.height} cell $cellW $cellH")
      }, 300)
    }
  }

  private fun field(view: TerminalView, name: String): Float =
    TerminalView::class.java.getDeclaredField(name).apply { isAccessible = true }.getFloat(view)

  private companion object {
    const val TAG = "TerminalReplay"
  }
}
