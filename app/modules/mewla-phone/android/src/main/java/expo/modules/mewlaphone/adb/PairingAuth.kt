// Adapted from Mika (https://github.com/wangzan101/mika), Apache License 2.0. See NOTICE.
package expo.modules.mewlaphone.adb

import java.security.SecureRandom
import javax.crypto.Cipher
import javax.crypto.Mac
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec

object Hkdf {
    fun sha256(ikm: ByteArray, salt: ByteArray?, info: ByteArray, length: Int): ByteArray {
        require(length in 1..255 * 32)
        val mac = Mac.getInstance("HmacSHA256")
        mac.init(SecretKeySpec(if (salt == null || salt.isEmpty()) ByteArray(32) else salt, "HmacSHA256"))
        val prk = mac.doFinal(ikm)
        mac.init(SecretKeySpec(prk, "HmacSHA256"))
        val out = ByteArray(length)
        var prev = ByteArray(0)
        var pos = 0
        var counter = 1
        while (pos < length) {
            mac.update(prev)
            mac.update(info)
            mac.update(counter.toByte())
            prev = mac.doFinal()
            val n = minOf(prev.size, length - pos)
            System.arraycopy(prev, 0, out, pos, n)
            pos += n
            counter++
        }
        return out
    }
}

class PairingCipher(keyMaterial: ByteArray) {
    private val key = SecretKeySpec(Hkdf.sha256(keyMaterial, null, INFO, 16), "AES")
    private var encSeq = 0L
    private var decSeq = 0L

    fun encrypt(plain: ByteArray): ByteArray = run(Cipher.ENCRYPT_MODE, encSeq++, plain)

    fun decrypt(sealed: ByteArray): ByteArray = run(Cipher.DECRYPT_MODE, decSeq, sealed).also { decSeq++ }

    private fun run(mode: Int, seq: Long, data: ByteArray): ByteArray {
        val nonce = ByteArray(12)
        for (i in 0 until 8) nonce[i] = (seq ushr (8 * i)).toByte()
        val c = Cipher.getInstance("AES/GCM/NoPadding")
        c.init(mode, key, GCMParameterSpec(128, nonce))
        return c.doFinal(data)
    }

    companion object {
        private val INFO = "adb pairing_auth aes-128-gcm key".toByteArray(Charsets.US_ASCII)
    }
}

class PairingAuth private constructor(role: Spake2.Role, password: ByteArray, seed: ByteArray?) {
    private val spake = if (role == Spake2.Role.ALICE) Spake2(role, CLIENT_NAME, SERVER_NAME) else Spake2(role, SERVER_NAME, CLIENT_NAME)
    val message: ByteArray = if (seed == null) spake.generateMessage(password, SecureRandom()) else spake.generateMessage(password, seed)
    private var cipher: PairingCipher? = null

    fun initCipher(theirMessage: ByteArray) {
        check(cipher == null)
        cipher = PairingCipher(spake.processMessage(theirMessage))
    }

    fun encrypt(data: ByteArray) = checkNotNull(cipher).encrypt(data)
    fun decrypt(data: ByteArray) = checkNotNull(cipher).decrypt(data)

    companion object {
        val CLIENT_NAME = "adb pair client\u0000".toByteArray(Charsets.US_ASCII)
        val SERVER_NAME = "adb pair server\u0000".toByteArray(Charsets.US_ASCII)
        fun client(password: ByteArray) = PairingAuth(Spake2.Role.ALICE, password, null)
        fun server(password: ByteArray) = PairingAuth(Spake2.Role.BOB, password, null)
        internal fun client(password: ByteArray, seed: ByteArray) = PairingAuth(Spake2.Role.ALICE, password, seed)
        internal fun server(password: ByteArray, seed: ByteArray) = PairingAuth(Spake2.Role.BOB, password, seed)
    }
}
