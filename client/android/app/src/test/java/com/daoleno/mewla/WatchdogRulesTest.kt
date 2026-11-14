package com.daoleno.mewla

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class WatchdogRulesTest {
  @Test fun noStallBeforeTheFirstBeat() {
    val stall = WatchdogRules.Stall()
    stall.resumed(0)
    assertNull(stall.silentFor(60_000))
  }

  @Test fun aStallIsMoreThanTenSecondsWithoutABeat() {
    val stall = WatchdogRules.Stall()
    stall.resumed(1_000)
    stall.beat(2_000)
    assertNull(stall.silentFor(12_000))
    assertEquals(10_001L, stall.silentFor(12_001))
  }

  @Test fun aResumeStartsTheCountAgain() {
    val stall = WatchdogRules.Stall()
    stall.beat(1_000)
    // An hour in the background without beats, then back in front.
    stall.resumed(3_600_000)
    assertNull(stall.silentFor(3_605_000))
    assertEquals(10_001L, stall.silentFor(3_610_001))
    stall.beat(3_606_000)
    assertNull(stall.silentFor(3_610_001))
  }

  @Test fun atMostTwoRestartsInFiveMinutes() {
    val now = 10 * WatchdogRules.WINDOW_MS
    assertTrue(WatchdogRules.mayRestart(emptyList(), now))
    assertTrue(WatchdogRules.mayRestart(listOf(now - 60_000), now))
    assertFalse(WatchdogRules.mayRestart(listOf(now - 120_000, now - 60_000), now))
    assertTrue(WatchdogRules.mayRestart(listOf(now - WatchdogRules.WINDOW_MS - 1, now - 60_000), now))
  }

  @Test fun theHistoryReadsBackAndSkipsWhatItCannotRead() {
    assertEquals(listOf(1L, 2L), WatchdogRules.readHistory(WatchdogRules.writeHistory(listOf(1L, 2L))))
    assertEquals(listOf(5L), WatchdogRules.readHistory("x,5,"))
    assertEquals(emptyList<Long>(), WatchdogRules.readHistory(null))
  }
}
