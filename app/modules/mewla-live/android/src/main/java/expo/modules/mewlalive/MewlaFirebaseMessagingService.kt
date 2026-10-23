package expo.modules.mewlalive

import android.util.Log
import com.google.firebase.messaging.RemoteMessage
import expo.modules.notifications.service.ExpoFirebaseMessagingService
import org.json.JSONObject

/**
 * Replaces Expo's FCM service (the config plugin removes Expo's registration) so a live_state
 * data message is handled natively, with the app closed, and everything else goes to Expo as before.
 */
class MewlaFirebaseMessagingService : ExpoFirebaseMessagingService() {
  override fun onMessageReceived(remoteMessage: RemoteMessage) {
    val live = extractLive(remoteMessage.data)
    if (live == null) {
      super.onMessageReceived(remoteMessage)
      return
    }
    Log.d(TAG, "live data message, keys=${remoteMessage.data.keys}")
    LiveController.apply(this, live.payload, live.serverRef)
    // A push is also the "changed, fetch now" signal for the widgets.
    WidgetSync.refreshAsync(this)
  }

  internal data class LivePush(val payload: String, val serverRef: String)

  companion object {
    private const val TAG = "MewlaLive"

    /**
     * Expo's push service puts the message's `data` object, stringified, under `body`; accept the
     * keys either at the top level or there so the handler does not depend on that wrapping.
     */
    internal fun extractLive(data: Map<String, String>): LivePush? {
      fun from(get: (String) -> String?): LivePush? {
        if (get("mewla_live") != "1") return null
        val payload = get("payload") ?: return null
        return LivePush(payload, get("server_ref").orEmpty())
      }
      from { data[it] }?.let { return it }
      val body = data["body"] ?: return null
      return try {
        val o = JSONObject(body)
        from { key -> if (o.has(key)) o.get(key).toString() else null }
      } catch (_: Exception) {
        null
      }
    }
  }
}
