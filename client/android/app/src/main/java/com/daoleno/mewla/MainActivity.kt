package com.daoleno.mewla

import android.content.Intent
import android.os.Bundle
import android.provider.Settings
import android.util.Log
import androidx.activity.ComponentActivity
import androidx.core.view.WindowCompat
import com.daoleno.mewla.live.LiveHost
import com.daoleno.mewla.push.PushHost
import com.daoleno.mewla.terminal.TerminalComponent
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
  private lateinit var push: PushHost
  private lateinit var channelHosts: ChannelHosts
  private lateinit var watchdog: JsWatchdog

  override fun onCreate(state: Bundle?) {
    super.onCreate(state)
    nsh = NshView(this)
    nsh.setErrorListener { Log.e(TAG, it); CrashLog.js(this, it) }
    CrashLog.register(this, nsh)
    watchdog = JsWatchdog(this, nsh)
    ScanHost(this, nsh)
    com.daoleno.mewla.link.LinkTransportHost(nsh)
    nsh.registerComponent("mewla.qrScanner") { events -> QrScanner(this, events) }
    nsh.registerComponent("mewla.terminal") { events -> TerminalComponent(this, events) }
    nsh.handle("secure.get") { data, reply -> answer(reply) { secure.get(key(data)) } }
    nsh.handle("secure.set") { data, reply -> answer(reply) { secure.set(key(data), field(data, "value")); null } }
    nsh.handle("secure.remove") { data, reply -> answer(reply) { secure.remove(key(data)); null } }
    nsh.handle("app.legacyStorage") { _, reply -> answer(reply) { legacy.entries() } }
    // The navigation bar's buttons for the app's appearance (App.tsx); the theme sets them only at start.
    nsh.handle("app.navigationBar") { data, reply ->
      answer(reply) {
        val dark = (data as? Map<*, *>)?.get("dark") == true
        runOnUiThread { WindowCompat.getInsetsController(window, window.decorView).isAppearanceLightNavigationBars = !dark }
        null
      }
    }
    nsh.handle("app.legacyStorageCopied") { _, reply -> answer(reply) { legacy.copied(); null } }
    push = PushHost(this, nsh)
    LiveHost(this, nsh)
    com.daoleno.mewla.files.FilesHost(this, nsh)
    channelHosts = ChannelHosts(this, nsh)
    nsh.setParams(mapOf("deviceName" to deviceName()))
    setContentView(nsh)
    // The Dev app runs from its `nsh dev` server when it answers (src/debug/DevBoot.kt); a release build has none.
    if (!DevBoot.start(this, nsh, intent)) nsh.loadAssetBundle("nsh")
    openLink(intent)
  }

  override fun onNewIntent(intent: Intent) {
    super.onNewIntent(intent)
    setIntent(intent)
    channelHosts.onNewIntent(intent)
    openLink(intent)
  }

  override fun onResume() {
    super.onResume()
    channelHosts.onResume()
    DevBoot.resumed(this)
    watchdog.resumed()
  }

  override fun onPause() {
    watchdog.paused()
    super.onPause()
  }

  override fun onUserLeaveHint() {
    super.onUserLeaveHint()
    channelHosts.onUserLeaveHint()
  }

  override fun onPictureInPictureModeChanged(inPip: Boolean, config: android.content.res.Configuration) {
    super.onPictureInPictureModeChanged(inPip, config)
    channelHosts.onPictureInPictureModeChanged(inPip, config)
  }

  /** A `mewla://` link the system opened us with; the app holds the event until it listens. */
  private fun openLink(intent: Intent?) {
    push.opened(intent)
    val url = intent?.takeIf { it.action == Intent.ACTION_VIEW }?.dataString ?: return
    nsh.send("links.open", mapOf("url" to url))
  }

  override fun onDestroy() {
    channelHosts.onDestroy()
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
  }
}
