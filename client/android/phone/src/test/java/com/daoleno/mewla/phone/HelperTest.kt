package com.daoleno.mewla.phone

import com.daoleno.mewla.phone.helper.Clones
import com.daoleno.mewla.phone.helper.DisplayHealth
import com.daoleno.mewla.phone.helper.PhoneUser
import com.daoleno.mewla.phone.helper.SettlePolicy
import com.daoleno.mewla.phone.helper.TextCheck
import com.daoleno.mewla.phone.helper.Wire
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.DataInputStream
import java.io.DataOutputStream
import java.io.IOException

// The Expo module's tests (app/modules/mewla-phone/android/src/test), on the ported code.

class ClonesTest {
    @Test
    fun usersExcludeThePersonsOwn() {
        val out = "Users:\n\tUserInfo{0:机主:c13} running\n\tUserInfo{999:分身:4410} running\n\tUserInfo{12:Work:30} \n"
        assertEquals(listOf(PhoneUser(999, "分身", true), PhoneUser(12, "Work", false)), Clones.parseUsers(out))
    }

    @Test
    fun packagesComeFromComponentLines() {
        val out = "com.tencent.mm/com.tencent.mm.ui.LauncherUI\n  com.tencent.mm/.Other\ncom.x.y/.A\nnoise line\n"
        assertEquals(listOf("com.tencent.mm", "com.x.y"), Clones.parsePackages(out))
    }

    @Test
    fun componentsAreTheLauncherActivitiesAsTheSystemWroteThem() {
        val out = "2 activities found:\n" +
            "  Activity #0:\n" +
            "    com.tencent.mm/.ui.LauncherUI\n" +
            "  Activity #1:\n" +
            "    com.tencent.mm/com.tencent.mm.ui.Alias\n" +
            "    com.android.chrome/com.google.android.apps.chrome.Main\n" +
            "\tcom.x.y/.A  \n" +
            "noise line\n"
        assertEquals(
            mapOf(
                "com.tencent.mm" to "com.tencent.mm/.ui.LauncherUI",
                "com.android.chrome" to "com.android.chrome/com.google.android.apps.chrome.Main",
                "com.x.y" to "com.x.y/.A",
            ),
            Clones.parseComponents(out),
        )
    }

    @Test
    fun noComponentsInAnEmptyAnswer() {
        assertTrue(Clones.parseComponents("").isEmpty())
        assertTrue(Clones.parseComponents("No activities found\n").isEmpty())
    }

    @Test
    fun aRefusalCarriesWhatBothStartsSaid() {
        val unresolved = "Error: Activity not started, unable to resolve Intent { act=android.intent.action.MAIN }\n"
        assertTrue(Clones.failed(unresolved))
        assertTrue(!Clones.failed("Starting: Intent { cmp=com.tencent.mm/.ui.LauncherUI }\n"))
        val text = Clones.refusal(unresolved, "Error type 3\n")
        assertTrue(text.contains("am start: Error: Activity not started"))
        assertTrue(text.contains("cmd activity start-activity: Error type 3"))
    }

    @Test
    fun cloneLabelsCarryTheUser() {
        assertEquals("微信 (分身 999)", Clones.label("微信", 999, true))
        assertEquals("WeChat (clone 999)", Clones.label("WeChat", 999, false))
    }

    @Test
    fun aStoppedUserIsRecognised() {
        assertTrue(Clones.needsStart("Error: java.lang.IllegalStateException: User 999 is stopped"))
        assertTrue(!Clones.needsStart("Starting: Intent { ... }"))
    }
}

class TextCheckTest {
    @Test
    fun aFieldTookTheTextOnlyWhenItReadsBackTheSame() {
        assertTrue(TextCheck.landed("Sylvan", "Sylvan"))
        assertTrue(!TextCheck.landed("Sylvan", ""))
        assertTrue(!TextCheck.landed("Sylvan", null))
        assertTrue(TextCheck.landed("你好", "你好"))
    }

    @Test
    fun aHintIsNeverTheTextAlreadyInTheField() {
        // The usual way: the field says it shows its hint.
        assertEquals("", TextCheck.existing("输入消息", "输入消息", true))
        // Telegram: the hint comes as the text and nothing says so.
        assertEquals("", TextCheck.existing("输入消息", "输入消息", false))
        // Real text stays, also when a hint exists.
        assertEquals("Hello", TextCheck.existing("Hello", "输入消息", false))
        assertEquals("", TextCheck.existing(null, null, false))
        assertEquals("输入消息", TextCheck.existing("输入消息", null, false))
    }
}

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

class HelperAdoptionTest {
    private fun hello(protocol: Int, build: Long) = JSONObject().put("protocol", protocol).put("build", build)

    @Test
    fun aHelperOfThisInstallIsUsed() {
        assertEquals(HelperKind.CURRENT, HelperAdoption.classify(hello(1, 42), protocol = 1, build = 42))
    }

    @Test
    fun aHelperLeftByAnEarlierInstallIsStaleNotUseless() {
        assertEquals(HelperKind.STALE, HelperAdoption.classify(hello(1, 41), protocol = 1, build = 42))
    }

    @Test
    fun aHelperOfAnotherProtocolIsOfNoUse() {
        assertEquals(HelperKind.FOREIGN, HelperAdoption.classify(hello(2, 42), protocol = 1, build = 42))
    }

    @Test
    fun aHelperThatDoesNotAnswerIsOfNoUse() {
        assertEquals(HelperKind.FOREIGN, HelperAdoption.classify(null, protocol = 1, build = 42))
    }
}

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

class WireTest {
    private fun roundTrip(vararg frames: Pair<Byte, ByteArray>): DataInputStream {
        val bytes = ByteArrayOutputStream()
        val out = DataOutputStream(bytes)
        frames.forEach { (kind, payload) -> Wire.write(out, kind, payload) }
        return DataInputStream(ByteArrayInputStream(bytes.toByteArray()))
    }

    @Test
    fun framesComeBackInOrderWithTheirKinds() {
        val input = roundTrip(Wire.AUTH to "token".toByteArray(), Wire.REQUEST to "{\"op\":\"hello\"}".toByteArray(), Wire.FRAME to byteArrayOf(1, 2, 3))
        val auth = Wire.read(input)!!
        assertEquals(Wire.AUTH, auth.kind)
        assertEquals("token", auth.text())
        assertEquals("{\"op\":\"hello\"}", Wire.read(input)!!.text())
        val frame = Wire.read(input)!!
        assertEquals(Wire.FRAME, frame.kind)
        assertEquals(listOf<Byte>(1, 2, 3), frame.payload.toList())
    }

    @Test
    fun aCleanCloseBetweenMessagesIsNullNotAnError() {
        assertNull(Wire.read(DataInputStream(ByteArrayInputStream(ByteArray(0)))))
    }

    @Test
    fun anOversizedLengthIsRefusedBeforeAnythingIsAllocated() {
        val bytes = ByteArrayOutputStream()
        DataOutputStream(bytes).apply {
            writeByte(Wire.REQUEST.toInt())
            writeInt(Wire.MAX_PAYLOAD + 1)
        }
        val failure = runCatching { Wire.read(DataInputStream(ByteArrayInputStream(bytes.toByteArray()))) }.exceptionOrNull()
        assertTrue(failure is IOException)
    }
}
