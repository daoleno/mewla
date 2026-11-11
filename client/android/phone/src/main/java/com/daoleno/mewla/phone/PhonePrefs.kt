package com.daoleno.mewla.phone

import android.content.Context
import android.content.SharedPreferences
import com.daoleno.mewla.phone.link.LinkTarget

/**
 * What phone use remembers between runs, in the Expo app's preferences file (so the switch and the
 * pairing survive the upgrade). None of it is secret: the helper's token lives in [HelperEndpoint]
 * and the device key in the app's secure store.
 */
internal class PhonePrefs(context: Context) {
    private val prefs: SharedPreferences = context.getSharedPreferences("mewla_phone", Context.MODE_PRIVATE)

    var paired: Boolean
        get() = prefs.getBoolean("paired", false)
        set(value) = prefs.edit().putBoolean("paired", value).apply()

    var adbPort: Int
        get() = prefs.getInt("adb_port", 0)
        set(value) = prefs.edit().putInt("adb_port", value).apply()

    /** Whether the person has Phone use turned on; the helper is only started while it is. */
    var enabled: Boolean
        get() = prefs.getBoolean("enabled", false)
        set(value) = prefs.edit().putBoolean("enabled", value).apply()

    /** The current computer the link follows, so it comes back after a reboot with no screen; null when there is none. */
    var target: LinkTarget?
        get() {
            val serverId = prefs.getString("link_server_id", null) ?: return null
            val url = prefs.getString("link_url", null) ?: return null
            val daemonId = prefs.getString("link_daemon_id", null) ?: return null
            return LinkTarget(serverId, url, daemonId)
        }
        set(value) {
            val edit = prefs.edit()
            if (value == null) edit.remove("link_server_id").remove("link_url").remove("link_daemon_id")
            else edit.putString("link_server_id", value.serverId).putString("link_url", value.url).putString("link_daemon_id", value.daemonId)
            edit.apply()
        }
}
