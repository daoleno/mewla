package expo.modules.mewlaphone

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class LinkRetryTest {
    @Test
    fun theFirstBackgroundAttemptIsSoonAndAComputerThatStaysAwayIsAskedLessAndLess() {
        assertEquals(10_000, LinkRetry.delayMs(0))
        assertTrue(LinkRetry.delayMs(1) > LinkRetry.delayMs(0))
        assertTrue(LinkRetry.delayMs(4) > LinkRetry.delayMs(3))
    }

    @Test
    fun theWaitNeverPassesFiveMinutesWhateverTheCount() {
        assertEquals(300_000, LinkRetry.delayMs(5))
        assertEquals(300_000, LinkRetry.delayMs(500))
        assertEquals(10_000, LinkRetry.delayMs(-3))
    }
}
