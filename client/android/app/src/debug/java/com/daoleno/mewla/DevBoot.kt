package com.daoleno.mewla

import android.app.Activity
import android.app.AlertDialog
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.util.Log
import com.longbridge.nsh.NshView
import java.net.InetSocketAddress
import java.net.Socket

/**
 * Debug builds only (src/release has a DevBoot that does nothing): the Dev app is live by itself. Opened from
 * its icon it runs from an `nsh dev` server, so a save on that server shows on the phone without a link or a
 * new APK. The server is the one the build was given (BuildConfig.DEV_SERVER, `-Pmewla.devServer`, which
 * client/scripts/dev-android.sh takes from the host that builds it), or the last one the start link pointed it
 * at (DevStartActivity).
 *
 * - The first open on a server asks once ("Load Mewla Dev from <host>:<port>?"): its code runs as the app,
 *   with its pairing. After Load the server is remembered and never asked about again.
 * - Every open after that connects straight away when the server answers within two seconds. When it does not
 *   (the phone is away from it, the server is down), the app starts on its built-in bundle, with no dialog,
 *   and looks again each time it comes to the front: once the server answers, it reopens on it.
 */
object DevBoot {
  private const val TAG = "MewlaDev"
  private const val PREFS = "mewla-dev"
  private const val LAST = "last"
  private const val CONFIRMED = "confirmed"
  private const val ANSWER_MS = 2_000

  /** Whether this activity runs on the built-in bundle while a dev server is configured (it looks again on resume). */
  @Volatile private var waiting: String? = null

  /** Starts the app: on its dev server when there is one, else returns false (the caller loads the bundle). */
  fun start(activity: Activity, nsh: NshView, intent: Intent?): Boolean {
    waiting = null
    val prefs = activity.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
    // The start link: the person confirmed this server a moment ago.
    DevLaunch.serverFor(intent?.getStringExtra(DevLaunch.EXTRA))?.let { server ->
      remember(prefs, server)
      if (!nsh.connectDevServer(server)) nsh.loadAssetBundle("nsh")
      return true
    }
    val server = DevLaunch.localServer(prefs.getString(LAST, null)) ?: DevLaunch.localServer(BuildConfig.DEV_SERVER) ?: return false
    if (server in confirmed(prefs)) {
      connectIfAnswering(activity, nsh, server)
      return true
    }
    confirmDialog(activity, server, onCancel = { nsh.loadAssetBundle("nsh") }) {
      remember(prefs, server)
      connectIfAnswering(activity, nsh, server)
    }
    return true
  }

  /** The app came to the front: if it waits on the built-in bundle and its server answers now, reopen on it. */
  fun resumed(activity: Activity) {
    val server = waiting ?: return
    Thread {
      if (answers(server) && waiting == server) activity.runOnUiThread {
        if (!activity.isFinishing && !activity.isDestroyed && waiting == server) {
          waiting = null
          activity.recreate()
        }
      }
    }.start()
  }

  /** The one confirm for a server: its code runs as the app. */
  fun confirmDialog(activity: Activity, server: String, onCancel: () -> Unit, onLoad: () -> Unit) {
    var decided = false
    AlertDialog.Builder(activity, android.R.style.Theme_DeviceDefault_Dialog_Alert)
      .setTitle("Load Mewla Dev from ${DevLaunch.where(server)}?")
      .setMessage("The app runs the code that server sends, with its pairing. Mewla Dev remembers it and opens on it from then on.")
      .setNegativeButton("Cancel") { _, _ -> decided = true; onCancel() }
      .setPositiveButton("Load") { _, _ -> decided = true; onLoad() }
      .setOnDismissListener { if (!decided) onCancel() }
      .show()
  }

  private fun connectIfAnswering(activity: Activity, nsh: NshView, server: String) {
    Thread {
      val up = answers(server)
      activity.runOnUiThread {
        if (activity.isFinishing || activity.isDestroyed) return@runOnUiThread
        if (up && nsh.connectDevServer(server)) return@runOnUiThread
        Log.i(TAG, "${DevLaunch.where(server)} did not answer; running the built-in bundle")
        nsh.loadAssetBundle("nsh")
        waiting = server
      }
    }.start()
  }

  /** Whether the server takes a connection within two seconds. */
  private fun answers(server: String): Boolean {
    val uri = Uri.parse(server)
    return runCatching { Socket().use { it.connect(InetSocketAddress(uri.host, uri.port), ANSWER_MS) } }.isSuccess
  }

  private fun confirmed(prefs: android.content.SharedPreferences): Set<String> = prefs.getStringSet(CONFIRMED, emptySet()) ?: emptySet()

  private fun remember(prefs: android.content.SharedPreferences, server: String) {
    prefs.edit().putString(LAST, server).putStringSet(CONFIRMED, confirmed(prefs) + server).apply()
  }
}
