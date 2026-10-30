package expo.modules.mewlaphone

import android.content.Context

/**
 * The words on the Settings and SystemUI screens the setup guide walks, in the language the phone
 * is set to. They are read from those packages' own string resources (so a Chinese phone shows
 * Chinese labels to match), with the English text as the fallback when a name is not there.
 */
internal class SettingsLabels(context: Context) {
    private val packages = context.packageManager

    private fun resource(pkg: String, name: String): String? = runCatching {
        val res = packages.getResourcesForApplication(pkg)
        val id = res.getIdentifier(name, "string", pkg)
        if (id == 0) null else res.getString(id)
    }.getOrNull()?.trim()?.takeIf { it.isNotEmpty() }

    private fun settings(name: String, english: String) = listOfNotNull(resource(SETTINGS, name), english).distinct()
    private fun systemUi(name: String, english: String) = listOfNotNull(resource(SYSTEMUI, name), english).distinct()

    val buildNumber = settings("build_number", "Build number")
    /** The row that leads to Build number on phones where it is one level down (ColorOS: About device > Version). */
    val versionRows = settings("firmware_version", "Android version") + listOf("Version")
    val wirelessRow = settings("adb_wireless_settings", "Wireless debugging")
    val wirelessSwitch = settings("wireless_debugging_main_switch_title", "Use wireless debugging")
    val pairWithCode = settings("adb_pair_method_code_title", "Pair device with pairing code")
    val networkTitle = systemUi("wifi_debugging_title", "Allow wireless debugging on this network")
    val alwaysAllow = systemUi("wifi_debugging_always", "Always allow on this network")

    companion object {
        const val SETTINGS = "com.android.settings"
        const val SYSTEMUI = "com.android.systemui"
    }
}
