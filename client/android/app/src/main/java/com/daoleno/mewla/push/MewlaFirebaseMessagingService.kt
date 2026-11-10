package com.daoleno.mewla.push

import android.util.Log
import com.daoleno.mewla.live.CalendarAlert
import com.daoleno.mewla.live.CalendarAlertRules
import com.daoleno.mewla.live.CalendarAlerts
import com.daoleno.mewla.live.LiveController
import com.daoleno.mewla.live.WidgetSync
import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage

/**
 * Every FCM message. The daemon's data-only pushes wake this with the app closed: a
 * calendar alert is posted by the phone's own alert code (once, even when its local alarm
 * already fired) and a live state goes to the live notification and widgets. A notification
 * push reaches here only while the app is in front (otherwise the system draws it), and is
 * drawn as the system would.
 */
class MewlaFirebaseMessagingService : FirebaseMessagingService() {
  override fun onMessageReceived(message: RemoteMessage) {
    val data = Notifications.dataOf(message.data)
    calendarAlert(data)?.let { alert ->
      val shown = CalendarAlerts.fire(this, alert, "push")
      Log.d(TAG, "calendar alert push key=${alert.key} shown=$shown")
      return
    }
    if (data["mewla_live"] == "1") {
      val payload = data["payload"] ?: return
      LiveController.apply(this, payload, data["server_ref"].orEmpty())
      // A push is also the "changed, fetch now" signal for the widgets.
      WidgetSync.refreshAsync(this)
      return
    }
    val notification = message.notification
    val title = notification?.title ?: message.data["title"]
    val body = notification?.body ?: message.data["message"] ?: ""
    if (title == null) {
      Log.d(TAG, "push without anything to show, keys=${message.data.keys.sorted()}")
      return
    }
    Notifications.post(this, title, body, notification?.channelId ?: message.data["channelId"], notification?.tag ?: message.data["tag"], data)
  }

  override fun onNewToken(token: String) {
    // The app exchanges the current token for its Expo token on every connection.
    Log.d(TAG, "new FCM token")
  }

  companion object {
    private const val TAG = "MewlaPush"

    /** The calendar alert a data-only push carries (`mewla_calendar` = "1"). */
    internal fun calendarAlert(data: Map<String, String>): CalendarAlert? {
      if (data["mewla_calendar"] != "1") return null
      val itemId = data["calendar_id"] ?: return null
      val atMs = data["alert_at"]?.toLongOrNull() ?: return null
      return CalendarAlert(
        key = data["alert_key"] ?: CalendarAlertRules.key(itemId, atMs),
        itemId = itemId,
        serverId = data["server_id"].orEmpty(),
        title = data["title"].orEmpty(),
        body = data["body"].orEmpty(),
        atMs = data["at"]?.toLongOrNull() ?: atMs,
      )
    }
  }
}
