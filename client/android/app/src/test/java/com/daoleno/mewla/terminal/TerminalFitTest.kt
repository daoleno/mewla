package com.daoleno.mewla.terminal

import org.junit.Assert.assertEquals
import org.junit.Test

class TerminalFitTest {
  // A phone's area: 83 x 40 cells of 13 x 26 px.
  private val cellW = 13
  private val cellH = 26
  private val areaW = 83 * cellW + 7
  private val areaH = 40 * cellH + 11
  private val floor = TerminalFit.minScale(8f)

  @Test fun theAreasOwnGridIsDrawnAsIs() {
    val fit = TerminalFit.of(areaW, areaH, 83, 40, cellW, cellH, floor)
    assertEquals(1f, fit.scale)
    assertEquals(0f, fit.maxPanX)
    assertEquals(0f, fit.topCut)
  }

  @Test fun aSmallerSessionIsDrawnAtFullSizeInTheCorner() {
    val fit = TerminalFit.of(areaW, areaH, 60, 20, cellW, cellH, floor)
    assertEquals(1f, fit.scale)
    assertEquals(0f, fit.maxPanX)
    assertEquals(0f, fit.topCut)
  }

  @Test fun aSlightlyWiderSessionIsScaledDownToFitWhole() {
    val fit = TerminalFit.of(areaW, areaH, 100, 40, cellW, cellH, floor)
    assertEquals(areaW / (100f * cellW), fit.scale, 1e-4f)
    assertEquals(0f, fit.maxPanX, 0.01f)
    assertEquals(0f, fit.topCut)
  }

  @Test fun aDeskSessionStopsAtTheReadableFloorAndPansSideways() {
    // 8 dp text never goes below 6 dp.
    assertEquals(0.75f, floor, 1e-6f)
    val fit = TerminalFit.of(areaW, areaH, 160, 45, cellW, cellH, floor)
    assertEquals(0.75f, fit.scale, 1e-6f)
    assertEquals(160f * cellW - areaW / 0.75f, fit.maxPanX, 0.01f)
    // 45 rows at 0.75 fit in 40 rows' height.
    assertEquals(0f, fit.topCut)
  }

  @Test fun aShortAreaShowsTheBottomRowsAndCutsTheTop() {
    // The keyboard is up: 14 rows of room.
    val shortH = 14 * cellH
    val fit = TerminalFit.of(areaW, shortH, 160, 45, cellW, cellH, floor)
    assertEquals(0.75f, fit.scale, 1e-6f)
    assertEquals(45f * cellH - shortH / 0.75f, fit.topCut, 0.01f)
  }

  @Test fun noFloorAboveOne() {
    assertEquals(1f, TerminalFit.minScale(4f))
    assertEquals(1f, TerminalFit.minScale(0f))
  }
}
