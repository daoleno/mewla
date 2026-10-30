package expo.modules.mewlaphone

import android.accessibilityservice.AccessibilityService
import android.os.SystemClock
import android.view.accessibility.AccessibilityEvent
import android.view.accessibility.AccessibilityNodeInfo

/** What the Wireless debugging screens show, as last read. */
object SettingsReading {
    class Pairing(val code: String, val port: Int, val host: String, val at: Long)
    class Connect(val port: Int, val host: String, val at: Long)

    @Volatile var pairing: Pairing? = null
    @Volatile var connect: Connect? = null
    @Volatile var serviceRunning = false

    /** While true the service walks Settings to the pairing dialog by itself (see [PairingAccessibilityService]). */
    @Volatile var guiding = false

    /** Set by the service so the App can leave Settings when pairing is done. */
    @Volatile var service: PairingAccessibilityService? = null
}

/**
 * Reads the pairing code and the connection address off the Wireless debugging screens, so nobody
 * types six digits, and while [SettingsReading.guiding] taps through those screens (allow the
 * network, switch Wireless debugging on, open Pair with pairing code). It only listens to the
 * Settings app and keeps nothing but those two readings.
 */
class PairingAccessibilityService : AccessibilityService() {
    private var lastActionAt = 0L

    override fun onServiceConnected() {
        SettingsReading.serviceRunning = true
        SettingsReading.service = this
    }

    override fun onUnbind(intent: android.content.Intent?): Boolean {
        SettingsReading.serviceRunning = false
        SettingsReading.service = null
        return super.onUnbind(intent)
    }

    override fun onInterrupt() {}

    /** Leaves the Settings screens the guide opened. */
    fun leaveSettings() {
        performGlobalAction(GLOBAL_ACTION_BACK)
        performGlobalAction(GLOBAL_ACTION_BACK)
    }

    override fun onAccessibilityEvent(event: AccessibilityEvent?) {
        val root = rootInActiveWindow ?: return
        if (root.packageName?.toString() != SETTINGS_PACKAGE) return
        val texts = ArrayList<String>()
        collect(root, texts)
        val code = texts.firstOrNull { CODE.matches(it) }
        val address = texts.firstNotNullOfOrNull { ADDRESS.find(it) }
        if (address != null) {
            val host = address.groupValues[1]
            val port = address.groupValues[2].toIntOrNull()
            if (port != null) {
                if (code != null) SettingsReading.pairing = SettingsReading.Pairing(code, port, host, System.currentTimeMillis())
                else SettingsReading.connect = SettingsReading.Connect(port, host, System.currentTimeMillis())
            }
        }
        if (SettingsReading.guiding) guide(root, texts)
    }

    /** One step of the walk, at most one every 700 ms so each screen has settled before the next tap. */
    private fun guide(root: AccessibilityNodeInfo, texts: List<String>) {
        val now = SystemClock.uptimeMillis()
        if (now - lastActionAt < STEP_MS) return
        val step = when {
            // The pairing dialog is up: the code is read above; nothing to tap.
            texts.any { CODE.matches(it) } -> return
            texts.any { it.startsWith("Allow wireless debugging on this network") } -> allowNetwork(root)
            texts.any { it.equals("Pair device with pairing code", ignoreCase = true) } && wirelessOn(root) ->
                click(find(root, "Pair device with pairing code"))
            texts.any { it.equals("Use wireless debugging", ignoreCase = true) } -> switchOn(root)
            else -> false
        }
        if (step) lastActionAt = now
    }

    private fun allowNetwork(root: AccessibilityNodeInfo): Boolean {
        val always = find(root, "Always allow on this network")
        if (always != null && always.isCheckable && !always.isChecked) click(always)
        return click(find(root, "Allow"))
    }

    private fun wirelessOn(root: AccessibilityNodeInfo): Boolean = switchNode(root)?.isChecked == true

    private fun switchOn(root: AccessibilityNodeInfo): Boolean {
        val switch = switchNode(root) ?: return false
        return if (!switch.isChecked) click(switch) else false
    }

    private fun switchNode(root: AccessibilityNodeInfo): AccessibilityNodeInfo? {
        if (root.className?.toString()?.endsWith("Switch") == true && root.isCheckable) return root
        for (i in 0 until root.childCount) {
            val child = root.getChild(i) ?: continue
            switchNode(child)?.let { return it }
        }
        return null
    }

    private fun find(root: AccessibilityNodeInfo, text: String): AccessibilityNodeInfo? =
        root.findAccessibilityNodeInfosByText(text).firstOrNull { it.text?.toString()?.equals(text, ignoreCase = true) == true }

    /** Clicks [node], or its nearest clickable ancestor (a row's label is rarely the clickable part). */
    private fun click(node: AccessibilityNodeInfo?): Boolean {
        var current = node
        while (current != null) {
            if (current.isClickable && current.isEnabled) return current.performAction(AccessibilityNodeInfo.ACTION_CLICK)
            current = current.parent
        }
        return false
    }

    private fun collect(node: AccessibilityNodeInfo, out: MutableList<String>) {
        node.text?.toString()?.trim()?.takeIf { it.isNotEmpty() }?.let { out += it }
        for (i in 0 until node.childCount) {
            val child = node.getChild(i) ?: continue
            collect(child, out)
        }
    }

    private companion object {
        const val SETTINGS_PACKAGE = "com.android.settings"
        const val STEP_MS = 700L
        val CODE = Regex("^\\d{6}$")
        val ADDRESS = Regex("((?:\\d{1,3}\\.){3}\\d{1,3}):(\\d{2,5})")
    }
}
