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
 *     --es file corpus/sgr.vt --ei cols 52 --ei rows 30 [--ef font 8] [--ez light true]
 *
 * `file` is relative to the app's files dir. The view's rectangle on screen is logged
 * ("TerminalReplay: rect x y w h") once the bytes are drawn.
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
      override fun input(data: String) {}
      override fun scroll(lines: Int) {}
      override fun viewport(top: Int, atBottom: Boolean) {}
      override fun tapCell(col: Int, row: Int, text: String, index: Int) {}
      override fun copy(text: String) {}
    })
    if (intent.hasExtra("font")) view.setFontSize(intent.getFloatExtra("font", 8f))
    // The grid's pixel size: whole cells, exactly cols x rows.
    val cellW = field(view, "cellW")
    val cellH = field(view, "cellH")
    root.addView(view, FrameLayout.LayoutParams(ceil(cols * cellW).toInt(), ceil(rows * cellH).toInt()))
    setContentView(root, ViewGroup.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
    if (isLight) view.setTheme("#fbfaf7", "#161412", "#161412", "#2C55C055", light)
    else view.setTheme("#141210", "#F4F0EA", "#F4F0EA", "#3D7EFF55", dark)
    view.post {
      view.write(file.readText(Charsets.UTF_8))
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
