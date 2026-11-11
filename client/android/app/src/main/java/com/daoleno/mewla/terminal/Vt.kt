package com.daoleno.mewla.terminal

/** libghostty-vt through the JNI bridge (src/main/cpp/jni_bridge.cpp). One handle per terminal view. */
internal object Vt {
  init { System.loadLibrary("ghostty_vt"); System.loadLibrary("terminal_vt") }
  external fun nativeCreateTerminal(cols: Int, rows: Int): Long
  external fun nativeDestroyTerminal(handle: Long)
  external fun nativeResize(handle: Long, cols: Int, rows: Int, cellWidth: Float, cellHeight: Float)
  external fun nativeSetTheme(handle: Long, foreground: String, background: String, cursor: String, palette: Array<String>)
  /** Bytes, not a String: JNI's modified UTF-8 would corrupt characters outside the BMP (emoji). */
  external fun nativeWriteBytes(handle: Long, data: ByteArray)
  /** [rows, cols, cursorCol, cursorRow, cursorVisible, _, dirtyCount, (row, runCount, (col, cells, fg, bg, flags)*)*] and the runs' texts. */
  external fun nativeRowRuns(handle: Long, forceFull: Boolean): Array<Any>?
  /** The text a program copied (OSC 52 and the like) during the writes since the last call, once; null when none. */
  external fun nativeTakeClipboard(handle: Long): String?
}

/** The 256-color palette from the theme's 16: xterm's 6×6×6 cube and gray ramp after them. */
internal fun palette256(ansi: List<String>): Array<String> {
  val out = ArrayList<String>(256)
  for (i in 0 until 16) out.add(ansi.getOrNull(i) ?: "#808080")
  val steps = intArrayOf(0, 95, 135, 175, 215, 255)
  for (r in 0 until 6) for (g in 0 until 6) for (b in 0 until 6) out.add("#%02x%02x%02x".format(steps[r], steps[g], steps[b]))
  for (i in 0 until 24) { val v = 8 + i * 10; out.add("#%02x%02x%02x".format(v, v, v)) }
  return out.toTypedArray()
}
