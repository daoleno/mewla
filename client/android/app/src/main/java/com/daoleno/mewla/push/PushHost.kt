package com.daoleno.mewla.push

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.util.Log
import androidx.activity.ComponentActivity
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.app.NotificationManagerCompat
import com.google.firebase.FirebaseApp
import com.google.firebase.messaging.FirebaseMessaging
import com.longbridge.nsh.NshHostReply
import com.longbridge.nsh.NshView

/**
 * `push.*`: whether notifications are allowed, the system prompt, and the device's
 * FCM token with the Expo installation id, which the app exchanges for its Expo token
 * (core/push). A tapped push reaches the app as the event `push.opened {data}`.
 */
class PushHost(private val activity: ComponentActivity, private val nsh: NshView) {
  private var asking: NshHostReply? = null
  private val prompt = activity.registerForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
    asking?.resolve(granted)
    asking = null
  }

  init {
    Notifications.ensureChannels(activity)
    nsh.handle("push.permission") { _, reply -> reply.resolve(mapOf("granted" to allowed())) }
    nsh.handle("push.requestPermission") { _, reply ->
      if (allowed() || Build.VERSION.SDK_INT < 33) return@handle reply.resolve(allowed())
      asking?.resolve(false)
      asking = reply
      prompt.launch(Manifest.permission.POST_NOTIFICATIONS)
    }
    nsh.handle("push.deviceToken") { _, reply ->
      if (FirebaseApp.getApps(activity).isEmpty()) return@handle reply.reject("This build has no push service.")
      FirebaseMessaging.getInstance().token.addOnCompleteListener { task ->
        val token = task.result.takeIf { task.isSuccessful }
        if (token == null) {
          Log.w(TAG, "no FCM token", task.exception)
          reply.reject(task.exception?.message ?: "No push token.")
          return@addOnCompleteListener
        }
        reply.resolve(
          mapOf(
            "type" to "fcm",
            "token" to token,
            "deviceId" to InstallationId.get(activity),
            "appId" to activity.packageName,
            "development" to false,
          ),
        )
      }
    }
  }

  /** The intent that started or reached the activity: a tapped push becomes `push.opened`. */
  fun opened(intent: Intent?) {
    val data = Notifications.tapped(intent) ?: return
    // Handled once: a recreated activity gets the same intent again.
    listOf(Notifications.EXTRA_DATA, "body", "google.message_id").forEach { intent?.removeExtra(it) }
    nsh.send("push.opened", mapOf("data" to data))
  }

  private fun allowed(): Boolean =
    NotificationManagerCompat.from(activity).areNotificationsEnabled() &&
      (Build.VERSION.SDK_INT < 33 || activity.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED)

  private companion object {
    const val TAG = "MewlaPush"
  }
}
