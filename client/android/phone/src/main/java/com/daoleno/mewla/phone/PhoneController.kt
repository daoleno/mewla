package com.daoleno.mewla.phone

import android.content.Context
import android.hardware.display.DisplayManager
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.os.Build
import android.os.PowerManager
import android.provider.Settings
import android.util.DisplayMetrics
import android.util.Log
import android.view.Display
import com.daoleno.mewla.phone.adb.AdbConnectPort
import com.daoleno.mewla.phone.adb.AdbConnection
import com.daoleno.mewla.phone.adb.AdbConnectionException
import com.daoleno.mewla.phone.adb.AdbPairing
import com.daoleno.mewla.phone.adb.AdbPairingException
import com.daoleno.mewla.phone.adb.AdbServiceTypes
import com.daoleno.mewla.phone.adb.AndroidAdb
import com.daoleno.mewla.phone.adb.NsdAdbServiceFinder
import com.daoleno.mewla.phone.adb.PortCache
import com.daoleno.mewla.phone.helper.Clones
import com.daoleno.mewla.phone.helper.PhoneHelper
import com.daoleno.mewla.phone.link.DeviceIdentity
import com.daoleno.mewla.phone.link.DeviceKeys
import com.daoleno.mewla.phone.link.LinkTarget
import com.daoleno.mewla.phone.link.PhoneFrames
import com.daoleno.mewla.phone.link.PhoneLink
import expo.modules.mewlaphone.PairingAccessibilityService
import expo.modules.mewlaphone.SettingsReading
import kotlinx.coroutines.runBlocking
import org.json.JSONArray
import org.json.JSONObject
import java.util.concurrent.CopyOnWriteArraySet
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * Phone use on this phone: the setup it needs, the helper it keeps running, the link to the one
 * current computer and the one entry the computer's requests come through. Everything here is plain
 * Android; the app's adapter (src/github/.../phone) puts it behind `phone.*`.
 */
object PhoneController {
    private const val TAG = "MewlaPhone"
    private const val MIN_SDK = Build.VERSION_CODES.R
    private const val STALL_MS = 25_000L
    // The agent display has the phone's real pixels; what the agent sees and sends is the picture space, at most this wide.
    private const val MODEL_IMAGE_WIDTH = 720
    private const val REPLACE_EVERY_MS = 60_000L
    private const val ANNOUNCE_WAIT_MS = 150_000L
    private const val WRITE_SECURE = "android.permission.WRITE_SECURE_SETTINGS"

    private lateinit var app: Context
    private lateinit var prefs: PhonePrefs

    /** Told the live status whenever it changes; called from worker threads. */
    val statusListeners = CopyOnWriteArraySet<(JSONObject) -> Unit>()

    private val helperLock = Any()
    @Volatile private var client: HelperClient? = null
    @Volatile private var display: IntArray? = null
    @Volatile private var watching = false
    private val finder by lazy { NsdAdbServiceFinder(app) }

    // What the person did to the agent's screen.
    @Volatile private var stopped = false
    @Volatile private var personInControl = false
    @Volatile var activeWorker: String? = null
        private set
    private var handoff: Handoff? = null

    /** Whether the agent's display has shown a picture since it was made; until then the float says what is going on instead of showing black. */
    @Volatile private var drawn = false
    /** The app being opened on the agent's display, and the one that could not be, by the name the agent knows it by. */
    @Volatile private var opening: String? = null
    @Volatile private var launchFailed: String? = null

    /** A new or gone display has drawn nothing yet. */
    private fun resetPicture() {
        drawn = false
        opening = null
        launchFailed = null
        PhoneLiveViews.last = null
    }

    private fun appLabel(pkg: String, user: Int): String {
        val base = runCatching { app.packageManager.getApplicationLabel(app.packageManager.getApplicationInfo(pkg, 0)).toString() }.getOrDefault(pkg)
        return if (user == 0) base else Clones.label(base, user, java.util.Locale.getDefault().language == "zh")
    }

    private class Handoff(val id: String, val reason: String) {
        val done = CountDownLatch(1)
    }

    val initialized: Boolean get() = ::app.isInitialized

    @Synchronized
    fun init(context: Context) {
        if (::app.isInitialized) return
        app = context.applicationContext
        prefs = PhonePrefs(app)
        if (prefs.enabled) {
            // The service is what keeps this process running with the App in the background, so it
            // comes back whenever the App does (after an update, a crash or a restart).
            runCatching { PhoneUseService.start(app) }.onFailure { Log.w(TAG, "cannot start the service from here", it) }
            startKeeper()
            ensureLink()
        }
    }

    // ---- the link to the computer ------------------------------------------------------------

    private val linkLock = Any()
    @Volatile private var link: PhoneLink? = null
    private val deviceKeys by lazy { DeviceKeys.load(app) }

    /** off (Phone use off, or no current computer), connecting, online, offline, replaced or unsupported. */
    @Volatile var linkState: String = "off"
        private set

    /** Whether the signed connection to the computer is up: the notification says so, since the computer's requests only come through it. */
    val linkOnline: Boolean get() = linkState == "online"

    /**
     * The computer the link follows: the app's one current computer, or null when there is none.
     * A switch closes the old computer's link and opens one to the next; the same computer keeps its
     * link (a "replaced" close stays until the computer or the switch changes).
     */
    fun follow(target: LinkTarget?) {
        prefs.target = target
        synchronized(linkLock) {
            if (link?.linkTarget == target && target != null) return
            stopLink()
        }
        ensureLink()
    }

    /** Brings the link up when Phone use is on and there is a computer to follow; nothing when it already runs. */
    fun ensureLink() {
        if (!::app.isInitialized) return
        synchronized(linkLock) {
            if (!prefs.enabled) return stopLink()
            val target = prefs.target ?: return stopLink()
            if (link != null) return
            val created = PhoneLink(target, linkHost)
            link = created
            setLinkState("connecting")
            created.start()
        }
    }

    /** The app came to the front: a link waiting out its backoff tries now. */
    fun retryLinkNow() {
        link?.retryNow()
    }

    private fun stopLink() {
        val running = link ?: return setLinkState("off")
        link = null
        running.stop()
        setLinkState("off")
    }

    private fun setLinkState(state: String) {
        if (linkState == state) return
        linkState = state
        if (::app.isInitialized) publish()
    }

    private val linkHost = object : PhoneLink.Host {
        override fun identity(): DeviceIdentity? = deviceKeys?.identity()
        override fun hello(): JSONObject {
            val version = runCatching { app.packageManager.getPackageInfo(app.packageName, 0).versionName }.getOrNull().orEmpty()
            return PhoneFrames.hello(Build.MODEL, Build.VERSION.SDK_INT, version, state())
        }
        override fun status(): JSONObject = this@PhoneController.status()
        override fun execute(request: JSONObject): JSONObject = this@PhoneController.execute(request)
        override fun onState(state: String) {
            // A link that was let go says nothing more.
            if (link == null) return
            setLinkState(state)
        }
    }

    // ---- keeping the helper up ---------------------------------------------------------------

    private var keeper: Thread? = null

    /**
     * While Phone use is on, brings the helper back whenever it is not there: after the App is
     * restarted or updated, after a Wi-Fi change or a reboot. The daemon only sends actions to a
     * phone that says it is ready, so this has to happen before an agent asks, not when it does.
     */
    @Synchronized
    private fun startKeeper() {
        if (keeper?.isAlive == true) return
        watchSystem()
        keeper = Thread({ keepHelper() }, "mewla-phone-keeper").apply { isDaemon = true; start() }
    }

    private val wake = java.util.concurrent.Semaphore(0)
    private val helperKeeper by lazy { HelperKeeper(keeperEnv) }

    /** Something the keeper waits for may have happened (Wi-Fi, the Wireless debugging switch, the helper going): its next pass is now. */
    private fun kick() {
        helperKeeper.changed()
        if (wake.availablePermits() == 0) wake.release()
    }

    private fun sleepUnlessKicked(ms: Long) {
        wake.tryAcquire(ms, java.util.concurrent.TimeUnit.MILLISECONDS)
        wake.drainPermits()
    }

    private fun keepHelper() {
        var shown = state()
        try {
            while (prefs.enabled) {
                // The notification follows the helper: it must not keep saying "on" after the helper is gone.
                if (state() != shown) {
                    shown = state()
                    publish()
                }
                if (client?.isClosed == false && staleHelper) replaceStaleHelper()
                val wait = helperKeeper.step()
                publish()
                sleepUnlessKicked(wait)
            }
        } catch (_: InterruptedException) {
        }
    }

    private val keeperEnv = object : HelperKeeper.Env {
        override fun facts() = Facts(
            enabled = prefs.enabled,
            paired = prefs.paired,
            pairing = pairing,
            wifi = onWifi(),
            wirelessOn = wirelessDebuggingOn(),
            mayEnableWireless = selfHealing,
            pausedUntil = promptGuard.pausedUntil,
            now = System.currentTimeMillis(),
        )
        override fun helperUp() = client?.isClosed == false
        override fun startHelper() {
            try {
                ensureHelper()
            } catch (e: IllegalStateException) {
                throw e
            } catch (e: Exception) {
                throw IllegalStateException("could not start the helper through Wireless debugging (${e.message ?: e.javaClass.simpleName}); Mewla tries again", e)
            }
        }
        override fun grant(): String? = grantThroughWirelessDebugging()
        override fun report(reason: String?) = noteProblem(reason)
    }

    private fun wirelessDebuggingOn(): Boolean = Settings.Global.getInt(app.contentResolver, "adb_wifi_enabled", 0) == 1

    /**
     * Wi-Fi coming and going, and Wireless debugging being switched (by Android after a network change,
     * or by the person), are what the keeper waits on: it looks again the moment one happens instead of
     * on its next poll.
     */
    private fun watchSystem() {
        if (watchingSystem) return
        watchingSystem = true
        runCatching {
            val cm = app.getSystemService(ConnectivityManager::class.java)
            cm.registerNetworkCallback(
                android.net.NetworkRequest.Builder().addTransportType(NetworkCapabilities.TRANSPORT_WIFI).build(),
                object : ConnectivityManager.NetworkCallback() {
                    override fun onAvailable(network: android.net.Network) = kick()
                    override fun onLost(network: android.net.Network) { kick(); publish() }
                },
            )
        }.onFailure { Log.w(TAG, "cannot watch Wi-Fi", it) }
        runCatching {
            app.contentResolver.registerContentObserver(
                Settings.Global.getUriFor("adb_wifi_enabled"), false,
                object : android.database.ContentObserver(android.os.Handler(android.os.Looper.getMainLooper())) {
                    override fun onChange(selfChange: Boolean) { kick(); publish() }
                },
            )
        }.onFailure { Log.w(TAG, "cannot watch the Wireless debugging switch", it) }
    }

    private var watchingSystem = false

    // ---- setup -------------------------------------------------------------------------------

    /** The live checks behind the setup screen, plus the state the daemon is told. */
    fun status(): JSONObject {
        val developer = Settings.Global.getInt(app.contentResolver, Settings.Global.DEVELOPMENT_SETTINGS_ENABLED, 0) == 1
        val wireless = Settings.Global.getInt(app.contentResolver, "adb_wifi_enabled", 0) == 1
        val helperUp = client?.isClosed == false
        return JSONObject()
            .put("supported", Build.VERSION.SDK_INT >= MIN_SDK)
            .put("sdk", Build.VERSION.SDK_INT)
            .put("enabled", prefs.enabled)
            .put("link", linkOnline)
            .put("link_state", linkState)
            .put("developer_options", developer)
            .put("wireless_debugging", wireless)
            .put("wifi", onWifi())
            .put("paired", prefs.paired)
            .put("helper", helperUp)
            .put("problem", setupReason() ?: JSONObject.NULL)
            .put("accessibility", SettingsReading.serviceRunning)
            .put("restricted", accessibilityRestricted())
            .put("self_healing", selfHealing)
            .put("guide", guide())
            .put("notifications", PhoneNotifier.canNotify(app))
            .put("state", state())
            .put("w", pictureSize()?.get(0) ?: 0)
            .put("h", pictureSize()?.get(1) ?: 0)
            .put("display_info", displayInfo ?: JSONObject.NULL)
            .put("agent", activeWorker ?: JSONObject.NULL)
            .put("drawn", drawn)
            .put("opening", opening ?: JSONObject.NULL)
            .put("launch_failed", launchFailed ?: JSONObject.NULL)
            .put("handoff", handoff?.reason ?: JSONObject.NULL)
    }

    /**
     * Where the guided setup is: the step it is on, what only the person can do ("allow" the
     * system's network prompt, "lock" the screen lock), and whether it has stopped getting
     * anywhere (the one step the person then does by hand).
     */
    private fun guide(): Any {
        if (!SettingsReading.guiding) return JSONObject.NULL
        val quiet = android.os.SystemClock.uptimeMillis() - SettingsReading.progressAt > STALL_MS
        return JSONObject()
            .put("step", SettingsReading.step ?: JSONObject.NULL)
            .put("needs", SettingsReading.needs ?: JSONObject.NULL)
            .put("stalled", quiet && SettingsReading.needs == null)
    }

    /**
     * True when Android may block switching a sideloaded app's accessibility service on until "Allow
     * restricted settings" is chosen in App info. An app cannot read that setting itself (the op is
     * system-only), so this is the rule's trigger: Android 13+ and not installed by the Play Store.
     */
    private fun accessibilityRestricted(): Boolean = runCatching {
        if (Build.VERSION.SDK_INT < 33 || SettingsReading.serviceRunning) return@runCatching false
        app.packageManager.getInstallSourceInfo(app.packageName).installingPackageName != "com.android.vending"
    }.getOrDefault(false)

    /**
     * Switches this App's accessibility service on. With the secure-settings grant that is a
     * silent write; before it, Android only lets the person do it, so this returns false and the
     * caller opens the system screen.
     */
    fun enableAccessibility(): Boolean {
        if (SettingsReading.serviceRunning) return true
        if (!selfHealing) return false
        val component = android.content.ComponentName(app, PairingAccessibilityService::class.java).flattenToString()
        val current = Settings.Secure.getString(app.contentResolver, Settings.Secure.ENABLED_ACCESSIBILITY_SERVICES).orEmpty()
        if (!current.split(':').contains(component)) {
            Settings.Secure.putString(app.contentResolver, Settings.Secure.ENABLED_ACCESSIBILITY_SERVICES, if (current.isEmpty()) component else "$current:$component")
        }
        Settings.Secure.putInt(app.contentResolver, Settings.Secure.ACCESSIBILITY_ENABLED, 1)
        return true
    }

    /** The one word the daemon and the App agree on (see daemon/phone/protocol.go). */
    fun state(): String = when {
        stopped -> "stopped"
        personInControl -> "person_in_control"
        client?.isClosed == false -> "ready"
        else -> "setup"
    }

    fun setEnabled(enabled: Boolean) {
        prefs.enabled = enabled
        if (enabled) {
            PhoneUseService.start(app)
            startKeeper()
            ensureLink()
        } else {
            synchronized(linkLock) { stopLink() }
            PhoneUseService.stop(app)
            stopHelper()
        }
        publish()
    }

    val enabled: Boolean get() = ::prefs.isInitialized && prefs.enabled

    val paired: Boolean get() = if (::prefs.isInitialized) prefs.paired else false

    /** The agent's display size in real pixels (what a person's touch maps onto), or null before it exists. */
    fun displaySize(): IntArray? = display

    /**
     * The picture space: the display scaled to at most [MODEL_IMAGE_WIDTH] wide, as pictures for the model are.
     * Everything the agent sees and sends (screen size, element bounds, tap and swipe points) is in it, so a
     * point on the picture is a point on the screen; only the phone itself works in real display pixels.
     * The height is cut the way the helper cuts a scaled picture's, so both always agree.
     */
    fun pictureSize(): IntArray? = display?.let { d ->
        val w = minOf(d[0], MODEL_IMAGE_WIDTH)
        intArrayOf(w, maxOf(1, d[1] * w / d[0]))
    }

    /** A point of the picture space as a display pixel. */
    private fun toDisplay(value: Int, axis: Int): Int {
        val real = display ?: return value
        val picture = pictureSize() ?: return value
        return Math.round(value.toDouble() * real[axis] / picture[axis]).toInt().coerceIn(0, real[axis] - 1)
    }

    /** A box of display pixels (left, top, right, bottom) as a box of the picture space, in place. */
    private fun toPicture(box: JSONArray?) {
        val real = display ?: return
        val picture = pictureSize() ?: return
        if (box == null || box.length() != 4) return
        for (i in 0 until 4) {
            val axis = i % 2
            box.put(i, Math.round(box.optInt(i).toDouble() * picture[axis] / real[axis]).toInt())
        }
    }

    private fun onWifi(): Boolean = runCatching {
        val cm = app.getSystemService(ConnectivityManager::class.java)
        @Suppress("DEPRECATION")
        cm.allNetworks.any { cm.getNetworkCapabilities(it)?.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) == true }
    }.getOrDefault(false)

    fun publish() {
        val now = status()
        statusListeners.forEach { listener -> runCatching { listener(now) } }
        link?.relayStatus(now)
        PhoneNotifier.refresh(app)
    }

    // ---- pairing -----------------------------------------------------------------------------

    @Volatile private var pairing = false
    @Volatile private var typedCode: String? = null

    /**
     * Pairs with this phone's own adbd. The code is [code] (typed into the notification) or, when
     * null, read off the Wireless debugging screen by the accessibility service, which also taps
     * through those screens while this runs; the port is what adbd announces over mDNS or what
     * that same screen shows. A typed code reaches a pairing already waiting for one.
     */
    fun pair(code: String?): Boolean {
        if (code != null) {
            typedCode = code
            if (pairing) return true
        }
        if (pairing) return false
        pairing = true
        SettingsReading.pairing = null
        SettingsReading.needs = null
        SettingsReading.guiding = code == null && SettingsReading.serviceRunning
        if (SettingsReading.guiding) SettingsReading.service?.beginGuide()
        promptGuard.personAsked()
        try {
            // A guided walk may start from nothing (Build number, a lock screen, the system's prompt), so it gets longer.
            val deadline = System.currentTimeMillis() + if (SettingsReading.guiding) 8 * 60_000 else 3 * 60_000
            val tls = AndroidAdb.tls(AndroidAdb.identity(app))
            var lastFailure: Exception? = null
            while (System.currentTimeMillis() < deadline) {
                val reading = SettingsReading.pairing?.takeIf { System.currentTimeMillis() - it.at < 2 * 60_000 }
                val digits = typedCode ?: reading?.code
                if (digits == null) {
                    Thread.sleep(400)
                    continue
                }
                val ports = LinkedHashSet<Int>()
                reading?.let { ports += it.port }
                ports += runBlocking { finder.findPorts(AdbServiceTypes.PAIRING, 2_500) }
                for (port in ports) {
                    for (host in listOfNotNull("127.0.0.1", reading?.host)) {
                        try {
                            AdbPairing(tls).pair(host, port, digits)
                            prefs.paired = true
                            Log.i(TAG, "paired")
                            returnToApp()
                            publish()
                            return true
                        } catch (e: AdbPairingException) {
                            lastFailure = e
                            // A wrong typed code will not become right by trying again.
                            if (e.reason == AdbPairingException.Reason.WRONG_CODE && typedCode != null) {
                                typedCode = null
                                return false
                            }
                        } catch (e: Exception) {
                            lastFailure = e
                        }
                    }
                }
                if (ports.isEmpty()) Thread.sleep(800)
            }
            Log.w(TAG, "pairing gave up", lastFailure)
            return false
        } catch (e: Throwable) {
            Log.w(TAG, "pairing stopped", e)
            throw e
        } finally {
            Log.i(TAG, "pair ended guiding=${SettingsReading.guiding}")
            pairing = false
            typedCode = null
            SettingsReading.guiding = false
            SettingsReading.step = null
            SettingsReading.needs = null
            SettingsReading.service?.endGuide()
        }
    }

    /** Leaves the Settings screens pairing used and brings the App back in front of the person, on the Phone page. */
    private fun returnToApp() {
        SettingsReading.service?.takeIf { SettingsReading.guiding }?.leaveSettings()
        runCatching { app.startActivity(PhoneNotifier.phoneIntent(app)) }
    }

    // ---- helper ------------------------------------------------------------------------------

    private fun connectAdb(): AdbConnection {
        // Right after Wireless debugging was switched on its port may not be announced yet.
        // Waiting once for the whole announcement beats trying again after a short wait and a growing pause:
        // it ends when the port is announced, not at the next try (on an emulator that took one to two minutes).
        val keepTryingMs = if (justSwitchedOn) ANNOUNCE_WAIT_MS else 20_000L
        if (justSwitchedOn) noteProblem("Wireless debugging was switched back on; waiting for it to announce its port.")
        justSwitchedOn = false
        val tls = AndroidAdb.tls(AndroidAdb.identity(app))
        val cache = object : PortCache {
            override fun load() = prefs.adbPort.takeIf { it > 0 }
            override fun save(port: Int) { prefs.adbPort = port }
        }
        val connect = AdbConnectPort({ null }, finder, cache, keepTryingMs = keepTryingMs)
        return try {
            // A connection after the switch was turned on means it did not fall back on a prompt: no strike.
            runBlocking { connect.connect { port -> AdbConnection.open("127.0.0.1", port, tls) } }.also { promptGuard.switchHeld() }
        } catch (e: AdbConnectionException) {
            // The phone no longer knows our key (a revoked authorisation): pairing must be done again.
            if (e.reason == AdbConnectionException.Reason.UNAUTHORIZED) prefs.paired = false
            throw e
        }
    }

    /**
     * A connected helper: reuses the running one of this install, else starts a fresh one through
     * Wireless debugging (turning that back on first when this app may). A helper left by an earlier
     * install is only retired once a fresh one can be started; when it cannot (Wireless debugging is
     * off and this app may not turn it on), the old one keeps serving instead of leaving no helper.
     * Throws when there is none, with [problem] saying why.
     */
    fun ensureHelper(): HelperClient = synchronized(helperLock) {
        client?.takeIf { !it.isClosed }?.let { return it }
        try {
            findOrStartHelper().also { noteProblem(null) }
        } catch (e: Exception) {
            noteProblem(e.message ?: e.javaClass.simpleName)
            throw e
        }
    }

    /** The keeper's last word on why the helper is not up; null while it is up. */
    @Volatile var problem: String? = null
        private set

    /**
     * Why the phone is in setup, in a sentence, for the person's screen, the computer and the agent's
     * error: the keeper's reason, or (between the helper going and the keeper's next pass) the plain fact.
     * Null when the phone is not in setup.
     */
    fun setupReason(): String? = when {
        state() != "setup" -> null
        !prefs.enabled -> "Phone use is switched off in Mewla on the phone."
        else -> problem ?: "The helper is not running on the phone; Mewla is starting it again."
    }

    private fun noteProblem(message: String?) {
        if (problem == message) return
        problem = message
        Log.i(TAG, if (message == null) "ready: no problem" else "not ready: $message")
        if (::app.isInitialized) publish()
    }

    private fun findOrStartHelper(): HelperClient {
        val running = HelperClient.connect(HelperEndpoint.load(app))
        val hello = running?.let { runCatching { it.call("hello", timeoutMs = 5_000) }.getOrNull() }
        val kind = HelperAdoption.classify(hello, PhoneHelper.PROTOCOL, HelperLauncher.installStamp(app))
        if (running != null && kind == HelperKind.CURRENT) return use(running, hello!!, stale = false)
        try {
            return startHelper()
        } catch (e: Exception) {
            if (running != null && kind == HelperKind.STALE && !running.isClosed) {
                Log.i(TAG, "keeping the helper of an earlier install: ${e.message}")
                return use(running, hello!!, stale = true)
            }
            running?.close()
            throw e
        }
    }

    /** Starts a fresh helper, replacing any other. Everything that can fail without touching the old helper is checked first. */
    private fun startHelper(): HelperClient {
        if (!prefs.paired) throw IllegalStateException("not paired")
        ensureWirelessDebugging()
        val started = connectAdb().use { adb ->
            // An older helper (another version, or one a cleared app lost the token for) must not hold the display or the port.
            adb.exec("pkill -f 'mewla-phone-hel[p]er'").use { it.readAll() }
            // Before the start: a start that throws after the helper is already serving must not skip the grant for good.
            grantSelfHealing(adb)
            HelperLauncher.start(app, adb)
        }
        val hello = runCatching { started.call("hello", timeoutMs = 5_000) }.getOrNull()
        if (HelperAdoption.classify(hello, PhoneHelper.PROTOCOL, HelperLauncher.installStamp(app)) != HelperKind.CURRENT) {
            runCatching { started.call("shutdown", timeoutMs = 2_000) }
            started.close()
            throw IllegalStateException("the helper does not speak this version")
        }
        return use(started, hello!!, stale = false)
    }

    private var nextReplaceAt = 0L

    /** True while the helper in use is one an earlier install left; the keeper replaces it when it can. */
    @Volatile private var staleHelper = false

    /** Makes [candidate] the helper: its frames and its end are watched, and the display it already has is known. */
    private fun use(candidate: HelperClient, hello: JSONObject, stale: Boolean): HelperClient {
        staleHelper = stale
        candidate.onFrame = { jpeg ->
            if (!drawn) {
                drawn = true
                publish()
            }
            PhoneLiveViews.deliver(jpeg)
        }
        candidate.onClosed = {
            if (client === candidate) {
                client = null
                display = null
                resetPicture()
                publish()
                // The keeper looks at once why it went and what brings it back.
                kick()
            }
        }
        client = candidate
        if (hello.optInt("w") > 0) display = intArrayOf(hello.optInt("w"), hello.optInt("h"))
        publish()
        // Views mounted while the link was down asked for frames that could not be requested then.
        PhoneLiveViews.resume()
        return candidate
    }

    /**
     * Retires a helper an earlier install left, once a fresh one can be started. Only while no agent
     * is on the phone, since the fresh one takes the agent's display with it.
     */
    private fun replaceStaleHelper() = synchronized(helperLock) {
        if (!staleHelper || client?.isClosed != false || activeWorker != null) return@synchronized
        val now = android.os.SystemClock.elapsedRealtime()
        // Finding the Wireless debugging port takes seconds under the lock every request waits on: not every tick.
        if (now < nextReplaceAt) return@synchronized
        nextReplaceAt = now + REPLACE_EVERY_MS
        // Nothing is wrong with the helper in use, so this never raises the system's network prompt to replace it.
        if (Settings.Global.getInt(app.contentResolver, "adb_wifi_enabled", 0) != 1) return@synchronized
        runCatching { startHelper() }
            .onSuccess { Log.i(TAG, "replaced the helper of an earlier install") }
            .onFailure { Log.i(TAG, "keeping the helper of an earlier install: ${it.message}") }
    }

    /**
     * With WRITE_SECURE_SETTINGS (see [grantThroughWirelessDebugging]) Wireless debugging is turned back on
     * without the person. [HelperKeeper] does not get here when it cannot or may not (see [SetupRecovery]);
     * the person's own Start does, and then the words say what is missing.
     */
    private fun ensureWirelessDebugging() {
        if (wirelessDebuggingOn()) {
            promptGuard.switchHeld()
            return
        }
        if (!selfHealing) {
            throw IllegalStateException("Wireless debugging is off and Mewla has not been given the permission to switch it back on")
        }
        // On a network Android has not been told to allow, switching it on raises the system's prompt, and
        // if that is dismissed the switch falls back. Twice in a row means this network is the person's to
        // approve: stop raising the prompt for a few minutes rather than nag.
        if (!promptGuard.mayRaisePrompt(System.currentTimeMillis())) {
            throw IllegalStateException("Wireless debugging keeps switching itself off on this Wi-Fi network")
        }
        Settings.Global.putInt(app.contentResolver, "adb_wifi_enabled", 1)
        Thread.sleep(2_500)
        justSwitchedOn = true
    }

    private val promptGuard = PromptGuard()

    /** Wireless debugging was switched on just now: its port is announced a moment later, so finding it waits longer. */
    @Volatile private var justSwitchedOn = false

    /** The person asked for the helper (Start): the pause the keeper put on raising the system's network prompt does not apply to them. */
    fun startedByPerson() {
        promptGuard.personAsked()
    }

    /** The words of the last refused grant, for the reason shown while Wireless debugging is off; null when it was not refused. */
    @Volatile private var grantFailure: String? = null

    /** Through the phone's own shell: lets this App turn Wireless debugging back on and its accessibility service on without the person. Returns why it did not work, or null. */
    private fun grantSelfHealing(adb: AdbConnection): String? {
        if (selfHealing) return null
        val outcome = runCatching { adb.exec("pm grant ${app.packageName} $WRITE_SECURE").use { it.readText() } }
            .onSuccess { Log.i(TAG, "pm grant WRITE_SECURE_SETTINGS: '${it.trim()}' granted=${selfHealing}") }
            .onFailure { Log.w(TAG, "pm grant WRITE_SECURE_SETTINGS failed", it) }
        grantFailure = if (selfHealing) null else {
            val said = outcome.getOrNull()?.trim()?.lineSequence()?.firstOrNull { it.isNotBlank() } ?: outcome.exceptionOrNull()?.message
            "the phone refused the grant" + (said?.let { ": ${it.take(100)}" } ?: "")
        }
        return grantFailure
    }

    /**
     * Gets the grant when a helper is already serving (adopted from before this install, or started by a
     * run that never reached the grant) and Wireless debugging is still on: after that it is switched back
     * on by itself, whatever turned it off.
     */
    private fun grantThroughWirelessDebugging(): String? = try {
        connectAdb().use { grantSelfHealing(it) }
    } catch (e: Exception) {
        Log.i(TAG, "cannot get the grant now: ${e.message}")
        grantFailure = "no connection to Wireless debugging to ask: ${e.message ?: e.javaClass.simpleName}".take(160)
        grantFailure
    }.also { if (it == null) publish() }

    /** Whether this App may write secure settings (the grant above happened). */
    val selfHealing: Boolean
        get() = app.checkSelfPermission(WRITE_SECURE) == android.content.pm.PackageManager.PERMISSION_GRANTED

    private fun stopHelper() {
        val c = client ?: return
        client = null
        display = null
        resetPicture()
        staleHelper = false
        runCatching { c.call("shutdown", timeoutMs = 2_000) }
        c.close()
    }

    // ---- the person's controls ---------------------------------------------------------------

    fun stop() {
        stopped = true
        handoff?.done?.countDown()
        emit("stop")
        publish()
    }

    fun takeOver() {
        personInControl = true
        emit("take_over")
        publish()
    }

    fun giveBack() {
        personInControl = false
        stopped = false
        handoff?.done?.countDown()
        emit("give_back")
        publish()
    }

    /** The person finished what a handoff asked of them. */
    fun handoffDone() {
        val pending = handoff ?: return
        emit("handed_off", pending.id)
        pending.done.countDown()
    }

    private fun emit(kind: String, id: String? = null) {
        link?.send(PhoneFrames.event(kind, id))
    }

    /** A live view asks for frames; the helper only spends the battery while someone watches. */
    fun watch(on: Boolean, fps: Int = 12, maxWidth: Int = 540): Boolean {
        val c = client ?: return false
        if (display == null) return false
        watching = on
        return runCatching { c.call("watch", JSONObject().put("on", on).put("fps", fps).put("maxWidth", maxWidth)).optBoolean("ok") }.getOrDefault(false)
    }

    /** A touch of the person driving the agent's screen. */
    fun touch(phase: String, x: Double, y: Double): Boolean {
        val c = client ?: return false
        return runCatching { c.call("touch", JSONObject().put("phase", phase).put("x", x).put("y", y), 3_000).optBoolean("ok") }.getOrDefault(false)
    }

    // ---- the daemon's requests ---------------------------------------------------------------

    private fun failure(id: String, code: String, error: String) = PhoneFrames.failure(id, code, error)

    /** Runs one request from the daemon and returns its result frame; never throws. */
    fun execute(request: JSONObject): JSONObject {
        val started = System.currentTimeMillis()
        val result = executeTimed(request)
        val took = System.currentTimeMillis() - started
        if (took > 3_000) Log.i(TAG, "${request.optString("op")} ${if (result.optBoolean("ok")) "ok" else result.optString("code")} took $took ms")
        return result
    }

    /** The agent is done: its display goes, and the App stops showing it, whether or not the helper is up. */
    private fun release(id: String): JSONObject {
        client?.takeIf { !it.isClosed }?.let { runCatching { it.call("display_release") } }
        display = null
        resetPicture()
        activeWorker = null
        publish()
        return JSONObject().put("type", "result").put("id", id).put("ok", true)
    }

    private fun executeTimed(request: JSONObject): JSONObject {
        val id = request.optString("id")
        val op = request.optString("op")
        try {
            // Giving the screen back needs no helper and is never refused: a helper that is down or a
            // person who stopped the agent still leaves the float showing a Worker that is gone.
            if (op == "release") return release(id)
            if (stopped) return failure(id, "stopped", "The person stopped you on their phone. Stop and report.")
            if (personInControl) {
                return failure(id, "person_in_control", "The person is using the agent's screen on their phone. Wait, or report.")
            }
            val helper = try {
                ensureHelper()
            } catch (e: Exception) {
                return failure(id, "not_ready", "Phone use isn't ready on the phone: ${setupReason() ?: e.message ?: "the helper is not up"}")
            }
            keepScreenOn()
            activeWorker = request.optString("worker_name").ifEmpty { request.optString("worker") }.ifEmpty { "An agent" }
            publish()
            val args = request.optJSONObject("args") ?: JSONObject()
            val wantImage = request.optBoolean("image", false)
            return when (op) {
                "apps" -> {
                    val reply = helper.call("apps")
                    if (!reply.optBoolean("ok")) failure(id, "failed", reply.optString("error"))
                    else JSONObject().put("type", "result").put("id", id).put("ok", true).put("apps", reply.getJSONArray("apps"))
                }
                "handoff" -> handoff(id, request, helper, args, wantImage)
                else -> act(id, op, helper, args, wantImage)
            }
        } catch (e: Exception) {
            Log.w(TAG, "$op failed", e)
            return failure(id, "failed", e.message ?: e.javaClass.simpleName)
        }
    }

    private val screenLock by lazy {
        @Suppress("DEPRECATION")
        app.getSystemService(PowerManager::class.java).newWakeLock(PowerManager.SCREEN_DIM_WAKE_LOCK, "mewla:phone-use")
            .apply { setReferenceCounted(false) }
    }

    /**
     * While an agent works, a screen that is on stays on: the screen reading stops while the phone
     * sleeps. This never wakes a screen the person turned off.
     */
    private fun keepScreenOn() {
        runCatching { screenLock.acquire(90_000) }
    }

    private fun act(id: String, op: String, helper: HelperClient, args: JSONObject, wantImage: Boolean): JSONObject {
        ensureDisplay(helper)
        var settleQuiet = 300L
        var settleMax = 2_500L
        var settleMin = 500L
        // An app that draws its own UI answers in bursts after a lull; settle on more quiet there.
        if (opaqueScreen) {
            settleQuiet = 600
            settleMin = 1_000
        }
        var note: String? = null
        when (op) {
            "screen" -> {}
            "launch" -> {
                val label = appLabel(args.getString("package"), args.optInt("user", 0))
                opening = label
                launchFailed = null
                publish()
                val reply = try {
                    helper.call("launch", JSONObject().put("package", args.getString("package")).put("user", args.optInt("user", 0)), 30_000)
                } finally {
                    opening = null
                }
                if (!reply.optBoolean("ok")) {
                    launchFailed = label
                    publish()
                    return failure(id, reply.optString("code", "failed").ifEmpty { "failed" }, reply.optString("error"))
                }
                publish()
                settleQuiet = 500
                settleMax = 5_000
                settleMin = 2_000
            }
            "tap" -> helper.call("tap", JSONObject().put("x", toDisplay(args.getInt("x"), 0)).put("y", toDisplay(args.getInt("y"), 1)).put("long", args.optBoolean("long")))
                .let { if (!it.optBoolean("ok")) return failure(id, "failed", it.optString("error")) }
            "swipe" -> helper.call(
                "swipe",
                JSONObject().put("x1", toDisplay(args.getInt("x1"), 0)).put("y1", toDisplay(args.getInt("y1"), 1))
                    .put("x2", toDisplay(args.getInt("x2"), 0)).put("y2", toDisplay(args.getInt("y2"), 1))
                    .put("ms", args.optInt("ms", 300)),
            ).let { if (!it.optBoolean("ok")) return failure(id, "failed", it.optString("error")) }
            "text" -> helper.call("text", JSONObject().put("value", args.getString("value")).put("replace", args.optBoolean("replace"))).let {
                if (!it.optBoolean("ok")) return failure(id, "failed", it.optString("error"))
                note = typedNote(it.optString("method"), it.optBoolean("verified"))
            }
            "key" -> helper.call("key", JSONObject().put("name", args.getString("name")))
                .let { if (!it.optBoolean("ok")) return failure(id, "failed", it.optString("error")) }
            "wait" -> {
                val ms = args.optLong("ms", 0)
                if (ms > 0) Thread.sleep(ms.coerceAtMost(10_000)) else settleMax = 4_000
            }
            else -> return failure(id, "failed", "Unknown phone op $op")
        }
        if (op != "screen") helper.call("settle", JSONObject().put("quiet_ms", settleQuiet).put("timeout_ms", settleMax).put("min_wait_ms", settleMin), settleMax + 3_000)
        return snapshot(id, helper, wantImage, acted = op != "screen" && op != "wait").also { result ->
            note?.let { result.put("note", listOf(it, result.optString("note")).filter { n -> n.isNotEmpty() }.joinToString(" ")) }
        }
    }

    private fun typedNote(method: String, verified: Boolean): String? = when {
        method == "set_text" && verified -> null
        method == "set_text" -> "Typed by setting the field's text; it could not be read back to check."
        else -> "The field did not take set-text, so the text went in by $method" + if (verified) "." else "; it could not be read back to check."
    }

    private fun handoff(id: String, request: JSONObject, helper: HelperClient, args: JSONObject, wantImage: Boolean): JSONObject {
        ensureDisplay(helper)
        val pending = Handoff(id, args.optString("reason").ifEmpty { "Help the agent" })
        handoff = pending
        personInControl = true
        publish()
        PhoneNotifier.handoff(app, pending.reason, request.optString("worker_name"))
        val timeoutMs = args.optLong("timeout_ms", 0).takeIf { it > 0 } ?: (10 * 60_000L)
        val finished = pending.done.await(timeoutMs, TimeUnit.MILLISECONDS)
        handoff = null
        PhoneNotifier.clearHandoff(app)
        val wasStopped = stopped
        personInControl = false
        publish()
        if (wasStopped) return failure(id, "stopped", "The person stopped you on their phone. Stop and report.")
        if (!finished) return failure(id, "failed", "The person did not finish in time.")
        helper.call("settle", JSONObject().put("quiet_ms", 400).put("timeout_ms", 3_000), 6_000)
        return snapshot(id, helper, wantImage)
    }

    /** The agent's display at the phone's real pixels and density, so apps lay out exactly as they do for the person. */
    private fun ensureDisplay(helper: HelperClient) {
        // Always asked, never assumed: the helper answers at once for a live display of this size and makes
        // a new one when the system dropped the old (a link drop or the screen going off can leave a stale one).
        val real = DisplayMetrics().also {
            app.getSystemService(DisplayManager::class.java).getDisplay(Display.DEFAULT_DISPLAY).getRealMetrics(it)
        }
        val w = minOf(real.widthPixels, real.heightPixels)
        val h = maxOf(real.widthPixels, real.heightPixels)
        val dpi = real.densityDpi
        val reply = helper.call("display", JSONObject().put("w", w).put("h", h).put("dpi", dpi))
        if (!reply.optBoolean("ok")) throw IllegalStateException(reply.optString("error", "no display"))
        displayInfo = "${w}x$h asked ${dpi}dpi, system ${reply.optInt("effective_dpi")}dpi; phone ${reply.optInt("main_w")}x${reply.optInt("main_h")} ${reply.optInt("main_dpi")}dpi (app sees ${app.resources.displayMetrics.densityDpi}dpi); agent sees ${minOf(w, MODEL_IMAGE_WIDTH)} wide"
        Log.i(TAG, "agent display $displayInfo")
        display = intArrayOf(w, h)
        publish()
        PhoneLiveViews.resume()
    }

    /** The agent display's size and density next to the phone's own, as the daemon is told it. */
    @Volatile private var displayInfo: String? = null

    /** Whether the last reading had nothing the agent could name: an app that draws its own UI. */
    @Volatile private var opaqueScreen = false

    /** What is on the agent's display now, in the daemon's Screen shape, with a picture when asked or needed. */
    private fun snapshot(id: String, helper: HelperClient, wantImage: Boolean, acted: Boolean = false): JSONObject {
        val tree = helper.call("tree")
        if (!tree.optBoolean("ok")) return failure(id, "failed", tree.optString("error"))
        val windows = tree.optJSONArray("windows") ?: JSONArray()
        val picture = pictureSize() ?: intArrayOf(tree.optInt("w"), tree.optInt("h"))
        for (i in 0 until windows.length()) {
            val window = windows.getJSONObject(i)
            toPicture(window.optJSONArray("bounds"))
            val nodes = window.optJSONArray("nodes") ?: continue
            for (j in 0 until nodes.length()) toPicture(nodes.getJSONObject(j).optJSONArray("b"))
        }
        val interactive = tree.optBoolean("interactive", true)
        val result = JSONObject().put("type", "result").put("id", id).put("ok", true)
        val screenOff = windows.length() == 0 && !interactive
        var elements = 0
        for (i in 0 until windows.length()) elements += windows.getJSONObject(i).optJSONArray("nodes")?.length() ?: 0
        // With nothing to read the picture is the screen: send it now, not after another round trip.
        opaqueScreen = !screenOff && elements == 0
        if (screenOff) {
            result.put("ok", false).put("code", "screen_off")
                .put("error", "The phone's own screen is off, so the screen cannot be read as elements. The picture is current; the person can wake the phone.")
        } else {
            result.put("screen", JSONObject().put("w", picture[0]).put("h", picture[1]).put("windows", windows))
        }
        // After an action on an app with nothing to read the picture is the answer: send it now, not after
        // another round trip. A plain read of such a screen is asked again with a picture only if needed
        // (a wait polls it, and a picture each time would be thrown away).
        if (wantImage || screenOff || (opaqueScreen && acted)) {
            val shot = helper.call("screenshot", JSONObject().put("maxWidth", picture[0]).put("quality", 60), 15_000)
            if (shot.optBoolean("ok")) result.put("image", shot.getString("jpeg"))
        }
        // The display draws only when something changes. After an action with no new frame the picture is
        // the screen from before it; say so, so the agent waits and reads again instead of trusting it.
        if (acted && !tree.optBoolean("drew_since_action", true)) {
            result.put("note", "Nothing was drawn on the screen after the action: the screen may be unchanged, or the app has not redrawn yet. Wait a moment and read again before acting on it.")
        }
        return result
    }
}
