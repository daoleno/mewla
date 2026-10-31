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
