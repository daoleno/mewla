package expo.modules.mewlaphone

import expo.modules.mewlaphone.helper.DisplayHealth
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class DisplayHealthTest {
    @Test
    fun aDisplayTheSystemDroppedIsMadeAgain() {
        assertTrue(DisplayHealth.needsRecreate(alive = false, drewSinceLaunch = true, everDrew = true, attempt = 0))
    }

    @Test
    fun aDisplayThatNeverDrewAfterTheLaunchIsMadeAgain() {
        assertTrue(DisplayHealth.needsRecreate(alive = true, drewSinceLaunch = false, everDrew = false, attempt = 0))
    }

    @Test
    fun anAppAlreadyInFrontDrawsNothingNewAndItsDisplayIsKept() {
        assertFalse(DisplayHealth.needsRecreate(alive = true, drewSinceLaunch = false, everDrew = true, attempt = 0))
        assertFalse(DisplayHealth.needsRecreate(alive = true, drewSinceLaunch = true, everDrew = true, attempt = 0))
    }

    @Test
    fun itIsOnlyTriedOnce() {
        assertFalse(DisplayHealth.needsRecreate(alive = false, drewSinceLaunch = false, everDrew = false, attempt = 1))
    }
}
