package expo.modules.mewlaphone

import expo.modules.mewlaphone.helper.Wire
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.DataInputStream
import java.io.DataOutputStream
import java.io.IOException

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
