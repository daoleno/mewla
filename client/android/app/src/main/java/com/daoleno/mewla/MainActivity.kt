package com.daoleno.mewla

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Bundle
import android.provider.Settings
import android.util.Log
import androidx.activity.ComponentActivity
import com.longbridge.nsh.NshHostReply
import com.longbridge.nsh.NshView

/**
 * The whole app is client/ui on NSH; this host gives it what only the platform has:
 * the secure store, the camera reader, the links the system opens and, in a debug
 * build, hot reload from `nsh dev`.
 */
class MainActivity : ComponentActivity() {
  private lateinit var nsh: NshView
  private val secure by lazy { SecureStore(this) }
  private val legacy by lazy { LegacyStorage(this) }

  override fun onCreate(state: Bundle?) {
    super.onCreate(state)
    nsh = NshView(this)
    nsh.setErrorListener { Log.e(TAG, it) }
    nsh.registerComponent("mewla.qrScanner") { events -> QrScanner(this, events) }
    nsh.handle("secure.get") { data, reply -> answer(reply) { secure.get(key(data)) } }
    nsh.handle("secure.set") { data, reply -> answer(reply) { secure.set(key(data), field(data, "value")); null } }
    nsh.handle("secure.remove") { data, reply -> answer(reply) { secure.remove(key(data)); null } }
    nsh.handle("app.legacyStorage") { _, reply -> answer(reply) { legacy.entries() } }
    nsh.handle("app.legacyStorageCopied") { _, reply -> answer(reply) { legacy.copied(); null } }
    nsh.setParams(mapOf("deviceName" to deviceName()))
    setContentView(nsh)
    // scripts/dev-android.sh starts a debug build on `nsh dev`: every save reloads the app.
    val dev = intent.getStringExtra("devServer")
    if (!(BuildConfig.DEBUG && dev != null && nsh.connectDevServer(dev))) nsh.loadAssetBundle("nsh")
    openLink(intent)
  }

  override fun onNewIntent(intent: Intent) {
    super.onNewIntent(intent)
    setIntent(intent)
    openLink(intent)
  }

  /** A `mewla://` link the system opened us with; the app holds the event until it listens. */
  private fun openLink(intent: Intent?) {
    val url = intent?.takeIf { it.action == Intent.ACTION_VIEW }?.dataString ?: return
    nsh.send("links.open", mapOf("url" to url))
  }

  fun cameraAllowed(): Boolean {
    if (checkSelfPermission(Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED) return true
    requestPermissions(arrayOf(Manifest.permission.CAMERA), CAMERA_REQUEST)
    return false
  }

  override fun onRequestPermissionsResult(code: Int, permissions: Array<out String>, results: IntArray) {
    super.onRequestPermissionsResult(code, permissions, results)
    if (code == CAMERA_REQUEST) QrScanner.cameraAnswered(results.firstOrNull() == PackageManager.PERMISSION_GRANTED)
  }

  override fun onDestroy() {
    nsh.destroy()
    super.onDestroy()
  }

  /** What the person named the phone, as the computer lists it after pairing. */
  private fun deviceName(): String =
    Settings.Global.getString(contentResolver, Settings.Global.DEVICE_NAME)?.takeIf { it.isNotBlank() }
      ?: listOf(android.os.Build.MANUFACTURER.replaceFirstChar { it.uppercase() }, android.os.Build.MODEL).joinToString(" ")

  private fun key(data: Any?) = field(data, "key")

  private fun field(data: Any?, name: String): String =
    (data as? Map<*, *>)?.get(name) as? String ?: throw IllegalArgumentException("$name is required")

  private fun answer(reply: NshHostReply, work: () -> Any?) {
    try {
      reply.resolve(work())
    } catch (error: Exception) {
      Log.w(TAG, "host call failed", error)
      reply.reject(error.message ?: error.javaClass.simpleName)
    }
  }

  private companion object {
    const val TAG = "Mewla"
    const val CAMERA_REQUEST = 1
  }
}
