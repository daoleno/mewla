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

class LinkTargetTest {
    private val pin = "ab".repeat(32)
    private val endpoints = listOf("wss://192.168.1.20:9876/phone/device", "wss://route.relay.example/phone/device")
    private fun relayed(url: String, pin: String? = this.pin, endpoints: List<String> = this.endpoints) =
        LinkTarget("s1", url, "d".repeat(64), pin, endpoints)

    @Test
    fun aMoveOrReconnectOfTheAppsOwnConnectionKeepsAPinnedLink() {
        // 11-19: the app's tunnel port changed on every reconnect and relay→LAN move, and Phone use was closed each time.
        val before = relayed("ws://127.0.0.1:40001/phone/device")
        assertTrue(before.sameLink(relayed("ws://127.0.0.1:40777/phone/device")))
    }

    @Test
    fun aPinnedLinkNeverConnectsToTheAppsLoopbackTunnel() {
        val target = relayed("ws://127.0.0.1:40001/phone/device")
        assertEquals(endpoints, target.connectUrls)
        assertFalse(target.connectUrls.any { it.contains("127.0.0.1") })
    }

    @Test
    fun anotherComputerPinOrAddressListStartsANewLink() {
        val target = relayed("ws://127.0.0.1:1/phone/device")
        assertFalse(target.sameLink(target.copy(serverId = "s2")))
        assertFalse(target.sameLink(relayed("ws://127.0.0.1:1/phone/device", pin = "cd".repeat(32))))
        assertFalse(target.sameLink(relayed("ws://127.0.0.1:1/phone/device", endpoints = endpoints.drop(1))))
        assertFalse(target.sameLink(null))
    }

    @Test
    fun aComputerPairedByAddressFollowsItsUrlAsBefore() {
        val plain = LinkTarget("s1", "ws://desk.local:9877/phone/device", "d".repeat(64))
        assertFalse(plain.pinned)
        assertEquals(listOf(plain.url), plain.connectUrls)
        assertTrue(plain.sameLink(plain.copy()))
        assertFalse(plain.sameLink(plain.copy(url = "ws://desk2.local:9877/phone/device")))
        // An app from before 11-19 sends no addresses: the link rides its url.
        assertFalse(relayed("ws://127.0.0.1:1/phone/device", endpoints = emptyList()).pinned)
    }
}

class PinnedTlsTest {
    private fun hex(bytes: ByteArray) = bytes.joinToString("") { "%02x".format(it) }

    @Test
    fun onlyTheKeyWhosePublicKeyInfoHashesToThePinIsTrusted() {
        val generator = java.security.KeyPairGenerator.getInstance("EC").apply { initialize(256) }
        val key = generator.generateKeyPair().public
        val other = generator.generateKeyPair().public
        val pin = hex(java.security.MessageDigest.getInstance("SHA-256").digest(key.encoded))
        assertTrue(PinnedTls.matches(key, pin))
        assertTrue(PinnedTls.matches(key, pin.uppercase()))
        assertFalse(PinnedTls.matches(other, pin))
        assertFalse(PinnedTls.matches(key, "not a pin"))
    }

    @Test
    fun anIpAddressIsNamedAsTheAppsTunnelsNameIt() {
        assertEquals("mewla-desktop.invalid", PinnedTls.serverName("192.168.1.20"))
        assertEquals("mewla-desktop.invalid", PinnedTls.serverName("[fd7a:115c::1]"))
        assertEquals("route.relay.example", PinnedTls.serverName("route.relay.example"))
    }
}

class LinkEndpointsTest {
    @Test
    fun eachFailureTriesTheNextAddressAndOnlyAWholeFailedRoundWaits() {
        val endpoints = LinkEndpoints(listOf("lan", "relay"))
        assertEquals("lan", endpoints.current)
        assertEquals(0L, endpoints.failed())
        assertEquals("relay", endpoints.current)
        assertEquals(1_000L, endpoints.failed())
        assertEquals("lan", endpoints.current)
        assertEquals(0L, endpoints.failed())
        assertEquals(2_000L, endpoints.failed())
    }

    @Test
    fun aLinkThatOpenedStartsFromTheFirstAddressAgainWithNoBackoffBuiltUp() {
        val endpoints = LinkEndpoints(listOf("lan", "relay"))
        endpoints.failed(); endpoints.failed(); endpoints.failed()
        endpoints.opened()
        assertEquals("lan", endpoints.current)
        assertEquals(0L, endpoints.failed())
        assertEquals(1_000L, endpoints.failed())
    }

    @Test
    fun oneAddressBacksOffAsTheLinkAlwaysDid() {
        val endpoints = LinkEndpoints(listOf("ws://desk/phone/device"))
        assertEquals(listOf(1_000L, 2_000L, 4_000L, 8_000L, 15_000L, 15_000L), (0..5).map { endpoints.failed() })
    }
}

/** The link's own pinned TLS against a real TLS 1.3 server with a self-made certificate, as the computer has. */
class PinnedTlsServerTest {
    private fun keystore(dir: java.io.File): java.io.File {
        val store = java.io.File(dir, "computer.p12")
        val keytool = java.io.File(System.getProperty("java.home"), "bin/keytool").path
        val process = ProcessBuilder(
            keytool, "-genkeypair", "-alias", "computer", "-keyalg", "EC", "-groupname", "secp256r1", "-dname", "CN=mewla-desktop.invalid",
            "-validity", "2", "-storetype", "PKCS12", "-keystore", store.path, "-storepass", "secret", "-keypass", "secret",
        ).redirectErrorStream(true).start()
        check(process.waitFor() == 0) { process.inputStream.bufferedReader().readText() }
        return store
    }

    private fun get(pin: String, port: Int): String {
        val trust = PinnedTls.trustManager(pin)
        val client = okhttp3.OkHttpClient.Builder()
            .sslSocketFactory(PinnedTls.socketFactory(trust), trust)
            .hostnameVerifier { _, _ -> true }
            .build()
        client.newCall(okhttp3.Request.Builder().url("https://127.0.0.1:$port/health").build()).execute().use { return it.body!!.string() }
    }

    @Test
    fun theRightPinConnectsAndAnyOtherIsRefused() {
        val dir = java.nio.file.Files.createTempDirectory("pinned-tls").toFile()
        try {
            val store = loadKeystore(dir)
            val certificate = store.getCertificate("computer")
            val pin = java.security.MessageDigest.getInstance("SHA-256").digest(certificate.publicKey.encoded).joinToString("") { "%02x".format(it) }
            val keys = javax.net.ssl.KeyManagerFactory.getInstance("PKIX").apply { init(store, "secret".toCharArray()) }
            val context = javax.net.ssl.SSLContext.getInstance("TLSv1.3").apply { init(keys.keyManagers, null, null) }
            val server = context.serverSocketFactory.createServerSocket(0, 4, java.net.InetAddress.getLoopbackAddress()) as javax.net.ssl.SSLServerSocket
            server.enabledProtocols = arrayOf("TLSv1.3")
            val names = java.util.concurrent.CopyOnWriteArrayList<String>()
            Thread {
                while (!server.isClosed) {
                    val socket = runCatching { server.accept() as javax.net.ssl.SSLSocket }.getOrNull() ?: break
                    runCatching {
                        socket.startHandshake()
                        (socket.session as? javax.net.ssl.ExtendedSSLSession)?.requestedServerNames?.forEach { names += (it as javax.net.ssl.SNIHostName).asciiName }
                        socket.inputStream.bufferedReader().let { reader -> while (reader.readLine()?.isNotEmpty() == true) Unit }
                        socket.outputStream.write("HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok".toByteArray())
                        socket.outputStream.flush()
                    }
                    runCatching { socket.close() }
                }
            }.apply { isDaemon = true }.start()
            try {
                assertEquals("ok", get(pin, server.localPort))
                // An IP address is named as the app's tunnels name it.
                assertEquals("mewla-desktop.invalid", names.firstOrNull())
                val refused = runCatching { get("00".repeat(32), server.localPort) }
                assertTrue(refused.isFailure)
            } finally {
                server.close()
            }
        } finally {
            dir.deleteRecursively()
        }
    }

    private fun loadKeystore(dir: java.io.File): java.security.KeyStore =
        java.security.KeyStore.getInstance("PKCS12").apply { keystore(dir).inputStream().use { load(it, "secret".toCharArray()) } }
}

class ForegroundRestartTest {
    @Test
    fun onlyALinkAnotherConnectionReplacedStartsAgainWhenTheAppComesToTheFront() {
        assertTrue(PhoneFrames.restartsOnForeground(PhoneFrames.closeOutcome(4001)))
        assertFalse(PhoneFrames.restartsOnForeground(PhoneFrames.closeOutcome(4002)))
        assertFalse(PhoneFrames.restartsOnForeground("online"))
        assertFalse(PhoneFrames.restartsOnForeground("offline"))
    }
}
