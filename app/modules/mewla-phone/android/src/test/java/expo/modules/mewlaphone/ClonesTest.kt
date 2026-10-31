package expo.modules.mewlaphone

import expo.modules.mewlaphone.helper.Clones
import expo.modules.mewlaphone.helper.PhoneUser
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

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
        assertTrue(expo.modules.mewlaphone.helper.TextCheck.landed("Sylvan", "Sylvan"))
        assertTrue(!expo.modules.mewlaphone.helper.TextCheck.landed("Sylvan", ""))
        assertTrue(!expo.modules.mewlaphone.helper.TextCheck.landed("Sylvan", null))
        assertTrue(expo.modules.mewlaphone.helper.TextCheck.landed("你好", "你好"))
    }
}
