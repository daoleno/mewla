// Adapted from Mika (https://github.com/wangzan101/mika), Apache License 2.0. See NOTICE.
package expo.modules.mewlaphone.adb

import java.net.InetSocketAddress
import java.net.Socket
import java.security.Principal
import java.security.PrivateKey
import java.security.Provider
import java.security.cert.X509Certificate
import javax.net.ssl.SSLContext
import javax.net.ssl.SSLEngine
import javax.net.ssl.SSLSocket
import javax.net.ssl.X509ExtendedKeyManager
import javax.net.ssl.X509ExtendedTrustManager

fun interface KeyingMaterialExporter {
    fun export(socket: SSLSocket, label: String, length: Int): ByteArray
}

class AdbTls(
    val identity: AdbIdentity,
    private val exporter: KeyingMaterialExporter,
    private val provider: Provider? = null,
) {
    val context: SSLContext by lazy {
        val ctx = if (provider == null) SSLContext.getInstance("TLS") else SSLContext.getInstance("TLS", provider)
        ctx.init(arrayOf(FixedKeyManager(identity.keyPair.private, identity.certificate)), arrayOf(TrustAll), null)
        ctx
    }

    fun connect(host: String, port: Int, timeoutMs: Int): SSLSocket {
        val plain = Socket()
        try {
            plain.tcpNoDelay = true
            plain.connect(InetSocketAddress(host, port), timeoutMs)
            plain.soTimeout = timeoutMs
        } catch (e: Exception) {
            plain.close()
            throw e
        }
        return wrap(plain, host, port, client = true)
    }

    fun wrap(plain: Socket, host: String, port: Int, client: Boolean): SSLSocket {
        val s = context.socketFactory.createSocket(plain, host, port, true) as SSLSocket
        try {
            s.enabledProtocols = arrayOf("TLSv1.3")
            s.useClientMode = client
            if (!client) s.needClientAuth = true
            s.startHandshake()
        } catch (e: Exception) {
            s.close()
            throw e
        }
        return s
    }

    fun exportKeyingMaterial(socket: SSLSocket, label: String, length: Int): ByteArray = exporter.export(socket, label, length)

    private class FixedKeyManager(private val key: PrivateKey, private val cert: X509Certificate) : X509ExtendedKeyManager() {
        override fun getClientAliases(keyType: String?, issuers: Array<out Principal>?) = arrayOf(ALIAS)
        override fun chooseClientAlias(keyType: Array<out String>?, issuers: Array<out Principal>?, socket: Socket?) = ALIAS
        override fun chooseEngineClientAlias(keyType: Array<out String>?, issuers: Array<out Principal>?, engine: SSLEngine?) = ALIAS
        override fun getServerAliases(keyType: String?, issuers: Array<out Principal>?) = arrayOf(ALIAS)
        override fun chooseServerAlias(keyType: String?, issuers: Array<out Principal>?, socket: Socket?) = ALIAS
        override fun chooseEngineServerAlias(keyType: String?, issuers: Array<out Principal>?, engine: SSLEngine?) = ALIAS
        override fun getCertificateChain(alias: String?) = arrayOf(cert)
        override fun getPrivateKey(alias: String?) = key
    }

    private object TrustAll : X509ExtendedTrustManager() {
        override fun checkClientTrusted(chain: Array<out X509Certificate>?, authType: String?) {}
        override fun checkClientTrusted(chain: Array<out X509Certificate>?, authType: String?, socket: Socket?) {}
        override fun checkClientTrusted(chain: Array<out X509Certificate>?, authType: String?, engine: SSLEngine?) {}
        override fun checkServerTrusted(chain: Array<out X509Certificate>?, authType: String?) {}
        override fun checkServerTrusted(chain: Array<out X509Certificate>?, authType: String?, socket: Socket?) {}
        override fun checkServerTrusted(chain: Array<out X509Certificate>?, authType: String?, engine: SSLEngine?) {}
        override fun getAcceptedIssuers(): Array<X509Certificate> = emptyArray()
    }

    private companion object {
        const val ALIAS = "adb"
    }
}
