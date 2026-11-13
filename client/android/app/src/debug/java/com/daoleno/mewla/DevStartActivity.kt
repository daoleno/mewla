package com.daoleno.mewla

import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.widget.Toast

/**
 * Debug builds only (src/debug; scripts/tests/test_android_dev_start_link.py keeps it out of every other
 * source set): `mewla-dev://start?server=ws://<host>:<port>/ws` points the Dev app at another `nsh dev`
 * server. The Dev app already runs from the server it was built with (DevBoot); this link is only for a
 * different one. The server must be on this network, and the person confirms it: the server's code runs as
 * the app, with its pairing. DevBoot remembers it, so the icon opens on it from then on.
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
    DevBoot.confirmDialog(this, server, onCancel = { finish() }) {
      startActivity(Intent(this, MainActivity::class.java)
        .putExtra(DevLaunch.EXTRA, DevLaunch.confirm(server))
        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TASK))
      finish()
    }
  }

  private fun serverOf(link: Uri): String? {
    if (link.scheme != "mewla-dev" || link.host != "start") return null
    return DevLaunch.localServer(link.getQueryParameter("server"))
  }
}
