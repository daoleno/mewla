package expo.modules.mewlaphone.helper

import java.io.DataInputStream
import java.io.DataOutputStream
import java.io.EOFException

/**
 * The framing between the app and its helper over the loopback socket: one kind byte, a four-byte
 * big-endian length, then the payload. Requests and replies are JSON; live frames are bare JPEGs.
 */
object Wire {
    /** The first message from the app: the token the helper was started with. */
    const val AUTH: Byte = 0
    const val REQUEST: Byte = 1
    const val REPLY: Byte = 2
    const val FRAME: Byte = 3
    const val EVENT: Byte = 4

    /** A reply carries a screenshot as base64; the largest message stays well under this. */
    const val MAX_PAYLOAD = 16 * 1024 * 1024

    class Message(val kind: Byte, val payload: ByteArray) {
        fun text(): String = payload.toString(Charsets.UTF_8)
    }

    fun write(out: DataOutputStream, kind: Byte, payload: ByteArray) {
        synchronized(out) {
            out.writeByte(kind.toInt())
            out.writeInt(payload.size)
            out.write(payload)
            out.flush()
        }
    }

    fun write(out: DataOutputStream, kind: Byte, text: String) = write(out, kind, text.toByteArray(Charsets.UTF_8))

    /** The next message, or null when the other side closed cleanly between messages. */
    fun read(input: DataInputStream): Message? {
        val kind = try {
            input.readByte()
        } catch (_: EOFException) {
            return null
        }
        val length = input.readInt()
        if (length < 0 || length > MAX_PAYLOAD) throw java.io.IOException("bad message length $length")
        val payload = ByteArray(length)
        input.readFully(payload)
        return Message(kind, payload)
    }
}
