package com.daoleno.mewla

import android.net.Uri
import java.net.InetAddress
import java.util.UUID

/**
 * Debug builds only. The start link (DevStartActivity) points the app at another `nsh dev` server: after the
 * person confirmed it, it keeps that server in this process under a random token and starts MainActivity with
 * only the token, so another app cannot start the app on a server of its choosing (MainActivity is exported).
 */
object DevLaunch {
  const val EXTRA = "devLaunch"

  @Volatile private var confirmed: Pair<String, String>? = null

  /** The token for a server the person just confirmed. */
  fun confirm(server: String): String = UUID.randomUUID().toString().also { confirmed = it to server }

  /** The confirmed server, when the launch carries its token. */
  fun serverFor(token: String?): String? = confirmed?.takeIf { token != null && it.first == token }?.second

  /** `ws://<host>:<port>/…` on this network (loopback, a private or Tailscale address, or a `.local` name), else null. */
  fun localServer(server: String?): String? {
    val uri = Uri.parse(server?.trim()?.takeIf { it.isNotEmpty() } ?: return null)
    if (uri.scheme != "ws" && uri.scheme != "wss") return null
    val host = uri.host ?: return null
    if (uri.port <= 0 || !isLocal(host)) return null
    return server.trim()
  }

  /** "host:port", as the confirm names a server. */
  fun where(server: String): String = Uri.parse(server).let { "${it.host}:${it.port}" }

  private fun isLocal(host: String): Boolean {
    if (host == "localhost" || host.endsWith(".local")) return true
    // A literal address only: a name is not resolved here (no network on the main thread, no rebinding).
    if (!Regex("""^\d{1,3}(\.\d{1,3}){3}$""").matches(host)) return false
    val b = InetAddress.getByName(host).address.map { it.toInt() and 0xff }
    return b[0] == 127 || b[0] == 10 || (b[0] == 172 && b[1] in 16..31) || (b[0] == 192 && b[1] == 168) ||
      (b[0] == 100 && b[1] in 64..127)
  }
}
