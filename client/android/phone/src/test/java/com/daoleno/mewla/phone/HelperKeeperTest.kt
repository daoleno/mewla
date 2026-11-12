package com.daoleno.mewla.phone

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** Every interruption a normal day produces, played through the keeper on a phone that is only values. */
class HelperKeeperTest {
    private class Phone : HelperKeeper.Env {
        var now = 1_000_000L
        var enabled = true
        var paired = true
        var pairing = false
        var wifi = true
        var wirelessOn = true
        var granted = true
        var pausedUntil = 0L
        var helper = true
        var grantResult: String? = null
        var startError: String? = null
        var reason: String? = "unset"
        var starts = 0
        var grants = 0

        override fun facts() = Facts(enabled, paired, pairing, wifi, wirelessOn, granted, pausedUntil, now)
        override fun helperUp() = helper
        override fun startHelper() {
            starts += 1
            startError?.let { throw IllegalStateException(it) }
            // Starting needs Wireless debugging, which a held grant switches on.
            if (!wirelessOn) wirelessOn = true
            helper = true
        }
        override fun grant(): String? {
            grants += 1
            if (grantResult == null) granted = true
            return grantResult
        }
        override fun report(reason: String?) { this.reason = reason }
    }

    private fun keeper(phone: Phone) = HelperKeeper(phone)

    @Test fun aHelperThatIsUpReportsNothingWrong() {
        val phone = Phone()
        assertEquals(HelperKeeper.IDLE_MS, keeper(phone).step())
        assertNull(phone.reason)
        assertEquals(0, phone.starts)
    }

    @Test fun wifiDropsAndReturnsWithWirelessDebuggingStillOn() {
        val phone = Phone().apply { helper = false; wifi = false }
        val keeper = keeper(phone)
        keeper.step()
        assertTrue(phone.reason!!.contains("not on Wi-Fi"))
        assertEquals(0, phone.starts)
        phone.wifi = true
        keeper.step()
        assertEquals(1, phone.starts)
        assertNull(phone.reason)
    }

    @Test fun androidSwitchesWirelessDebuggingOffAfterTheWifiGapAndMewlaMaySwitchItOn() {
        val phone = Phone().apply { helper = false; wirelessOn = false; granted = true }
        keeper(phone).step()
        assertEquals(1, phone.starts)
        assertTrue(phone.wirelessOn)
        assertNull(phone.reason)
    }

    @Test fun withoutTheGrantTheReasonIsSaidInPlainWordsAndNothingIsTriedInVain() {
        // 13:38 on 2026-11-12: the helper went with Wireless debugging, and the permission was never there.
        val phone = Phone().apply { helper = false; wirelessOn = false; granted = false }
        val keeper = keeper(phone)
        repeat(3) { keeper.step() }
        assertEquals(0, phone.starts)
        val reason = phone.reason!!
        assertTrue(reason, reason.contains("Wireless debugging is off"))
        assertTrue(reason, reason.contains("permission"))
        assertFalse(reason, reason.contains("open Mewla → Phone to set it up"))
        // The person switches Wireless debugging on once; the helper starts and the grant is taken on the spot.
        phone.wirelessOn = true
        keeper.step()
        assertEquals(1, phone.starts)
        keeper.step()
        assertEquals(1, phone.grants)
        assertTrue(phone.granted)
        // From then on the same interruption heals by itself.
        phone.helper = false
        phone.wirelessOn = false
        keeper.step()
        assertTrue(phone.helper)
        assertNull(phone.reason)
    }

    @Test fun aHelperAdoptedWithoutTheGrantGetsItWhileWirelessDebuggingIsOn() {
        val phone = Phone().apply { granted = false }
        val keeper = keeper(phone)
        keeper.step()
        assertEquals(1, phone.grants)
        assertTrue(phone.granted)
        keeper.step()
        assertEquals("not asked again once it has it", 1, phone.grants)
    }

    @Test fun aRefusedGrantIsRetriedLaterNotInALoopAndItsWordsReachTheReason() {
        val phone = Phone().apply { granted = false; grantResult = "Android refused: SecurityException" }
        val keeper = keeper(phone)
        keeper.step()
        keeper.step()
        assertEquals(1, phone.grants)
        phone.now += HelperKeeper.GRANT_RETRY_MS
        keeper.step()
        assertEquals(2, phone.grants)
        phone.helper = false
        phone.wirelessOn = false
        keeper.step()
        assertTrue(phone.reason!!, phone.reason!!.contains("Android refused: SecurityException"))
    }

    @Test fun theAppKilledAndStartedAgainAdoptsTheHelperThatKeptRunning() {
        val phone = Phone().apply { helper = false }
        keeper(phone).step()
        assertEquals(1, phone.starts)
        assertTrue(phone.helper)
        assertNull(phone.reason)
    }

    @Test fun thePhoneRestartingWaitsForWifiThenStartsTheHelper() {
        val phone = Phone().apply { helper = false; wifi = false; wirelessOn = false }
        val keeper = keeper(phone)
        keeper.step()
        assertEquals(0, phone.starts)
        phone.wifi = true
        keeper.step()
        assertEquals(1, phone.starts)
        assertNull(phone.reason)
    }

    @Test fun aFailedStartIsRetriedWithABackoffAndTheFailureIsTheReason() {
        val phone = Phone().apply { helper = false; startError = "no wireless debugging port found" }
        val keeper = keeper(phone)
        val waits = List(6) { keeper.step() }
        assertEquals(listOf(4_000L, 8_000L, 16_000L, 30_000L, 30_000L, 30_000L), waits)
        assertEquals("no wireless debugging port found", phone.reason)
        phone.startError = null
        assertEquals(HelperKeeper.IDLE_MS, keeper.step())
        assertNull(phone.reason)
        // And the next loss starts from the short wait again.
        phone.helper = false
        phone.startError = "again"
        assertEquals(4_000L, keeper.step())
    }

    @Test fun aChangeFromTheSystemStartsTheBackoffOver() {
        val phone = Phone().apply { helper = false; startError = "x" }
        val keeper = keeper(phone)
        repeat(4) { keeper.step() }
        keeper.changed()
        assertEquals(4_000L, keeper.step())
    }

    @Test fun anUnpairedPhoneSaysSoAndNothingIsStarted() {
        val phone = Phone().apply { helper = false; paired = false }
        keeper(phone).step()
        assertEquals(0, phone.starts)
        assertTrue(phone.reason!!.contains("not paired"))
    }

    @Test fun aSetupInProgressIsLeftAlone() {
        val phone = Phone().apply { helper = false; pairing = true }
        keeper(phone).step()
        assertEquals(0, phone.starts)
        assertTrue(phone.reason!!.contains("in progress"))
    }

    @Test fun thePromptPauseIsShortSaysWhenItEndsAndLiftsByItself() {
        val guard = PromptGuard()
        var now = 1_000_000L
        assertTrue(guard.mayRaisePrompt(now))
        now += 10_000
        assertTrue("one fall-back within 90 s is allowed", guard.mayRaisePrompt(now))
        now += 10_000
        assertFalse("the second one in a row pauses", guard.mayRaisePrompt(now))
        assertEquals(now + PromptGuard.PAUSE_MS, guard.pausedUntil)
        val phone = Phone().apply { helper = false; wirelessOn = false; granted = true; this.now = now; pausedUntil = guard.pausedUntil }
        val keeper = keeper(phone)
        keeper.step()
        assertEquals(0, phone.starts)
        assertTrue(phone.reason!!, phone.reason!!.contains("tries again at"))
        phone.now = guard.pausedUntil
        keeper.step()
        assertEquals(1, phone.starts)
        assertTrue(phone.helper)
    }

    @Test fun aPersonAskingClearsThePause() {
        val guard = PromptGuard()
        val t = 1_000_000L
        guard.mayRaisePrompt(t)
        guard.mayRaisePrompt(t + 1_000)
        assertFalse(guard.mayRaisePrompt(t + 2_000))
        guard.personAsked()
        assertEquals(0, guard.pausedUntil)
        assertTrue(guard.mayRaisePrompt(t + 3_000))
    }

    @Test fun aSwitchThatStaysOnIsNoStrike() {
        val guard = PromptGuard()
        var now = 1_000_000L
        repeat(5) {
            assertTrue(guard.mayRaisePrompt(now))
            guard.switchHeld()
            now += 10_000
        }
    }
}
