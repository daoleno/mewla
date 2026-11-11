package com.daoleno.mewla.link

import android.os.Handler
import android.os.Looper
import com.longbridge.nsh.NshView
import java.io.Closeable
import java.io.IOException
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.ServerSocket
import java.net.Socket
import java.security.MessageDigest
import java.security.SecureRandom
import java.security.cert.CertificateException
import java.security.cert.X509Certificate
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.RejectedExecutionException
import java.util.concurrent.Semaphore
import java.util.concurrent.atomic.AtomicBoolean
import javax.net.ssl.SNIHostName
import javax.net.ssl.SSLContext
import javax.net.ssl.SSLSocket
import javax.net.ssl.X509TrustManager

/**
 * Mewla Link's pinned tunnels (ported from the Expo app's link-transport module): a loopback
 * listener per key whose every connection is bridged to TLS 1.3 at host:port, trusting only a
 * leaf whose SubjectPublicKeyInfo SHA-256 is the pin the computer's QR code carried.
 * - `link.start {key, host, port, pin, mode}` → `{port, rttMs}`; mode `measure` first does one
 *   pinned handshake and reports its time, `on-demand` only listens. The same key with the same
 *   endpoint returns the running tunnel.
 * - `link.stop {key}`, `link.stopAll`.
 * - `link.events {key}` → `[]`: blocking sockets raise their failure to the caller directly.
 */
class LinkTransportHost(nsh: NshView) {
  private val tunnels = ConcurrentHashMap<String, PinnedProxy>()
  // start() dials and handshakes: never on the thread that delivers host calls.
  private val calls = Executors.newCachedThreadPool { task -> Thread(task, "link-transport-call").apply { isDaemon = true } }
  private val main = Handler(Looper.getMainLooper())

  init {
    nsh.handle("link.start") { data, reply ->
      val input = data as? Map<*, *> ?: return@handle reply.reject("Mewla Link tunnel input is missing.")
      calls.execute {
        val result = runCatching { start(input) }
        main.post { result.fold({ reply.resolve(it) }, { reply.reject(it.message ?: "Mewla Link could not start.") }) }
      }
    }
    nsh.handle("link.stop") { data, reply ->
      val key = (data as? Map<*, *>)?.get("key") as? String
      if (key != null) calls.execute { tunnels.remove(key)?.close() }
      reply.resolve(null)
    }
    nsh.handle("link.stopAll") { _, reply ->
      val current = tunnels.values.toList()
      tunnels.clear()
      calls.execute { current.forEach { it.close() } }
      reply.resolve(null)
    }
    nsh.handle("link.events") { _, reply -> reply.resolve(emptyList<String>()) }
  }

  private fun start(input: Map<*, *>): Map<String, Int> {
    val key = input["key"] as? String ?: ""
    val host = input["host"] as? String ?: ""
    val port = (input["port"] as? Number)?.toInt() ?: 0
    val pin = input["pin"] as? String ?: ""
    val mode = input["mode"] as? String ?: ""
    require(key.isNotBlank()) { "Mewla Link tunnel key is required." }
    require(host.isNotBlank() && !host.contains('/') && !host.contains('\u0000')) { "Mewla Link host is invalid." }
    require(port in 1..65535) { "Mewla Link port is invalid." }
    require(pin.matches(Regex("^[0-9a-fA-F]{64}$"))) { "Mewla Link SPKI pin is invalid." }
    require(mode == "measure" || mode == "on-demand") { "Mewla Link tunnel mode is invalid." }
    tunnels[key]?.let { existing ->
      if (existing.matches(host, port, pin, mode)) return mapOf("port" to existing.localPort, "rttMs" to existing.lastRttMs)
      existing.close()
      tunnels.remove(key, existing)
    }
    val proxy = PinnedProxy(host, port, pin, mode == "measure")
    try {
      proxy.start()
    } catch (error: Throwable) {
      proxy.close()
      throw IllegalStateException("Mewla Link could not reach a pinned relay candidate.", error)
    }
    tunnels[key] = proxy
    return mapOf("port" to proxy.localPort, "rttMs" to proxy.lastRttMs)
  }
}

internal class PinnedProxy(
  private val host: String,
  private val port: Int,
  private val pin: String,
  private val measureBeforeListen: Boolean,
) : Closeable {
  private val running = AtomicBoolean(false)
  private val worker = Executors.newCachedThreadPool { task -> Thread(task, "link-transport").apply { isDaemon = true } }
  private val connectionLimit = Semaphore(64)
  private val openSockets = ConcurrentHashMap.newKeySet<Socket>()
  private val sslContext = pinnedSSLContext(pin)
  private lateinit var listener: ServerSocket

  var localPort: Int = 0
    private set
  var lastRttMs: Int = 0
    private set

  fun matches(otherHost: String, otherPort: Int, otherPin: String, mode: String): Boolean =
    host == otherHost && port == otherPort && pin.equals(otherPin, ignoreCase = true) &&
      measureBeforeListen == (mode == "measure") && running.get()

  fun start() {
    if (measureBeforeListen) {
      val startedAt = System.nanoTime()
      createRemoteSocket().use { it.startHandshake() }
      lastRttMs = maxOf(1, ((System.nanoTime() - startedAt) / 1_000_000L).toInt())
    }
    listener = ServerSocket()
    listener.reuseAddress = true
    listener.bind(InetSocketAddress(InetAddress.getByName("127.0.0.1"), 0), 64)
    localPort = listener.localPort
    running.set(true)
    worker.execute { acceptLoop() }
  }

  private fun acceptLoop() {
    while (running.get()) {
      val local = try {
        listener.accept()
      } catch (_: Throwable) {
        close()
        break
      }
      if (!connectionLimit.tryAcquire()) {
        closeQuietly(local)
        continue
      }
      openSockets.add(local)
      try {
        worker.execute {
          try {
            bridge(local)
          } catch (_: IOException) {
            // A failed or cancelled connection must not kill the app.
          } catch (_: RejectedExecutionException) {
            // close() can stop the pool while this owner starts its pumps.
          } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
          } finally {
            openSockets.remove(local)
            closeQuietly(local)
            connectionLimit.release()
          }
        }
      } catch (_: RejectedExecutionException) {
        openSockets.remove(local)
        closeQuietly(local)
        connectionLimit.release()
      }
    }
  }

  private fun bridge(local: Socket) {
    val remote = createRemoteSocket()
    openSockets.add(remote)
    try {
      remote.startHandshake()
      remote.soTimeout = 0
      val done = CountDownLatch(2)
      worker.execute { pumpSocket(local, remote, done) }
      worker.execute { pumpSocket(remote, local, done) }
      done.await()
    } finally {
      openSockets.remove(remote)
      closeQuietly(remote)
    }
  }

  private fun createRemoteSocket(): SSLSocket {
    val plain = Socket()
    try {
      plain.tcpNoDelay = true
      plain.keepAlive = true
      plain.connect(InetSocketAddress(host, port), 5_000)
      val socket = sslContext.socketFactory.createSocket(plain, pinnedServerName(host), port, true) as SSLSocket
      socket.enabledProtocols = arrayOf("TLSv1.3")
      val parameters = socket.sslParameters
      parameters.serverNames = listOf(SNIHostName(pinnedServerName(host)))
      socket.sslParameters = parameters
      socket.soTimeout = 15_000
      return socket
    } catch (error: Throwable) {
      closeQuietly(plain)
      throw error
    }
  }

  override fun close() {
    if (!running.getAndSet(false) && !::listener.isInitialized) {
      worker.shutdownNow()
      return
    }
    if (::listener.isInitialized) closeQuietly(listener)
    openSockets.forEach(::closeQuietly)
    openSockets.clear()
    worker.shutdownNow()
  }
}

internal fun pumpSocket(source: Socket, destination: Socket, done: CountDownLatch) {
  try {
    source.getInputStream().copyTo(destination.getOutputStream(), 32 * 1024)
    runCatching { destination.shutdownOutput() }
  } catch (_: IOException) {
    // Wake the other pump as well; a reset cannot leave its owner waiting.
    closeQuietly(source)
    closeQuietly(destination)
  } finally {
    done.countDown()
  }
}

private fun pinnedSSLContext(pinHex: String): SSLContext {
  val expected = pinHex.lowercase().chunked(2).map { it.toInt(16).toByte() }.toByteArray()
  val trustManager = object : X509TrustManager {
    override fun getAcceptedIssuers(): Array<X509Certificate> = emptyArray()

    override fun checkClientTrusted(chain: Array<X509Certificate>, authType: String) {
      throw CertificateException("Client certificates are unsupported.")
    }

    override fun checkServerTrusted(chain: Array<X509Certificate>, authType: String) {
      val leaf = chain.firstOrNull() ?: throw CertificateException("Mewla Link peer sent no certificate.")
      val actual = MessageDigest.getInstance("SHA-256").digest(leaf.publicKey.encoded)
      if (!MessageDigest.isEqual(actual, expected)) throw CertificateException("Mewla Link transport certificate pin mismatch.")
    }
  }
  return SSLContext.getInstance("TLSv1.3").apply { init(null, arrayOf(trustManager), SecureRandom()) }
}

/** An IP literal has no name to send; the computer's Link certificate answers this one. */
internal fun pinnedServerName(host: String): String {
  val literal = host.trim().removePrefix("[").removeSuffix("]")
  return if (':' in literal || literal.matches(Regex("""^\d{1,3}(?:\.\d{1,3}){3}$"""))) "mewla-desktop.invalid" else host
}

private fun closeQuietly(closeable: Closeable) {
  runCatching { closeable.close() }
}
