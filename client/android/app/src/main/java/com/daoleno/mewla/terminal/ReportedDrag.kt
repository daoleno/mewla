package com.daoleno.mewla.terminal

/**
 * A drag the view does not scroll itself (the alternate screen, or nothing to scroll) is reported to the
 * page as whole lines of finger travel (`lines` > 0 back in time); the part of a line not yet reached stays
 * here. The drag's fling goes on through the same accumulator.
 */
internal class ReportedDrag {
  private var rest = 0f

  /** `px` more of finger travel (> 0 moving down) over rows `cellPx` tall: the whole lines now reached, else 0. */
  fun add(px: Float, cellPx: Float): Int {
    if (cellPx <= 0f) return 0
    rest += px / cellPx
    val lines = rest.toInt()
    rest -= lines
    return lines
  }

  fun reset() { rest = 0f }
}
