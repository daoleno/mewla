package com.daoleno.mewla.terminal

import kotlin.math.roundToInt

/**
 * The phones' terminal type (SPEC 11, Terminal): mono 10/14 by default, and a pinch sets 8–14. While the
 * fingers move only the drawing scales; when they lift, the size is the nearest whole one, and the
 * page sets it (once: one new grid, one resize of the Session).
 */
object TerminalPinch {
  const val MIN_DP = 8
  const val MAX_DP = 14

  /** The line height for a text size, in whole dp: 1.4 times it (10/14, 9/13, 8/11). */
  fun lineHeightDp(fontDp: Float): Int = (fontDp * 1.4f).roundToInt()

  /** The drawing's scale while pinching from `fontDp`, kept to what 8–14 allows. */
  fun drawScale(fontDp: Float, scale: Float): Float =
    if (fontDp <= 0f) 1f else scale.coerceIn(MIN_DP / fontDp, MAX_DP / fontDp)

  /** The size a pinch from `fontDp` ends at. */
  fun sizeAfter(fontDp: Float, scale: Float): Int = (fontDp * scale).roundToInt().coerceIn(MIN_DP, MAX_DP)
}
