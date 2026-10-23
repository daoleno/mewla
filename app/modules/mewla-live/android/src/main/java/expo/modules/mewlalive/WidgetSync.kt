package expo.modules.mewlalive

import android.content.Context
import android.util.Log
import java.net.HttpURLConnection
import java.net.URL
import kotlin.concurrent.thread

/**
 * Fetches the read-only snapshot with the scoped widget token (never the pairing credential).
 * A push is only the "changed, fetch now" signal: the widget shows what the endpoint returns,
 * so a revoked token stops it even while pushes keep arriving.
 */
internal object WidgetSync {
  private const val TAG = "MewlaLive"
  const val PATH = "/widget/snapshot"

  enum class Result { OK, UNCONFIGURED, REVOKED, FAILED }

  /** Runs off the main thread; the widgets repaint when it finishes. */
  fun refreshAsync(context: Context, done: (() -> Unit)? = null) {
    val app = context.applicationContext
    thread(name = "mewla-widget-sync") {
      try {
        fetch(app)
      } finally {
        WidgetRender.renderAll(app)
        done?.invoke()
      }
    }
  }

  fun fetch(context: Context): Result {
    val prefs = LivePrefs(context)
    val base = prefs.widgetBaseUrl
    val token = SecretBox.open(prefs.widgetTokenSealed)
    if (token.isNullOrEmpty() && prefs.widgetTokenSealed.isNotEmpty()) {
      // A token the Keystore can no longer open is useless: forget it so the app issues a new one.
      prefs.widgetTokenSealed = ""
    }
    if (base.isEmpty() || token.isNullOrEmpty()) return Result.UNCONFIGURED
    var connection: HttpURLConnection? = null
    return try {
      connection = (URL(base.trimEnd('/') + PATH).openConnection() as HttpURLConnection).apply {
        requestMethod = "GET"
        connectTimeout = 8_000
        readTimeout = 8_000
        setRequestProperty("Authorization", "Bearer $token")
        setRequestProperty("Accept", "application/json")
      }
      when (val code = connection.responseCode) {
        200 -> {
          val body = connection.inputStream.bufferedReader().use { it.readText() }
          val live = LiveState.liveFromSnapshot(body)
          if (live == null) {
            Log.w(TAG, "widget snapshot was not a live state")
            Result.FAILED
          } else {
            LiveState.parse(live)?.let { prefs.cacheWidgetState(live, it) }
            prefs.widgetRevoked = false
            Result.OK
          }
        }
        401, 403 -> {
          // Revoked: forget everything the widget knew, and say so instead of showing stale state.
          prefs.widgetCache = ""
          prefs.widgetTokenSealed = ""
          prefs.widgetRevoked = true
          Log.i(TAG, "widget token rejected ($code); widgets cleared")
          Result.REVOKED
        }
        else -> {
          Log.w(TAG, "widget snapshot answered $code")
          Result.FAILED
        }
      }
    } catch (e: Exception) {
      Log.w(TAG, "widget snapshot fetch failed: ${e.javaClass.simpleName}")
      Result.FAILED
    } finally {
      connection?.disconnect()
    }
  }
}
