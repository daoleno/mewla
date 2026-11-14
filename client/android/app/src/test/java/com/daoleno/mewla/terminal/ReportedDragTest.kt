package com.daoleno.mewla.terminal

import org.junit.Assert.assertEquals
import org.junit.Test

class ReportedDragTest {
  @Test fun aRowOfTravelIsALineAndTheRestWaits() {
    val drag = ReportedDrag()
    assertEquals(0, drag.add(10f, 26f))
    assertEquals(0, drag.add(10f, 26f))
    // 10 + 10 + 10 = 30 px: one 26 px row, 4 px kept.
    assertEquals(1, drag.add(10f, 26f))
    assertEquals(0, drag.add(21f, 26f))
    assertEquals(1, drag.add(2f, 26f))
  }

  @Test fun movingDownIsBackInTimeAndUpIsTheOtherWay() {
    val drag = ReportedDrag()
    assertEquals(3, drag.add(80f, 26f))
    drag.reset()
    assertEquals(-3, drag.add(-80f, 26f))
  }

  @Test fun aFlingGoesOnThroughTheSameAccumulator() {
    val drag = ReportedDrag()
    var total = 0
    // A fling slowing down: each frame moves less than the one before.
    var step = 120f
    repeat(20) { total += drag.add(step, 26f); step *= 0.8f }
    // 120 * (1 - 0.8^20) / 0.2 = about 593 px: 22 rows of 26 px and a bit.
    assertEquals(22, total)
  }

  @Test fun aMissingCellHeightReportsNothing() {
    assertEquals(0, ReportedDrag().add(100f, 0f))
  }
}
