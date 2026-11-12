package com.daoleno.mewla.terminal

/**
 * libghostty-vt and the terminal's own glyphs through src/main/cpp/jni_terminal.cpp, over the
 * packer both phones share (client/native/terminal). One handle per terminal view.
 */
internal object Vt {
  init { System.loadLibrary("ghostty_vt"); System.loadLibrary("terminal_vt") }
  external fun nativeCreateTerminal(cols: Int, rows: Int): Long
  external fun nativeDestroyTerminal(handle: Long)
  external fun nativeResize(handle: Long, cols: Int, rows: Int, cellWidth: Float, cellHeight: Float)
  /** Colors as 0xRRGGBB (alpha ignored); `palette` holds 256. */
  external fun nativeSetTheme(handle: Long, foreground: Int, background: Int, cursor: Int, palette: IntArray): Boolean
  /** Bytes, not a String: JNI's modified UTF-8 would corrupt characters outside the BMP (emoji). */
  external fun nativeWriteBytes(handle: Long, data: ByteArray)
  /** [meta: IntArray, text: CharArray], TerminalVt.h's frame (see [Frame]). */
  external fun nativeFrame(handle: Long, forceFull: Boolean): Array<Any>?
  /** The text a program copied (OSC 52 and the like) during the writes since the last call, once; null when none. */
  external fun nativeTakeClipboard(handle: Long): String?
  /** Pasted text (UTF-8) as the bytes for the program: framed for bracketed paste when it asked; null when nothing. */
  external fun nativePaste(handle: Long, data: ByteArray): ByteArray?
  /** TerminalGlyphs.h's steps for `codepoint` in a `width` x `height` pixel cell, 12 floats each; empty when the font draws it. */
  external fun nativeGlyphOps(codepoint: Int, width: Int, height: Int): FloatArray
  /** The light line thickness for a cell `height` pixels tall. */
  external fun nativeGlyphThickness(height: Int): Int

  // TerminalVt.h's layout.
  const val HEADER = 10
  const val ROW = 3
  const val RUN = 8
  const val BOLD = 1
  const val ITALIC = 2
  const val FAINT = 4
  const val INVERSE = 8
  const val STRIKE = 16
  const val OVERLINE = 32
  const val BLINK = 64
  const val CLUSTER = 128
  const val UNDERLINE_SHIFT = 8
}

/** The 256-color palette from the theme's 16 (0xRRGGBB): xterm's 6×6×6 cube and gray ramp after them. */
internal fun palette256(ansi: List<Int>): IntArray {
  val out = IntArray(256)
  for (i in 0 until 16) out[i] = ansi.getOrNull(i) ?: 0x808080
  val steps = intArrayOf(0, 95, 135, 175, 215, 255)
  var at = 16
  for (r in 0 until 6) for (g in 0 until 6) for (b in 0 until 6) out[at++] = (steps[r] shl 16) or (steps[g] shl 8) or steps[b]
  for (i in 0 until 24) { val v = 8 + i * 10; out[at++] = (v shl 16) or (v shl 8) or v }
  return out
}
