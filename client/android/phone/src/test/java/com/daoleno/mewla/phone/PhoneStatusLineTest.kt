package com.daoleno.mewla.phone

import com.daoleno.mewla.phone.PhoneStatusLine.Buttons
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

class PhoneStatusLineTest {
    private fun line(link: Boolean = true, state: String = "ready", paired: Boolean = true, agent: String? = null) =
        PhoneStatusLine.of(link, state, paired, agent)

    @Test
    fun readySaysOnAndOffersNothing() {
        assertEquals(PhoneStatusLine("Phone use is on"), line())
        assertEquals(Buttons.None, line().buttons)
    }

    @Test
    fun anUnpairedPhoneNeverSaysOn() {
        val unpaired = line(state = "setup", paired = false)
        assertEquals("Finish setting up Phone use", unpaired.title)
        assertEquals(Buttons.None, unpaired.buttons)
    }

    @Test
    fun aPairedPhoneWithoutAHelperNeverSaysOn() {
        assertEquals("Phone use isn't ready", line(state = "setup").title)
    }

    @Test
    fun stoppedOffersResume() {
        assertEquals(PhoneStatusLine("Stopped", buttons = Buttons.Resume), line(state = "stopped"))
    }

    @Test
    fun anAgentAtWorkOffersTakeOverAndStop() {
        assertEquals(
            PhoneStatusLine("Writer is using your phone", buttons = Buttons.TakeOverAndStop),
            line(agent = "Writer"),
        )
    }

    @Test
    fun aLostLinkComesFirstWhateverTheState() {
        for (state in listOf("ready", "setup", "stopped", "person_in_control")) {
            assertEquals("Open Mewla to reconnect", line(link = false, state = state, agent = "Writer").title)
        }
    }

    @Test
    fun personInControlWithoutAnAgentStillSaysOn() {
        assertEquals("Phone use is on", line(state = "person_in_control").title)
    }

    @Test
    fun onIsSaidOnlyWhenTheHelperIsUp() {
        for (paired in listOf(true, false)) {
            assertNotEquals("Phone use is on", line(state = "setup", paired = paired).title)
        }
    }
}
