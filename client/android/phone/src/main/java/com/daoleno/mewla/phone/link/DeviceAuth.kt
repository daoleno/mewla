package com.daoleno.mewla.phone.link

import android.content.Context
import android.content.pm.PackageManager
import com.daoleno.mewla.phone.adb.EdPoint
import java.math.BigInteger
import java.security.MessageDigest
import java.security.SecureRandom

/** The computer the link follows: the one current computer, as the app tells it (core/phone/link.ts). */
data class LinkTarget(
    val serverId: String,
    /** The socket's address: `ws(s)://<computer>/phone/device`. */
    val url: String,
    /** The computer's id, which the signature names. */
    val daemonId: String,
)

/** This phone's device key, as the app keeps it (its secure store): what signs the link's Authorization. */
class DeviceIdentity(val deviceId: String, val seedHex: String)

/**
 * Where the link reads the device key. The app implements it over its secure store and names the
 * class in this module's manifest (`com.daoleno.mewla.phone.DEVICE_KEYS`), so the link can sign with
 * no screen at all: after a reboot, or once the app was swiped away.
 */
interface DeviceKeys {
    /** The paired device key, or null when this phone has none yet. */
    fun identity(): DeviceIdentity?

    companion object {
        const val META = "com.daoleno.mewla.phone.DEVICE_KEYS"

        /** The app's key source, made from the class the manifest names (it takes a Context). */
        fun load(context: Context): DeviceKeys? = runCatching {
            val info = context.packageManager.getApplicationInfo(context.packageName, PackageManager.GET_META_DATA)
            val name = info.metaData?.getString(META) ?: return null
            Class.forName(name).getConstructor(Context::class.java).newInstance(context.applicationContext) as DeviceKeys
        }.getOrNull()
    }
}

/**
 * The `Authorization: Device v1:…` header a computer checks (daemon/auth, core/deviceAuthContract.ts):
 * Ed25519 over `purpose\ndaemonId\ndeviceId\ntimestamp\nnonce`, a fresh nonce and time each time, so
 * every connection signs again.
 */
object DeviceAuth {
    const val PURPOSE = "mewla-phone-device"
    private const val PREFIX = "Device "
    private val random = SecureRandom()

    fun header(daemonId: String, identity: DeviceIdentity, purpose: String = PURPOSE): String =
        header(purpose, daemonId, identity, System.currentTimeMillis().toString(), hex(ByteArray(16).also { random.nextBytes(it) }))

    fun header(purpose: String, daemonId: String, identity: DeviceIdentity, timestamp: String, nonceHex: String): String {
        val daemon = fixedHex(daemonId, 64) ?: throw IllegalArgumentException("Missing daemon identity.")
        val seed = fixedHex(identity.seedHex, 64) ?: throw IllegalArgumentException("Invalid device key.")
        val nonce = fixedHex(nonceHex, 32) ?: throw IllegalArgumentException("Invalid nonce.")
        val device = identity.deviceId.trim()
        val time = timestamp.trim()
        require(device.isNotEmpty() && time.isNotEmpty()) { "Invalid device authorization input." }
        val signature = sign(unhex(seed), payload(purpose, daemon, device, time, nonce))
        return "${PREFIX}v1:$device:$daemon:$time:$nonce:${hex(signature)}"
    }

    fun payload(purpose: String, daemonId: String, deviceId: String, timestamp: String, nonceHex: String): ByteArray =
        listOf(purpose.trim(), daemonId, deviceId, timestamp, nonceHex).joinToString("\n").toByteArray(Charsets.UTF_8)

    /** The Ed25519 public key of a 32-byte seed. */
    fun publicKey(seed: ByteArray): ByteArray = EdPoint.BASE.times(scalar(sha512(seed))).encode()

    /** An Ed25519 signature (RFC 8032) of [message] by the key of [seed]. */
    fun sign(seed: ByteArray, message: ByteArray): ByteArray {
        require(seed.size == 32) { "an Ed25519 seed is 32 bytes" }
        val h = sha512(seed)
        val a = scalar(h)
        val publicKey = EdPoint.BASE.times(a).encode()
        val r = EdPoint.fromLe(sha512(h.copyOfRange(32, 64), message)).mod(EdPoint.L)
        val bigR = EdPoint.BASE.times(r).encode()
        val k = EdPoint.fromLe(sha512(bigR, publicKey, message)).mod(EdPoint.L)
        val s = (r + k * a).mod(EdPoint.L)
        return bigR + EdPoint.toLe(s, 32)
    }

    private fun scalar(h: ByteArray): BigInteger {
        val a = h.copyOfRange(0, 32)
        a[0] = (a[0].toInt() and 248).toByte()
        a[31] = ((a[31].toInt() and 127) or 64).toByte()
        return EdPoint.fromLe(a)
    }

    private fun sha512(vararg parts: ByteArray): ByteArray {
        val digest = MessageDigest.getInstance("SHA-512")
        parts.forEach { digest.update(it) }
        return digest.digest()
    }

    /** Lowercase hex of exactly [length] digits, or null (core/protocolCrypto.ts normalizeFixedHex). */
    fun fixedHex(value: String?, length: Int): String? =
        value?.trim()?.lowercase()?.takeIf { it.length == length && it.all { c -> c in '0'..'9' || c in 'a'..'f' } }

    fun hex(bytes: ByteArray): String = bytes.joinToString("") { "%02x".format(it) }

    fun unhex(value: String): ByteArray = ByteArray(value.length / 2) { value.substring(it * 2, it * 2 + 2).toInt(16).toByte() }
}
