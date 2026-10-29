package expo.modules.mewlalive

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class CalendarAlertRulesTest {
  private val minute = 60_000L

  private fun alert(item: String, at: Long, native: Boolean = false) =
    CalendarAlert(CalendarAlertRules.key(item, at), item, "s", "t", "b", at, native)

  @Test fun theKeyNamesTheItemAndTheInstant() {
    val key = CalendarAlertRules.key("item-1", 1234)
    assertEquals("item-1@1234", key)
    assertEquals("item-1", CalendarAlertRules.itemOf(key))
    assertEquals(1234L, CalendarAlertRules.atOf(key))
    assertEquals(CalendarAlertRules.notificationId(key), CalendarAlertRules.notificationId("item-1@1234"))
  }

  @Test fun firedKeysAreForgottenAfterTwoDays() {
    val now = 10 * CalendarAlertRules.FIRED_TTL_MS
    val fired = mapOf("a@1" to now - CalendarAlertRules.FIRED_TTL_MS - 1, "b@2" to now - 1000)
    assertEquals(setOf("b@2"), CalendarAlertRules.pruneFired(fired, now).keys)
  }

  @Test fun anAlertThatFiredIsNotFiredAgainByAnAlarmOrAPush() {
    val fired = mapOf("item@1000000" to 5L)
    assertTrue(CalendarAlertRules.alreadyFired(fired, "item@1000000"))
    assertFalse(CalendarAlertRules.alreadyFired(fired, "item@${1_000_000 + 10 * minute}"))
    assertFalse(CalendarAlertRules.alreadyFired(fired, "other@1000000"))
  }

  @Test fun aSnoozeHereAndTheDaemonsSnoozeOfTheSamePressAreOneAlert() {
    val fired = mapOf("item@${100 * minute}" to 1L)
    assertTrue(CalendarAlertRules.alreadyFired(fired, "item@${100 * minute + 45_000}"))
    assertTrue(CalendarAlertRules.alreadyFired(fired, "item@${100 * minute - 100_000}"))
    assertFalse(CalendarAlertRules.alreadyFired(fired, "item@${103 * minute}"))
  }

  @Test fun afterARebootALateAlarmFiresOnlyWithinTheWindow() {
    val now = 1_000 * minute
    assertEquals(BootDecision.Arm, CalendarAlertRules.onBoot(now + minute, now))
    assertEquals(BootDecision.FireNow, CalendarAlertRules.onBoot(now - 14 * minute, now))
    assertEquals(BootDecision.FireNow, CalendarAlertRules.onBoot(now - CalendarAlertRules.WINDOW_MS, now))
    assertEquals(BootDecision.Drop, CalendarAlertRules.onBoot(now - 16 * minute, now))
  }

  @Test fun aSnoozeAlertsMinutesLaterAndNeverInThePast() {
    assertEquals(1_000_000L + 5 * minute, CalendarAlertRules.snoozeAt(1_000_000L, 5))
    assertEquals(1_000_000L + 15 * minute, CalendarAlertRules.snoozeAt(1_000_000L, 15))
    assertEquals(1_000_000L + minute, CalendarAlertRules.snoozeAt(1_000_000L, 0))
  }

  @Test fun aSyncReplacesTheArmedSetButKeepsAnUnsyncedSnooze() {
    val now = 100 * minute
    val old = alert("a", 110 * minute)
    val snoozed = alert("b", 105 * minute, native = true)
    val synced = listOf(alert("c", 120 * minute), alert("d", 50 * minute))
    val merged = CalendarAlertRules.mergeArmed(listOf(old, snoozed), synced, now)
    assertEquals(listOf("c@${120 * minute}", "b@${105 * minute}"), merged.map { it.key })
  }

  @Test fun theDaemonsAlertForTheSnoozedItemReplacesThePhonesOwn() {
    val now = 100 * minute
    val snoozed = alert("b", 105 * minute, native = true)
    val daemon = alert("b", 105 * minute + 30_000)
    val merged = CalendarAlertRules.mergeArmed(listOf(snoozed), listOf(daemon), now)
    assertEquals(listOf(daemon.key), merged.map { it.key })
  }
}
