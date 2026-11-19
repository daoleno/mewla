package com.daoleno.mewla.phone.link

import java.net.InetAddress
import java.net.Socket
import java.security.MessageDigest
import java.security.PublicKey
import java.security.SecureRandom
import java.security.cert.CertificateException
import java.security.cert.X509Certificate
import javax.net.ssl.SNIHostName
import javax.net.ssl.SSLContext
import javax.net.ssl.SSLSocket
import javax.net.ssl.SSLSocketFactory
import javax.net.ssl.X509TrustManager

/**
 * Phone use's own pinned TLS to the computer (Mewla Relay or its LAN/Tailscale address), as the app's tunnels do it
 * (app/link/LinkTransportHost.kt): TLS 1.3, trusting only a leaf whose SubjectPublicKeyInfo SHA-256 is the pin the
 * computer's QR code carried, and the same server name. The link no longer rides the app's loopback tunnel.
 */
object PinnedTls {
    /** Whether [key]'s SubjectPublicKeyInfo hashes to [pinHex]. */
    fun matches(key: PublicKey, pinHex: String): Boolean {
        val expected = decodePin(pinHex) ?: return false
        return MessageDigest.isEqual(MessageDigest.getInstance("SHA-256").digest(key.encoded), expected)
    }

    fun trustManager(pinHex: String): X509TrustManager {
        require(decodePin(pinHex) != null) { "Mewla Relay SPKI pin is invalid." }
        return object : X509TrustManager {
            override fun getAcceptedIssuers(): Array<X509Certificate> = emptyArray()

            override fun checkClientTrusted(chain: Array<X509Certificate>, authType: String) {
                throw CertificateException("Client certificates are unsupported.")
            }

            override fun checkServerTrusted(chain: Array<X509Certificate>, authType: String) {
                val leaf = chain.firstOrNull() ?: throw CertificateException("The computer sent no certificate.")
                if (!matches(leaf.publicKey, pinHex)) throw CertificateException("The computer's transport certificate does not match its pin.")
            }
        }
    }

    /** Sockets that speak TLS 1.3 with the pinned trust and name the server as the app's tunnels do. */
    fun socketFactory(trust: X509TrustManager): SSLSocketFactory {
        val context = SSLContext.getInstance("TLSv1.3").apply { init(null, arrayOf(trust), SecureRandom()) }
        return NamedSocketFactory(context.socketFactory)
    }

    /** An IP literal has no name to send; the computer's Link certificate answers this one (as `pinnedServerName`). */
    fun serverName(host: String): String {
        val literal = host.trim().removePrefix("[").removeSuffix("]")
        return if (':' in literal || literal.matches(Regex("""^\d{1,3}(?:\.\d{1,3}){3}$"""))) "mewla-desktop.invalid" else host
    }

    private fun decodePin(pinHex: String): ByteArray? {
        if (!pinHex.matches(Regex("^[0-9a-fA-F]{64}$"))) return null
        return pinHex.lowercase().chunked(2).map { it.toInt(16).toByte() }.toByteArray()
    }

    private class NamedSocketFactory(private val inner: SSLSocketFactory) : SSLSocketFactory() {
        override fun getDefaultCipherSuites(): Array<String> = inner.defaultCipherSuites
        override fun getSupportedCipherSuites(): Array<String> = inner.supportedCipherSuites

        override fun createSocket(s: Socket, host: String, port: Int, autoClose: Boolean): Socket =
            named(inner.createSocket(s, serverName(host), port, autoClose), host)

        override fun createSocket(host: String, port: Int): Socket = named(inner.createSocket(host, port), host)
        override fun createSocket(host: String, port: Int, localHost: InetAddress, localPort: Int): Socket =
            named(inner.createSocket(host, port, localHost, localPort), host)
        override fun createSocket(host: InetAddress, port: Int): Socket = inner.createSocket(host, port)
        override fun createSocket(address: InetAddress, port: Int, localAddress: InetAddress, localPort: Int): Socket =
            inner.createSocket(address, port, localAddress, localPort)

        private fun named(socket: Socket, host: String): Socket {
            val ssl = socket as? SSLSocket ?: return socket
            ssl.enabledProtocols = arrayOf("TLSv1.3")
            val parameters = ssl.sslParameters
            parameters.serverNames = listOf(SNIHostName(serverName(host)))
            ssl.sslParameters = parameters
            return ssl
        }
    }
}

/**
 * Which of the computer's addresses the link tries next. It starts from the first (a direct address) each time the
 * link was up, moves on after each failure, and waits the link's backoff only once every address failed in a round.
 */
class LinkEndpoints(private val urls: List<String>) {
    private var index = 0
    private var failedRounds = 0

    init {
        require(urls.isNotEmpty()) { "the link needs an address" }
    }

    val current: String get() = urls[index]

    /** The link opened on [current]: next time it starts from the first again. */
    fun opened() {
        index = 0
        failedRounds = 0
    }

    /** [current] failed (or an open link on it ended): the next address, and how long to wait before trying it. */
    fun failed(): Long {
        index += 1
        if (index < urls.size) return 0
        index = 0
        return LinkBackoff.delayMs(failedRounds++)
    }
}
