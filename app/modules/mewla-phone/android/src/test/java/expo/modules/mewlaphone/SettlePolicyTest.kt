package expo.modules.mewlaphone

import expo.modules.mewlaphone.helper.SettlePolicy
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SettlePolicyTest {
    @Test
    fun aScreenThatDrewIsSettledOnceQuiet() {
        // action at 1000, last frame at 1100, quiet window 300
        assertFalse(SettlePolicy.settled(now = 1300, actionAt = 1000, lastFrameAt = 1100, quietMs = 300, minWaitMs = 700))
        assertTrue(SettlePolicy.settled(now = 1400, actionAt = 1000, lastFrameAt = 1100, quietMs = 300, minWaitMs = 700))
    }

    @Test
    fun aScreenThatDidNotDrawWaitsTheMinimumBeforeItIsCalledStill() {
        assertFalse(SettlePolicy.settled(now = 1500, actionAt = 1000, lastFrameAt = 900, quietMs = 300, minWaitMs = 700))
        assertTrue(SettlePolicy.settled(now = 1700, actionAt = 1000, lastFrameAt = 900, quietMs = 300, minWaitMs = 700))
    }

    @Test
    fun aFrameBeforeTheActionIsNotWhatTheActionDrew() {
        assertFalse(SettlePolicy.drewSince(actionAt = 1000, lastFrameAt = 999))
        assertTrue(SettlePolicy.drewSince(actionAt = 1000, lastFrameAt = 1000))
    }

    @Test
    fun anOpaqueAppAsksForMoreQuiet() {
        // a lull of 400 ms inside a burst: the short window calls it settled, the long one waits
        assertTrue(SettlePolicy.settled(now = 1500, actionAt = 1000, lastFrameAt = 1100, quietMs = 300, minWaitMs = 700))
        assertFalse(SettlePolicy.settled(now = 1500, actionAt = 1000, lastFrameAt = 1100, quietMs = 600, minWaitMs = 1000))
    }
}
