package com.daoleno.mewla.terminal

import org.junit.Assert.assertEquals
import org.junit.Test

class TerminalPinchTest {
  @Test fun lineHeightIsOnePointFourTimesTheSizeInWholePoints() {
    assertEquals(14, TerminalPinch.lineHeightDp(10f))
    assertEquals(13, TerminalPinch.lineHeightDp(9f))
    assertEquals(11, TerminalPinch.lineHeightDp(8f))
    assertEquals(20, TerminalPinch.lineHeightDp(14f))
  }

  @Test fun theDrawingScalesNoFurtherThanEightToFourteen() {
    assertEquals(0.8f, TerminalPinch.drawScale(10f, 0.5f), 1e-6f)
    assertEquals(1.4f, TerminalPinch.drawScale(10f, 3f), 1e-6f)
    assertEquals(1.1f, TerminalPinch.drawScale(10f, 1.1f), 1e-6f)
  }

  @Test fun aPinchEndsAtTheNearestWholeSize() {
    assertEquals(12, TerminalPinch.sizeAfter(10f, 1.18f))
    assertEquals(10, TerminalPinch.sizeAfter(10f, 1.04f))
    assertEquals(8, TerminalPinch.sizeAfter(10f, 0.5f))
    assertEquals(14, TerminalPinch.sizeAfter(12f, 2f))
  }
}
