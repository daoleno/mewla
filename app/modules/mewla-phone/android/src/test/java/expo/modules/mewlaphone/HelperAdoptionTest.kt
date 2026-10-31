package expo.modules.mewlaphone

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Test

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
