package expo.modules.mewlaphone

import android.accessibilityservice.AccessibilityService
import android.content.Intent
import android.graphics.Color
import android.graphics.PixelFormat
import android.graphics.drawable.GradientDrawable
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import android.provider.Settings
import android.view.Gravity
import android.view.View
import android.view.WindowManager
import android.view.accessibility.AccessibilityEvent
import android.view.accessibility.AccessibilityNodeInfo
import android.widget.TextView

/** What the Wireless debugging screens show, as last read, and how far the guide has got. */
object SettingsReading {
    class Pairing(val code: String, val port: Int, val host: String, val at: Long)
    class Connect(val port: Int, val host: String, val at: Long)

    @Volatile var pairing: Pairing? = null
    @Volatile var connect: Connect? = null
    @Volatile var serviceRunning = false

    /** While true the service walks Settings by itself (see [PairingAccessibilityService]). */
    @Volatile var guiding = false

    /** The step the walk is on: developer, wireless or pair; null when it is not walking. */
    @Volatile var step: String? = null

    /** What only the person can do right now: "allow" (the system's network prompt) or "lock" (the screen lock). */
    @Volatile var needs: String? = null

    /** The last time the walk moved on (a tap, a new step, a new screen), in uptime milliseconds. */
    @Volatile var progressAt = 0L

    /** Set by the service so the App can drive it and leave Settings when pairing is done. */
    @Volatile var service: PairingAccessibilityService? = null
}

/**
 * Reads the pairing code and the connection address off the Wireless debugging screens, so nobody
 * types six digits, and while [SettingsReading.guiding] walks Settings for the person: Build number
 * until Developer options is on, the Wireless debugging switch, Pair device with pairing code. It
 * never approves a system prompt: the "Allow wireless debugging on this network" dialog is the
 * person's, and the walk only ticks its "Always allow" box and waits. It listens to the Settings
 * and SystemUI apps only and keeps nothing but those readings.
 */
class PairingAccessibilityService : AccessibilityService() {
    private val handler = Handler(Looper.getMainLooper())
    private var lastActionAt = 0L
    private var lastLaunchAt = 0L
    private var banner: TextView? = null
    private var labels: SettingsLabels? = null

    private val poll = object : Runnable {
        override fun run() {
            if (!SettingsReading.guiding) {
                hideBanner()
                return
            }
            runCatching { step() }
            handler.postDelayed(this, POLL_MS)
        }
    }

    override fun onServiceConnected() {
        SettingsReading.serviceRunning = true
        SettingsReading.service = this
    }

    override fun onUnbind(intent: Intent?): Boolean {
        SettingsReading.serviceRunning = false
        SettingsReading.service = null
        hideBanner()
        return super.onUnbind(intent)
    }

    override fun onInterrupt() {}

    /** Starts the walk (the controller has set [SettingsReading.guiding]). */
    fun beginGuide() {
        labels = SettingsLabels(this)
        SettingsReading.progressAt = SystemClock.uptimeMillis()
        handler.removeCallbacks(poll)
        handler.post(poll)
    }

    fun endGuide() {
        handler.removeCallbacks(poll)
        handler.post { hideBanner() }
    }

    /** Leaves the Settings screens the guide opened. */
    fun leaveSettings() {
        performGlobalAction(GLOBAL_ACTION_BACK)
        performGlobalAction(GLOBAL_ACTION_BACK)
    }

    override fun onAccessibilityEvent(event: AccessibilityEvent?) {
        val root = rootInActiveWindow ?: return
        if (root.packageName?.toString() != SettingsLabels.SETTINGS) return
        val texts = ArrayList<String>()
        collect(root, texts)
        readPairing(texts)
    }

    private fun readPairing(texts: List<String>) {
        val code = texts.firstOrNull { CODE.matches(it) }
        val address = texts.firstNotNullOfOrNull { ADDRESS.find(it) } ?: return
        val host = address.groupValues[1]
        val port = address.groupValues[2].toIntOrNull() ?: return
        if (code != null) SettingsReading.pairing = SettingsReading.Pairing(code, port, host, System.currentTimeMillis())
        else SettingsReading.connect = SettingsReading.Connect(port, host, System.currentTimeMillis())
    }

    // ---- the walk ----------------------------------------------------------------------------

    private fun moved(step: String? = SettingsReading.step) {
        SettingsReading.step = step
        SettingsReading.progressAt = SystemClock.uptimeMillis()
    }

    private fun developerOn() = Settings.Global.getInt(contentResolver, Settings.Global.DEVELOPMENT_SETTINGS_ENABLED, 0) == 1
    private fun wirelessOn() = Settings.Global.getInt(contentResolver, "adb_wifi_enabled", 0) == 1

    /** One step of the walk, every [POLL_MS]; each action waits for the screen to settle. */
    private fun step() {
        val l = labels ?: SettingsLabels(this).also { labels = it }
        val now = SystemClock.uptimeMillis()
        val root = rootInActiveWindow
        val pkg = root?.packageName?.toString()
        val wanted = when {
            !developerOn() -> "developer"
            !wirelessOn() -> "wireless"
            else -> "pair"
        }
        if (SettingsReading.step != wanted) moved(wanted)

        val texts = ArrayList<String>()
        if (root != null) collect(root, texts)

        // The system's own prompt: not ours to answer. Tick "Always allow" (so a reboot needs no
        // second answer) and show what is left for the person.
        if (pkg == SettingsLabels.SYSTEMUI && root != null && texts.any { startsWithAny(it, l.networkTitle) }) {
            SettingsReading.needs = "allow"
            hideBanner()
            if (now - lastActionAt > ACTION_MS) {
                findLabel(root, l.alwaysAllow)?.takeIf { it.isCheckable && !it.isChecked }?.let { if (click(it)) lastActionAt = now }
            }
            return
        }
        if (pkg != SettingsLabels.SETTINGS || root == null) {
            SettingsReading.needs = null
            hideBanner()
            // Somewhere else (Mewla, the launcher): bring the right Settings screen up.
            if (now - lastLaunchAt > LAUNCH_MS) open(wanted)
            return
        }
        // Settings is in front: the walk is on it, and says so.
        SettingsReading.needs = null
        showBanner(wanted)
        if (texts.any { CODE.matches(it) }) return // the pairing dialog is up: the code is read, nothing to tap
        if (hasPasswordField(root)) {
            SettingsReading.needs = "lock"
            moved()
            return
        }
        if (now - lastActionAt < ACTION_MS) return
        val acted = when (wanted) {
            "developer" -> buildNumber(root, l, now)
            "wireless" -> wireless(root, texts, l, now)
            else -> pair(root, texts, l, now)
        }
        if (acted) {
            lastActionAt = now
            SettingsReading.progressAt = now
        }
    }

    private fun open(step: String) {
        val intent = when (step) {
            "developer" -> Intent(Settings.ACTION_DEVICE_INFO_SETTINGS)
            else -> Intent(Settings.ACTION_APPLICATION_DEVELOPMENT_SETTINGS)
        }.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        lastLaunchAt = SystemClock.uptimeMillis()
        runCatching { startActivity(intent) }
    }

    /** Taps Build number (seven times over seven polls) until Developer options turns on. */
    private fun buildNumber(root: AccessibilityNodeInfo, l: SettingsLabels, now: Long): Boolean {
        findLabel(root, l.buildNumber)?.let { return click(it) }
        // Not on this screen: one level down on some phones, else further down the list.
        if (scrollForward(root)) return true
        findLabel(root, l.versionRows)?.let { return click(it) }
        if (now - lastLaunchAt > LAUNCH_MS) open("developer")
        return false
    }

    private fun wireless(root: AccessibilityNodeInfo, texts: List<String>, l: SettingsLabels, now: Long): Boolean {
        // On the Wireless debugging page itself: its main switch.
        if (texts.any { equalsAny(it, l.wirelessSwitch) }) {
            val switch = switchNode(root) ?: return false
            return if (!switch.isChecked) click(switch) else false
        }
        // In Developer options: the Wireless debugging row, further down the list.
        findLabel(root, l.wirelessRow)?.let { return click(it) }
        if (scrollForward(root)) return true
        if (now - lastLaunchAt > LAUNCH_MS) open("wireless")
        return false
    }

    private fun pair(root: AccessibilityNodeInfo, texts: List<String>, l: SettingsLabels, now: Long): Boolean {
        findLabel(root, l.pairWithCode)?.let { return click(it) }
        // Still in Developer options: the row that leads to the Wireless debugging page.
        findLabel(root, l.wirelessRow)?.let { return click(it) }
        if (scrollForward(root)) return true
        if (now - lastLaunchAt > LAUNCH_MS) open("wireless")
        return false
    }

    // ---- the banner --------------------------------------------------------------------------

    /**
     * A line over Settings saying what Mewla is doing, so the screen changing by itself is not a
     * mystery. It never takes a touch, and it is gone whenever Settings is not in front (the
     * system's prompts must not be covered: they ignore a tap that comes through an overlay).
     */
    private fun showBanner(step: String) {
        val text = when (step) {
            "developer" -> "Mewla is turning on Developer options"
            "wireless" -> "Mewla is turning on Wireless debugging"
            else -> "Mewla is pairing this phone"
        }
        val view = banner ?: TextView(this).apply {
            setTextColor(Color.WHITE)
            textSize = 14f
            setPadding(40, 20, 40, 20)
            background = GradientDrawable().apply { setColor(0xE6202124.toInt()); cornerRadius = 60f }
            val params = WindowManager.LayoutParams(
                WindowManager.LayoutParams.WRAP_CONTENT,
                WindowManager.LayoutParams.WRAP_CONTENT,
                WindowManager.LayoutParams.TYPE_ACCESSIBILITY_OVERLAY,
                WindowManager.LayoutParams.FLAG_NOT_FOCUSABLE or WindowManager.LayoutParams.FLAG_NOT_TOUCHABLE,
                PixelFormat.TRANSLUCENT,
            ).apply { gravity = Gravity.BOTTOM or Gravity.CENTER_HORIZONTAL; y = 160 }
            runCatching { getSystemService(WindowManager::class.java).addView(this, params) }
            banner = this
        }
        if (view.text != text) view.text = text
        view.visibility = View.VISIBLE
    }

    private fun hideBanner() {
        banner?.visibility = View.GONE
    }

    // ---- reading and tapping nodes -----------------------------------------------------------

    private fun equalsAny(text: String, candidates: List<String>) = candidates.any { it.equals(text, ignoreCase = true) }

    private fun startsWithAny(text: String, candidates: List<String>) = candidates.any { text.startsWith(it, ignoreCase = true) }

    private fun findLabel(root: AccessibilityNodeInfo, candidates: List<String>): AccessibilityNodeInfo? {
        for (candidate in candidates) {
            root.findAccessibilityNodeInfosByText(candidate)
                .firstOrNull { it.text?.toString()?.trim()?.equals(candidate, ignoreCase = true) == true }
                ?.let { return it }
        }
        return null
    }

    private fun hasPasswordField(node: AccessibilityNodeInfo): Boolean {
        if (node.isPassword) return true
        for (i in 0 until node.childCount) {
            val child = node.getChild(i) ?: continue
            if (hasPasswordField(child)) return true
        }
        return false
    }

    private fun switchNode(root: AccessibilityNodeInfo): AccessibilityNodeInfo? {
        if (root.className?.toString()?.endsWith("Switch") == true && root.isCheckable) return root
        for (i in 0 until root.childCount) {
            val child = root.getChild(i) ?: continue
            switchNode(child)?.let { return it }
        }
        return null
    }

    private fun scrollableNode(node: AccessibilityNodeInfo): AccessibilityNodeInfo? {
        if (node.isScrollable) return node
        for (i in 0 until node.childCount) {
            val child = node.getChild(i) ?: continue
            scrollableNode(child)?.let { return it }
        }
        return null
    }

    private fun scrollForward(root: AccessibilityNodeInfo): Boolean =
        scrollableNode(root)?.performAction(AccessibilityNodeInfo.ACTION_SCROLL_FORWARD) == true

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
        const val POLL_MS = 600L
        const val ACTION_MS = 450L
        const val LAUNCH_MS = 6_000L
        val CODE = Regex("^\\d{6}$")
        val ADDRESS = Regex("((?:\\d{1,3}\\.){3}\\d{1,3}):(\\d{2,5})")
    }
}
