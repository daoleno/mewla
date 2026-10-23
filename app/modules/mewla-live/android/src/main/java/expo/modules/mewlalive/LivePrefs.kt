package expo.modules.mewlalive

import android.content.Context
import android.content.SharedPreferences

/** The module's private state. Nothing here leaves the app sandbox. */
internal class LivePrefs(context: Context) {
  private val prefs: SharedPreferences =
    context.applicationContext.getSharedPreferences("mewla_live", Context.MODE_PRIVATE)

  fun memory() = LiveMemory(
    epoch = prefs.getLong("epoch", 0L),
    seq = prefs.getLong("seq", 0L),
    dismissedKey = prefs.getString("dismissed_key", "") ?: "",
  )

  fun remember(state: LiveState) {
    prefs.edit().putLong("epoch", state.epoch).putLong("seq", state.seq).apply()
  }

  fun dismiss(runKey: String) {
    prefs.edit().putString("dismissed_key", runKey).apply()
  }

  var currentServerRef: String
    get() = prefs.getString("server_ref", "") ?: ""
    set(value) { prefs.edit().putString("server_ref", value).apply() }

  fun resetForServerSwitch() {
    prefs.edit().remove("epoch").remove("seq").remove("dismissed_key").apply()
  }

  // Widget snapshot cache and credentials (sealed with the Android Keystore).
  var widgetCache: String
    get() = prefs.getString("widget_cache", "") ?: ""
    set(value) { prefs.edit().putString("widget_cache", value).apply() }

  var widgetFetchedAt: Long
    get() = prefs.getLong("widget_fetched_at", 0L)
    set(value) { prefs.edit().putLong("widget_fetched_at", value).apply() }

  var widgetBaseUrl: String
    get() = prefs.getString("widget_base_url", "") ?: ""
    set(value) { prefs.edit().putString("widget_base_url", value).apply() }

  var widgetTokenSealed: String
    get() = prefs.getString("widget_token", "") ?: ""
    set(value) { prefs.edit().putString("widget_token", value).apply() }

  /** Set when the daemon answered 401: the widget shows "open Mewla" until re-provisioned. */
  var widgetRevoked: Boolean
    get() = prefs.getBoolean("widget_revoked", false)
    set(value) { prefs.edit().putBoolean("widget_revoked", value).apply() }

  fun clearWidget() {
    prefs.edit().remove("widget_cache").remove("widget_fetched_at").remove("widget_base_url")
      .remove("widget_token").remove("widget_revoked").apply()
  }
}
