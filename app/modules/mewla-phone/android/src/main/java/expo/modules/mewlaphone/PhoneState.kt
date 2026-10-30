package expo.modules.mewlaphone

import android.content.Context
import android.content.SharedPreferences

/** What phone use remembers between runs; none of it is secret (the token lives in [HelperEndpoint]). */
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
}
