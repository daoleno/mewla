package com.daoleno.mewla.phone

import com.daoleno.mewla.phone.PairingScreen.Front
import com.daoleno.mewla.phone.PairingScreen.Move
import com.daoleno.mewla.phone.PairingScreen.Node
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PairingScreenTest {
    @Test
    fun aCodeIsSixDigitsInOnePieceOrSpaced() {
        assertEquals("123456", PairingScreen.code("123456"))
        assertEquals("123456", PairingScreen.code(" 123 456 "))
        assertEquals("123456", PairingScreen.code("1 2 3 4 5 6"))
        assertEquals("123456", PairingScreen.code("123 456"))
    }

    @Test
    fun nothingElseIsACode() {
        assertNull(PairingScreen.code("12345"))
        assertNull(PairingScreen.code("1234567"))
        assertNull(PairingScreen.code("2026-11"))
        assertNull(PairingScreen.code("12  3456"))
        assertNull(PairingScreen.code("Wi‑Fi pairing code"))
        assertNull(PairingScreen.code("192.168.1.5:37000"))
        assertNull(PairingScreen.code(null))
    }

    @Test
    fun theCodeIsReadWithoutAnAddressAndFromADescription() {
        val dialog = listOf(Node("Pair with device"), Node("Wi‑Fi pairing code"), Node(null, "482 913"))
        assertEquals("482913", PairingScreen.readCode(dialog))
        assertNull(PairingScreen.readAddress(dialog))
    }

    @Test
    fun aospsPairingCodeViewWinsOverOtherDigits() {
        val dialog = listOf(
            Node("111111"),
            Node("482913", viewId = "com.android.settings:id/pairing_code"),
            Node("192.168.1.5:37000", viewId = "com.android.settings:id/ip_addr"),
        )
        assertEquals("482913", PairingScreen.readCode(dialog))
        assertEquals("192.168.1.5" to 37000, PairingScreen.readAddress(dialog))
    }

    private fun pairStep(
        front: Front = Front.Settings,
        dialogUp: Boolean = false,
        codeOnScreen: Boolean = false,
        pairRowVisible: Boolean = false,
        msSincePairTap: Long? = null,
    ) = PairingScreen.pairStep(front, dialogUp, codeOnScreen, pairRowVisible, msSincePairTap)

    @Test
    fun aDialogWhoseCodeCannotBeReadIsNeverClosedByTheWalk() {
        // ColorOS's dialog: nothing reads as a code, yet Settings must not be opened again over it.
        assertEquals(Move.Hold, pairStep(dialogUp = true))
        // Its window not named a dialog either: the walk tapped the row and the row is gone.
        assertEquals(Move.Hold, pairStep(msSincePairTap = 5_000))
        assertEquals(Move.Hold, pairStep(codeOnScreen = true))
    }

    @Test
    fun typingIntoTheNotificationOverTheDialogIsWaitedFor() {
        assertEquals(Move.Hold, pairStep(front = Front.SystemUi, msSincePairTap = 60_000))
        assertEquals(Move.Hold, pairStep(front = Front.SystemUi))
    }

    @Test
    fun theWalkGoesOnWhenTheDialogIsGone() {
        // Back on the Wireless debugging page (the dialog closed): tap Pair device with pairing code again.
        assertEquals(Move.Act, pairStep(pairRowVisible = true, msSincePairTap = 5_000))
        // Never tapped yet, or the wait for a dialog that never came is over.
        assertEquals(Move.Act, pairStep())
        assertEquals(Move.Act, pairStep(msSincePairTap = PairingScreen.TAP_HOLD_MS))
        // Somewhere else entirely (Mewla, the launcher): Settings again.
        assertEquals(Move.Open, pairStep(front = Front.Other))
    }

    @Test
    fun dialogsAreToldFromPagesByTheirWindowClass() {
        assertTrue(PairingScreen.isDialog("android.app.AlertDialog"))
        assertTrue(PairingScreen.isDialog("androidx.appcompat.app.AlertDialog"))
        assertTrue(PairingScreen.isDialog("com.coui.appcompat.panel.COUIBottomSheetDialog"))
        assertFalse(PairingScreen.isDialog("com.android.settings.SubSettings"))
        assertFalse(PairingScreen.isDialog(null))
    }
}
