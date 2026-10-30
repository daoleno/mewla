package expo.modules.mewlaphone

import expo.modules.mewlaphone.helper.Wire
import org.json.JSONObject
import java.io.Closeable
import java.io.DataInputStream
import java.io.DataOutputStream
import java.io.IOException
import java.net.InetSocketAddress
import java.net.Socket
import java.util.concurrent.CompletableFuture
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.TimeUnit
import java.util.concurrent.TimeoutException
import java.util.concurrent.atomic.AtomicInteger

/** The app's end of the helper socket: requests out, replies matched by id, live frames pushed in. */
class HelperClient private constructor(
    private val socket: Socket,
    private val input: DataInputStream,
    private val out: DataOutputStream,
) : Closeable {
    private val pending = ConcurrentHashMap<Int, CompletableFuture<JSONObject>>()
    private val ids = AtomicInteger(1)
    @Volatile private var closed = false

    /** Live JPEG frames while watching; called on the reader thread. */
    @Volatile var onFrame: ((ByteArray) -> Unit)? = null
    /** Called once when the helper goes away. */
    @Volatile var onClosed: (() -> Unit)? = null

    val isClosed: Boolean get() = closed

    init {
        Thread({ readLoop() }, "mewla-helper-client").apply { isDaemon = true }.start()
    }

    private fun readLoop() {
        try {
            while (true) {
                val message = Wire.read(input) ?: break
                when (message.kind) {
                    Wire.REPLY -> {
                        val reply = JSONObject(message.text())
                        pending.remove(reply.optInt("id"))?.complete(reply)
                    }
                    Wire.FRAME -> onFrame?.invoke(message.payload)
                }
            }
        } catch (_: IOException) {
        } finally {
            close()
        }
    }

    /** Sends one request and waits for its reply; the reply's `ok` says whether the helper did it. */
    fun call(op: String, args: JSONObject = JSONObject(), timeoutMs: Long = 20_000): JSONObject {
        if (closed) throw IOException("the helper is gone")
        val id = ids.getAndIncrement()
        val future = CompletableFuture<JSONObject>()
        pending[id] = future
        try {
            Wire.write(out, Wire.REQUEST, JSONObject(args.toString()).put("id", id).put("op", op).toString())
            return future.get(timeoutMs, TimeUnit.MILLISECONDS)
        } catch (e: TimeoutException) {
            throw IOException("the helper did not answer $op", e)
        } finally {
            pending.remove(id)
        }
    }

    override fun close() {
        if (closed) return
        closed = true
        runCatching { socket.close() }
        pending.values.forEach { it.completeExceptionally(IOException("the helper is gone")) }
        pending.clear()
        onClosed?.invoke()
    }

    companion object {
        /** A client for the helper at [endpoint], or null when none is running or it refuses the token. */
        fun connect(endpoint: HelperEndpoint): HelperClient? {
            val socket = Socket()
            return try {
                socket.tcpNoDelay = true
                socket.connect(InetSocketAddress("127.0.0.1", endpoint.port), 1_000)
                socket.soTimeout = 5_000
                val input = DataInputStream(socket.getInputStream().buffered(64 * 1024))
                val out = DataOutputStream(socket.getOutputStream().buffered(64 * 1024))
                Wire.write(out, Wire.AUTH, endpoint.token)
                // The helper answers an admitted client at once and drops any other.
                val welcome = Wire.read(input)
                if (welcome == null || welcome.kind != Wire.REPLY) throw IOException("refused")
                socket.soTimeout = 0
                HelperClient(socket, input, out)
            } catch (_: IOException) {
                runCatching { socket.close() }
                null
            }
        }
    }
}
