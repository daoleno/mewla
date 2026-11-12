package com.daoleno.mewla.terminal

import kotlin.math.max
import kotlin.math.min

/**
 * How a terminal grid is drawn in the view's area. The grid is the area's own (scale 1, nothing cut)
 * unless the Session is another size because another viewer holds it: then the view draws the
 * Session's grid scaled down until it fits, never below `minScale` (the text stays readable), and
 * what still does not fit is reached by panning: `maxPanX` content pixels sideways, and `topCut`
 * pixels of the top rows above the area (the bottom rows, where the prompt is, show first).
 */
class TerminalFit(val scale: Float, val maxPanX: Float, val topCut: Float) {
  companion object {
    val NONE = TerminalFit(1f, 0f, 0f)

    fun of(areaW: Int, areaH: Int, cols: Int, rows: Int, cellW: Int, cellH: Int, minScale: Float): TerminalFit {
      if (areaW <= 0 || areaH <= 0 || cols <= 0 || rows <= 0 || cellW <= 0 || cellH <= 0) return NONE
      val w = cols * cellW.toFloat()
      val h = rows * cellH.toFloat()
      val s = max(min(1f, min(areaW / w, areaH / h)), min(minScale, 1f))
      return TerminalFit(s, max(0f, w - areaW / s), max(0f, h - areaH / s))
    }

    /** The smallest text, in dp, a scaled-down grid may draw: the scale's floor for a font size. */
    const val MIN_TEXT_DP = 6f

    fun minScale(fontDp: Float): Float = if (fontDp <= 0f) 1f else min(1f, MIN_TEXT_DP / fontDp)
  }
}
