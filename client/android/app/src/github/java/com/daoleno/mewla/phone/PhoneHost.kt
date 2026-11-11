package com.daoleno.mewla.phone

import android.content.Intent
import android.os.Handler
import android.os.Looper
import android.util.Log
import com.daoleno.mewla.MainActivity
import com.daoleno.mewla.phone.link.LinkTarget
import com.longbridge.nsh.NshHostReply
import com.longbridge.nsh.NshView
import org.json.JSONArray
import org.json.JSONObject

/**
 * `phone.*`: Phone use (the phone module, client/android/phone) behind `nsh.handle`, the live status as
 * the event `phone.status`, and the agent's screen as the component `mewla.phoneLive`. The link to the
 * computer is the module's own (it outlives this activity); the app only says which computer it follows
 * (`phone.follow`). Calls that block (pairing, the helper) run off the main thread.
 */
class PhoneHost(private val activity: MainActivity, private val nsh: NshView) {
  private val context = activity.applicationContext
  private val main = Handler(Looper.getMainLooper())
  private val stopWatching: () -> Unit

  init {
    PhoneUse.init(context)
    nsh.registerComponent("mewla.phoneLive") { _ -> PhoneLiveComponent(activity) }
    stopWatching = PhoneUse.watch { status -> main.post { nsh.send("phone.status", plain(status)) } }

    nsh.handle("phone.status") { _, reply -> answer(reply) { plain(PhoneUse.status()) } }
    nsh.handle("phone.setEnabled") { data, reply ->
      answer(reply) {
        PhoneUse.setEnabled(context, (data as? Map<*, *>)?.get("enabled") == true)
        plain(PhoneUse.status())
      }
    }
    // The one current computer, or null: the link moves with a switch and stops with none.
    nsh.handle("phone.follow") { data, reply -> answer(reply) { PhoneUse.follow(target(data)); null } }
    nsh.handle("phone.pair") { data, reply ->
      background(reply) { PhoneUse.pair(context, ((data as? Map<*, *>)?.get("code") as? String)?.trim()?.takeIf { it.isNotEmpty() }) }
    }
    nsh.handle("phone.ensureHelper") { _, reply -> background(reply) { PhoneUse.ensureHelper() } }
    nsh.handle("phone.enableAccessibility") { _, reply -> answer(reply) { PhoneUse.enableAccessibility() } }
    nsh.handle("phone.takeOver") { _, reply -> answer(reply) { PhoneUse.takeOver(); null } }
    nsh.handle("phone.giveBack") { _, reply -> answer(reply) { PhoneUse.giveBack(); null } }
    nsh.handle("phone.stop") { _, reply -> answer(reply) { PhoneUse.stop(); null } }
    nsh.handle("phone.handoffDone") { _, reply -> answer(reply) { PhoneUse.handoffDone(); null } }
    nsh.handle("phone.showPairingCodeBox") { _, reply -> answer(reply) { PhoneUse.showPairingCodeBox(context); null } }
    // The system screens setup sends the person to.
    nsh.handle("phone.open") { data, reply ->
      answer(reply) {
        when ((data as? Map<*, *>)?.get("screen")) {
          "developer" -> PhoneUse.openDeveloperSettings(activity)
          "wireless" -> PhoneUse.openWirelessDebugging(activity)
          "accessibility" -> PhoneUse.openAccessibilitySettings(activity)
          "app" -> PhoneUse.openAppSettings(activity)
          else -> throw IllegalArgumentException("screen must be developer, wireless, accessibility or app")
        }
        null
      }
    }
    nsh.handle("phone.enterPip") { data, reply -> answer(reply) { PhoneUse.enterPip(activity, int(data, "w"), int(data, "h")) } }
    nsh.handle("phone.setPipAuto") { data, reply ->
      answer(reply) { PhoneUse.setPipAuto(activity, (data as? Map<*, *>)?.get("on") == true, int(data, "w"), int(data, "h")); null }
    }
  }

  /** Back in front (from Settings, a notification, the floating window): fresh checks, a waiting link tries now. */
  fun onResume() {
    PhoneUse.takeOverFrom(activity.intent)
    PhoneUse.foreground()
    nsh.send("phone.status", plain(PhoneUse.status()))
  }

  /** Take over from a notification or the floating window arrives as the intent that opens the page. */
  fun onNewIntent(intent: Intent) = PhoneUse.takeOverFrom(intent)

  fun onUserLeaveHint() = PhoneUse.leaving(activity)

  fun onPictureInPictureModeChanged(inPip: Boolean) = PhoneUse.pipChanged(activity, inPip)

  fun onDestroy() = stopWatching()

  private fun target(data: Any?): LinkTarget? {
    val m = data as? Map<*, *> ?: return null
    val serverId = (m["serverId"] as? String)?.takeIf { it.isNotBlank() } ?: return null
    val url = (m["url"] as? String)?.takeIf { it.isNotBlank() } ?: return null
    val daemonId = (m["daemonId"] as? String)?.takeIf { it.isNotBlank() } ?: return null
    return LinkTarget(serverId, url, daemonId)
  }

  private fun int(data: Any?, name: String): Int = ((data as? Map<*, *>)?.get(name) as? Number)?.toInt() ?: 0

  private fun answer(reply: NshHostReply, work: () -> Any?) {
    try {
      reply.resolve(work())
    } catch (error: Exception) {
      Log.w(TAG, "phone call failed", error)
      reply.reject(error.message ?: error.javaClass.simpleName)
    }
  }

  private fun background(reply: NshHostReply, work: () -> Any?) {
    Thread({
      val result = runCatching(work)
      main.post {
        result.onSuccess { reply.resolve(it) }.onFailure {
          Log.w(TAG, "phone call failed", it)
          reply.reject(it.message ?: it.javaClass.simpleName)
        }
      }
    }, "mewla-phone-call").start()
  }

  private companion object {
    const val TAG = "MewlaPhone"

    /** A status as plain maps and lists, JSON's null as null. */
    fun plain(value: Any?): Any? = when (value) {
      is JSONObject -> value.keys().asSequence().associateWith { plain(value.opt(it)) }
      is JSONArray -> (0 until value.length()).map { plain(value.opt(it)) }
      JSONObject.NULL -> null
      else -> value
    }
  }
}
