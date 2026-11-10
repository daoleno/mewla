package com.daoleno.mewla.push

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import com.daoleno.mewla.MainActivity
import org.json.JSONObject

/**
 * The pushes Expo's service sends: title, body, `channelId` and the daemon's routing
 * `data`. With the app in the background the system draws them itself (channel, icon and
 * color from the manifest's Firebase meta-data) and a tap starts the app with the data as
 * extras; in front, [post] draws them the same way. Either tap reaches the app as
 * `push.opened`. The channel ids are the Expo app's: a person's settings for them survive
 * the upgrade.
 */
internal object Notifications {
  const val AGENTS = "mewla-agents"
  /** The extra our own notifications carry: the push's data as JSON. */
  const val EXTRA_DATA = "mewla_push_data"

  /** Android 13+ shows the permission prompt only once the app owns a channel. */
  fun ensureChannels(context: Context) {
    val manager = context.getSystemService(NotificationManager::class.java)
    if (manager.getNotificationChannel(AGENTS) != null) return
    manager.createNotificationChannel(
      NotificationChannel(AGENTS, "Worker Alerts", NotificationManager.IMPORTANCE_HIGH).apply {
        vibrationPattern = longArrayOf(0, 250, 250, 250)
        enableVibration(true)
      },
    )
  }

  fun post(context: Context, title: String, body: String, channel: String?, tag: String?, data: Map<String, String>) {
    ensureChannels(context)
    val json = JSONObject(data as Map<*, *>).toString()
    val open = Intent(context, MainActivity::class.java)
      .setAction(Intent.ACTION_MAIN)
      .putExtra(EXTRA_DATA, json)
      .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP)
    val id = (tag ?: json).hashCode()
    val notification = NotificationCompat.Builder(context, channel?.takeIf { it.isNotEmpty() } ?: AGENTS)
      .setSmallIcon(smallIcon(context))
      .setColor(0xFFC8372B.toInt())
      .setContentTitle(title)
      .setContentText(body)
      .setStyle(NotificationCompat.BigTextStyle().bigText(body))
      .setPriority(NotificationCompat.PRIORITY_HIGH)
      .setAutoCancel(true)
      .setContentIntent(PendingIntent.getActivity(context, id, open, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE))
      .build()
    try {
      NotificationManagerCompat.from(context).notify(tag, id, notification)
    } catch (_: SecurityException) {
      // Notifications not allowed: the app shows nothing, as the system would.
    }
  }

  /** The routing data of a tapped push, from the intent that opened the app; null for any other start. */
  fun tapped(intent: Intent?): Map<String, String>? {
    val extras = intent?.extras ?: return null
    extras.getString(EXTRA_DATA)?.let { return parse(it) }
    // A notification the system drew: FCM's keys plus Expo's, the data as JSON under `body`.
    if (extras.getString("google.message_id") == null && extras.getString("body") == null) return null
    extras.getString("body")?.let { body -> parse(body)?.let { return it } }
    return extras.keySet().filter { !it.startsWith("google.") && !it.startsWith("gcm.") }
      .mapNotNull { key -> extras.getString(key)?.let { key to it } }.toMap()
  }

  /** Expo's wrapping: the push's `data` as a JSON string under `body`; else the keys themselves. */
  fun dataOf(data: Map<String, String>): Map<String, String> = data["body"]?.let(::parse) ?: data

  private fun parse(json: String): Map<String, String>? = try {
    val o = JSONObject(json)
    o.keys().asSequence().associateWith { o.get(it).toString() }
  } catch (_: Exception) {
    null
  }

  private fun smallIcon(context: Context): Int {
    val id = context.resources.getIdentifier("notification_icon", "drawable", context.packageName)
    return if (id != 0) id else context.applicationInfo.icon
  }
}
