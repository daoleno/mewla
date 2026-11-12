package com.daoleno.mewla

import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.widget.Toast
import java.net.InetAddress

/**
 * Debug builds only (src/debug; scripts/tests/test_android_dev_start_link.py keeps it out of every other
 * source set): `mewla-dev://start?server=ws://<host>:<port>/ws` starts the app on that `nsh dev` server, so
 * a phone needs no adb per start, only a tap on the link. The server must be on this network (loopback,
 * a private or Tailscale address, or a `.local` name), and the person confirms it: the server's code runs
 * as the app, with its pairing.
 */
class DevStartActivity : Activity() {
  override fun onCreate(state: Bundle?) {
    super.onCreate(state)
    val server = intent?.data?.let(::serverOf)
    if (server == null) {
      Toast.makeText(this, "Not a dev server on this network", Toast.LENGTH_LONG).show()
      finish()
      return
    }
    val where = Uri.parse(server).let { "${it.host}:${it.port}" }
    AlertDialog.Builder(this, android.R.style.Theme_DeviceDefault_Dialog_Alert)
      .setTitle("Load Mewla Dev from $where?")
      .setMessage("The app runs the code that server sends, until you start it from its icon again.")
      .setNegativeButton("Cancel") { _, _ -> finish() }
      .setPositiveButton("Load") { _, _ ->
        startActivity(Intent(this, MainActivity::class.java)
          .putExtra(DevLaunch.EXTRA, DevLaunch.confirm(server))
          .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TASK))
        finish()
      }
      .setOnCancelListener { finish() }
      .show()
  }

  private fun serverOf(link: Uri): String? {
    if (link.scheme != "mewla-dev" || link.host != "start") return null
    val server = link.getQueryParameter("server")?.trim() ?: return null
    val uri = Uri.parse(server)
    if (uri.scheme != "ws" && uri.scheme != "wss") return null
    val host = uri.host ?: return null
    if (uri.port <= 0 || !isLocal(host)) return null
    return server
  }

  private fun isLocal(host: String): Boolean {
    if (host == "localhost" || host.endsWith(".local")) return true
    // A literal address only: a name is not resolved here (no network on the main thread, no rebinding).
    if (!Regex("""^\d{1,3}(\.\d{1,3}){3}$""").matches(host)) return false
    val b = InetAddress.getByName(host).address.map { it.toInt() and 0xff }
    return b[0] == 127 || b[0] == 10 || (b[0] == 172 && b[1] in 16..31) || (b[0] == 192 && b[1] == 168) ||
      (b[0] == 100 && b[1] in 64..127)
  }
}
