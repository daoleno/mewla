package com.daoleno.mewla.phone

import com.daoleno.mewla.phone.helper.Clones
import com.daoleno.mewla.phone.helper.DisplayHealth
import com.daoleno.mewla.phone.helper.FrameCheck
import com.daoleno.mewla.phone.helper.PhoneUser
import com.daoleno.mewla.phone.helper.SettlePolicy
import com.daoleno.mewla.phone.helper.TextCheck
import com.daoleno.mewla.phone.helper.TypingRuns
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
    fun aDisplayThatDrewBeforeButIsBlackWithNoAppOnItIsMadeAgain() {
        // 11-19: Chrome, X and 小红书 all "opened" on a display that had drawn earlier and now stayed black, 0 elements.
        assertTrue(DisplayHealth.needsRecreate(alive = true, drewSinceLaunch = false, everDrew = true, attempt = 0, showsNothing = true))
        assertFalse(DisplayHealth.needsRecreate(alive = true, drewSinceLaunch = false, everDrew = true, attempt = 1, showsNothing = true))
        // An app window on it, even one drawing black, is kept: remaking the display would close it.
        assertFalse(DisplayHealth.needsRecreate(alive = true, drewSinceLaunch = false, everDrew = true, attempt = 0, showsNothing = false))
    }

    @Test
    fun onlyALaunchThatLeftNoAppWindowFails() {
        // 11-20 emulator: a fresh display read before any launch, an app drawing black (ProbeBlack), a cold start's black
        // splash and the same behind the lock screen all failed as screen_blank. Each is a healthy phone.
        assertEquals(DisplayHealth.Empty.NOT_OPENED, DisplayHealth.empty(windows = 0, elements = 0, dark = true, launched = true))
        assertEquals(DisplayHealth.Empty.NOTHING_OPEN, DisplayHealth.empty(windows = 0, elements = 0, dark = true, launched = false))
        assertEquals(DisplayHealth.Empty.BLACK_APP, DisplayHealth.empty(windows = 1, elements = 0, dark = true, launched = true))
        assertEquals(DisplayHealth.Empty.BLACK_APP, DisplayHealth.empty(windows = 1, elements = 0, dark = true, launched = false))
        // 11-20 00:30: 小红书 hung, nothing answered and the last frame stayed: say so instead of a bare "0 elements".
        assertEquals(DisplayHealth.Empty.NOT_ANSWERING, DisplayHealth.empty(windows = 0, elements = 0, dark = false, launched = false))
        assertEquals(DisplayHealth.Empty.OWN_UI, DisplayHealth.empty(windows = 1, elements = 0, dark = false, launched = false))
        assertEquals(DisplayHealth.Empty.NONE, DisplayHealth.empty(windows = 1, elements = 3, dark = true, launched = true))
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

class FrameCheckTest {
    private fun frame(width: Int, height: Int, pixelStride: Int = 4, rowPad: Int = 16, fill: (Int, Int) -> Int): java.nio.ByteBuffer {
        val rowStride = width * pixelStride + rowPad
        val buffer = java.nio.ByteBuffer.allocate(rowStride * height)
        for (y in 0 until height) for (x in 0 until width) {
            val rgb = fill(x, y)
            val at = y * rowStride + x * pixelStride
            buffer.put(at, (rgb shr 16).toByte()); buffer.put(at + 1, (rgb shr 8).toByte()); buffer.put(at + 2, rgb.toByte()); buffer.put(at + 3, 0xFF.toByte())
        }
        return buffer
    }

    @Test
    fun aBlackFrameIsDarkAndAFewBrightPixelsDoNotChangeThat() {
        assertTrue(FrameCheck.dark(frame(200, 400) { _, _ -> 0x000000 }, 200 * 4 + 16, 4, 200, 400))
        assertTrue(FrameCheck.dark(frame(200, 400) { x, y -> if (x < 3 && y < 3) 0xFFFFFF else 0x050505 }, 200 * 4 + 16, 4, 200, 400))
    }

    @Test
    fun anAppsPictureIsNotDarkEvenInADarkTheme() {
        assertFalse(FrameCheck.dark(frame(200, 400) { _, _ -> 0xFFFFFF }, 200 * 4 + 16, 4, 200, 400))
        // Dark theme: a near-black background with lines of text across it.
        assertFalse(FrameCheck.dark(frame(200, 400) { _, y -> if (y % 40 < 6) 0xE0E0E0 else 0x121212 }, 200 * 4 + 16, 4, 200, 400))
    }
}

class TypingRunsTest {
    private val ascii = { c: Char -> c.code in 0x20..0x7e }
    private fun shape(text: String) = TypingRuns.split(text, ascii).map { it.text to it.keys }
    private fun pastes(text: String) = TypingRuns.split(text, ascii).count { !it.keys }

    @Test
    fun aTriggerGoesAsKeysUpToItsFirstCharacterTheKeyboardLacksAndTheRestIsOnePaste() {
        // 11-20 小红书: "#AI编程" reaches the field as "#AI" typed, so the picker opens, then "编程" pasted right after it.
        assertEquals(listOf("#AI" to true, "编程 and " to false, "@X" to true), shape("#AI编程 and @X"))
        assertEquals("#AI编程 and @X", TypingRuns.split("#AI编程 and @X", ascii).joinToString("") { it.text })
    }

    @Test
    fun mixedTextWithNoTriggerIsOnePasteAndPlainKeyboardTextNone() {
        // Every paste makes the phone say "pasted from your clipboard": a mixed paragraph is one, not one per run.
        assertEquals(listOf("受够了手动管AI，我重写了Mewla" to false), shape("受够了手动管AI，我重写了Mewla"))
        assertEquals(listOf("hi\n👋ok" to false), shape("hi\n👋ok"))
        assertEquals(listOf("plain words, 100% ok" to true), shape("plain words, 100% ok"))
        // A # or @ inside a word (an address, C#) opens no picker, so it does not split the paste.
        assertEquals(1, pastes("写信给 a@b.com 讲 C# 吧"))
    }

    @Test
    fun theXiaohongshuPostNeedsFivePastesInsteadOfTwentyOne() {
        val title = "受够了手动管AI，我重写了Mewla"
        val body = "受够了手动管理一个个 agent session，每一步都要人盯着、不停介入。\n\n" +
            "所以我把 Mewla 从头重写了一遍，这次是原生的：一个开源 App，在你自己的电脑上带一队 AI 编程 agent（Claude Code、Codex、Pi）干活。\n\n" +
            "现在我用手机就能同时跑 10 个以上的任务，只有真需要我拍板的时候才叫我。\n\n" +
            "iPhone、Android、网页都能用；电脑端支持 macOS、Linux、Windows（Beta）。\n\n" +
            "#AI编程 #AIAgent #ClaudeCode #独立开发 #开源"
        assertEquals(1, pastes(title))
        // The body in one paste, then one for each tag with Chinese after its typed #.
        assertEquals(4, pastes(body))
        assertEquals(body, TypingRuns.split(body, ascii).joinToString("") { it.text })
    }

    @Test
    fun emptyTextHasNoRuns() {
        assertTrue(TypingRuns.split("", ascii).isEmpty())
    }
}

class InsertedAtTest {
    @Test
    fun textPutInAtTheCursorIsSeenWhereverTheCursorWas() {
        assertTrue(TextCheck.insertedAt("", "#AI编程", "#AI编程"))
        assertTrue(TextCheck.insertedAt("ab", "XY", "abXY"))
        assertTrue(TextCheck.insertedAt("ab", "XY", "XYab"))
        assertTrue(TextCheck.insertedAt("ab", "XY", "aXYb"))
        assertTrue(TextCheck.insertedAt("aa", "a", "aaa"))
    }

    @Test
    fun anythingElseIsNot() {
        assertFalse(TextCheck.insertedAt("ab", "XY", "abX"))
        assertFalse(TextCheck.insertedAt("ab", "XY", "XY"))
        assertFalse(TextCheck.insertedAt("ab", "XY", "aYXb"))
        assertFalse(TextCheck.insertedAt("ab", "XY", null))
    }
}
