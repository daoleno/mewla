package com.daoleno.mewla.phone.link

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class LinkBackoffTest {
    @Test
    fun theLinkTriesAgainAfterOneTwoFourEightThenEveryFifteenSeconds() {
        assertEquals(listOf(1_000L, 2_000L, 4_000L, 8_000L, 15_000L, 15_000L, 15_000L), (0..6).map { LinkBackoff.delayMs(it) })
    }

    @Test
    fun aNegativeCountIsTheFirstTry() {
        assertEquals(1_000L, LinkBackoff.delayMs(-3))
    }
}

class PhoneFramesTest {
    @Test
    fun helloSaysProtocolOneTheAppVersionAndTheState() {
        val hello = PhoneFrames.hello("Pixel 9", 36, "0.3.2", "setup")
        assertEquals("hello", hello.getString("type"))
        assertEquals(1, hello.getInt("version"))
        assertEquals("Pixel 9", hello.getString("model"))
        assertEquals(36, hello.getInt("sdk"))
        assertEquals("0.3.2", hello.getString("app_version"))
        assertEquals("setup", hello.getString("state"))
    }

    private fun status(state: String, w: Int = 0, h: Int = 0, problem: Any = JSONObject.NULL, display: Any = JSONObject.NULL) =
        JSONObject().put("state", state).put("w", w).put("h", h).put("problem", problem).put("display_info", display)

    @Test
    fun aStateFrameLeavesOutWhatIsNotThereYet() {
        val frame = PhoneFrames.state(status("ready"))
        assertEquals("state", frame.getString("type"))
        assertEquals("ready", frame.getString("state"))
        assertFalse(frame.has("w"))
        assertFalse(frame.has("h"))
        assertFalse(frame.has("problem"))
        assertFalse(frame.has("display"))
    }

    @Test
    fun aStateFrameCarriesTheDisplayOnceItExists() {
        val frame = PhoneFrames.state(status("ready", 720, 1600, display = "1080x2400 asked 420dpi"))
        assertEquals(720, frame.getInt("w"))
        assertEquals(1600, frame.getInt("h"))
        assertEquals("1080x2400 asked 420dpi", frame.getString("display"))
    }

    @Test
    fun theProblemIsSaidOnlyInSetup() {
        assertEquals("Wireless debugging is off", PhoneFrames.state(status("setup", problem = "Wireless debugging is off")).getString("problem"))
        assertFalse(PhoneFrames.state(status("ready", problem = "Wireless debugging is off")).has("problem"))
    }

    @Test
    fun anUnchangedStatusIsNotSentAgain() {
        assertEquals(PhoneFrames.stateKey(status("ready", 720, 1600)), PhoneFrames.stateKey(status("ready", 720, 1600)))
        assertNotEquals(PhoneFrames.stateKey(status("ready")), PhoneFrames.stateKey(status("stopped")))
        assertNotEquals(PhoneFrames.stateKey(status("setup")), PhoneFrames.stateKey(status("setup", problem = "not paired")))
        // A problem outside setup is not something the computer hears about.
        assertEquals(PhoneFrames.stateKey(status("ready")), PhoneFrames.stateKey(status("ready", problem = "old")))
    }

    @Test
    fun eventsCarryTheHandoffIdOnlyWhenThereIsOne() {
        val stop = PhoneFrames.event("stop")
        assertEquals(setOf("type", "kind"), stop.keys().asSequence().toSet())
        assertEquals("event", stop.getString("type"))
        assertEquals("stop", stop.getString("kind"))
        assertEquals("h1", PhoneFrames.event("handed_off", "h1").getString("id"))
    }

    @Test
    fun replacedAndUnsupportedStopTheLinkEverythingElseRetries() {
        assertEquals("replaced", PhoneFrames.closeOutcome(4001))
        assertEquals("unsupported", PhoneFrames.closeOutcome(4002))
        assertEquals("retry", PhoneFrames.closeOutcome(4003))
        assertEquals("retry", PhoneFrames.closeOutcome(1006))
        assertEquals("retry", PhoneFrames.closeOutcome(1008))
    }
}

class DeviceAuthTest {
    private fun unhex(s: String) = DeviceAuth.unhex(s)

    // RFC 8032, 7.1, TEST 1 and TEST 2.
    @Test
    fun signaturesAreRfc8032Ed25519() {
        val seed = unhex("9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60")
        assertEquals("d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a", DeviceAuth.hex(DeviceAuth.publicKey(seed)))
        assertEquals(
            "e5564300c360ac729086e2cc806e828a84877f1eb8e5d974d873e065224901555fb8821590a33bacc61e39701cf9b46bd25bf5f0595bbe24655141438e7a100b",
            DeviceAuth.hex(DeviceAuth.sign(seed, ByteArray(0))),
        )
        val seed2 = unhex("4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb")
        assertEquals(
            "92a009a9f0d4cab8720e820b5f642540a2b27b5416503f8fb3762223ebdb69da085ac1e43e15996e458f3613d0f11d8c387b2eaeb4302aeeb00d291612bb0c00",
            DeviceAuth.hex(DeviceAuth.sign(seed2, byteArrayOf(0x72))),
        )
    }

    @Test
    fun theHeaderIsTheOneTheComputerChecks() {
        val identity = DeviceIdentity(" device-1 ", "9D61B19DEFFD5A60BA844AF492EC2CC44449C5697B326919703BAC031CAE7F60")
        val daemon = "AB".repeat(32)
        val nonce = "0123456789abcdef0123456789abcdef"
        val header = DeviceAuth.header("mewla-phone-device", daemon, identity, "1700000000000", nonce)
        val parts = header.removePrefix("Device ").split(":")
        assertTrue(header.startsWith("Device v1:"))
        assertEquals(listOf("v1", "device-1", "ab".repeat(32), "1700000000000", nonce), parts.take(5))
        val signed = "mewla-phone-device\n${"ab".repeat(32)}\ndevice-1\n1700000000000\n$nonce".toByteArray()
        assertEquals(DeviceAuth.hex(DeviceAuth.sign(unhex(identity.seedHex.lowercase()), signed)), parts[5])
        // What core/deviceAuthContract.ts signDeviceAuthorization (tweetnacl) makes of the same inputs.
        assertEquals(
            "Device v1:device-1:${"ab".repeat(32)}:1700000000000:$nonce:" +
                "f47b6faf7ea590bb70e7a34bf2787d325623c00c6f9a711476a466d3bbb58a8a5028280de89eae7c5ace2917f7eec81c739d046d18e89b06052cec0c2b86f60b",
            header,
        )
    }

    @Test
    fun aComputerWithoutAnIdCannotBeSignedFor() {
        val identity = DeviceIdentity("device-1", "00".repeat(32))
        val failure = runCatching { DeviceAuth.header("mewla-phone-device", "", identity, "1", "00".repeat(16)) }.exceptionOrNull()
        assertTrue(failure is IllegalArgumentException)
    }
}
