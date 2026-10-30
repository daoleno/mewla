package expo.modules.mewlaphone

import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.os.Build
import android.os.PowerManager
import android.provider.Settings
import android.util.Log
import android.view.WindowManager
import expo.modules.mewlaphone.adb.AdbConnectPort
import expo.modules.mewlaphone.adb.AdbConnection
import expo.modules.mewlaphone.adb.AdbConnectionException
import expo.modules.mewlaphone.adb.AdbPairing
import expo.modules.mewlaphone.adb.AdbPairingException
import expo.modules.mewlaphone.adb.AdbServiceTypes
import expo.modules.mewlaphone.adb.AndroidAdb
import expo.modules.mewlaphone.adb.NsdAdbServiceFinder
import expo.modules.mewlaphone.adb.PortCache
import expo.modules.mewlaphone.helper.PhoneHelper
import kotlinx.coroutines.runBlocking
import org.json.JSONArray
import org.json.JSONObject
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * Phone use on this phone: the setup it needs, the helper it keeps running, and the one entry the
 * daemon's requests come through. Everything here is plain Android, so the Expo module stays a
 * thin shell around it.
 */
object PhoneController {
    private const val TAG = "MewlaPhone"
    private const val MIN_SDK = Build.VERSION_CODES.R
    private const val STALL_MS = 25_000L
    private const val WRITE_SECURE = "android.permission.WRITE_SECURE_SETTINGS"

    /** Told what the App must show or forward to the daemon; called from worker threads. */
    interface Listener {
        fun onStatus(status: JSONObject)
        fun onEvent(event: JSONObject)
        fun onFrame(jpeg: ByteArray)
    }

    private lateinit var app: Context
    private lateinit var prefs: PhonePrefs
    @Volatile var listener: Listener? = null

    /** Whether the App's signed connection to the computer is up: the notification says so, since the daemon's requests only come through it. */
    @Volatile var linkOnline = false
        set(value) {
            field = value
            if (::app.isInitialized) publish()
        }

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

    private class Handoff(val id: String, val reason: String) {
        val done = CountDownLatch(1)
    }

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
        }
    }

    // ---- keeping the link up -----------------------------------------------------------------

    private var linkStartedAt = 0L

    /**
     * The computer reaches the phone through the App's JavaScript link. React Native stops its timers
     * while the App is in the background, and the link's address probing and retries wait on them, so
     * a link that drops there (the computer restarted) would stay down until the App came to the front.
     * While the link is down and the App is not in front, this runs a headless task, which keeps the
     * timers going until the link is back; with no screen at all (after a reboot) it also starts the
     * JavaScript. Backed off (see [LinkRetry]) so a computer that is off costs next to nothing.
     */
    @Synchronized
    fun ensureLink() {
        if (!::app.isInitialized || !prefs.enabled) return
        if (linkOnline) {
            linkAttempts = 0
            return
        }
        // With the App in front its JavaScript timers run and the link retries by itself.
        if (appForeground) return
        val now = android.os.SystemClock.elapsedRealtime()
        if (linkStartedAt != 0L && now - linkStartedAt < LinkRetry.delayMs(linkAttempts)) return
        linkStartedAt = now
        linkAttempts += 1
        runCatching { PhoneLinkTaskService.start(app) }.onFailure { Log.w(TAG, "cannot start the link", it) }
    }

    private var linkAttempts = 0

    /** Whether the App's screen is in front: set by the module as the activity comes and goes. */
    @Volatile var appForeground = false

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
        keeper = Thread({ keepHelper() }, "mewla-phone-keeper").apply { isDaemon = true; start() }
    }

    private fun keepHelper() {
        var wait = 1_000L
        try {
            while (prefs.enabled) {
                // React Native stops JS timers while the App is in the background, so the link's own
                // retry never fires there. A link that is down is told to try again from here.
                if (!linkOnline) {
                    listener?.onEvent(JSONObject().put("type", "tick"))
                    ensureLink()
                }
                if (client?.isClosed == false) {
                    wait = 5_000L
                } else if (prefs.paired && !pairing && onWifi()) {
                    val up = runCatching { ensureHelper() }.onFailure { Log.i(TAG, "helper not up yet: ${it.message}") }.isSuccess
                    publish()
                    wait = if (up) 5_000L else (wait * 2).coerceIn(4_000L, 120_000L)
                } else {
                    wait = 5_000L
                }
                Thread.sleep(wait)
            }
        } catch (_: InterruptedException) {
        }
    }

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
            .put("developer_options", developer)
            .put("wireless_debugging", wireless)
            .put("wifi", onWifi())
            .put("paired", prefs.paired)
            .put("helper", helperUp)
            .put("accessibility", SettingsReading.serviceRunning)
            .put("restricted", accessibilityRestricted())
            .put("self_healing", selfHealing)
            .put("guide", guide())
            .put("notifications", PhoneNotifier.canNotify(app))
            .put("state", state())
            .put("w", display?.get(0) ?: 0)
            .put("h", display?.get(1) ?: 0)
            .put("agent", activeWorker ?: JSONObject.NULL)
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

    /** True when Android blocks switching a sideloaded app's accessibility service on until "Allow restricted settings" is chosen in App info. */
    private fun accessibilityRestricted(): Boolean = runCatching {
        if (Build.VERSION.SDK_INT < 33 || SettingsReading.serviceRunning) return@runCatching false
        val ops = app.getSystemService(android.app.AppOpsManager::class.java)
        ops.unsafeCheckOpNoThrow("android:access_restricted_settings", android.os.Process.myUid(), app.packageName) !=
            android.app.AppOpsManager.MODE_ALLOWED
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
        } else {
            PhoneUseService.stop(app)
            stopHelper()
        }
        publish()
    }

    val enabled: Boolean get() = prefs.enabled

    /** The agent's display size, or null before it exists. */
    fun displaySize(): IntArray? = display

    private fun onWifi(): Boolean = runCatching {
        val cm = app.getSystemService(ConnectivityManager::class.java)
        @Suppress("DEPRECATION")
        cm.allNetworks.any { cm.getNetworkCapabilities(it)?.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) == true }
    }.getOrDefault(false)

    fun publish() {
        listener?.onStatus(status())
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
        pausedUntil = 0
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
        } finally {
            pairing = false
            typedCode = null
            SettingsReading.guiding = false
            SettingsReading.step = null
            SettingsReading.needs = null
            SettingsReading.service?.endGuide()
        }
    }

    /** Leaves the Settings screens pairing used and brings the App back in front of the person. */
    private fun returnToApp() {
        SettingsReading.service?.takeIf { SettingsReading.guiding }?.leaveSettings()
        runCatching {
            app.startActivity(
                android.content.Intent(android.content.Intent.ACTION_VIEW, android.net.Uri.parse("mewla://phone"))
                    .setPackage(app.packageName)
                    .addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK or android.content.Intent.FLAG_ACTIVITY_SINGLE_TOP),
            )
        }
    }

    // ---- helper ------------------------------------------------------------------------------

    private fun connectAdb(): AdbConnection {
        val tls = AndroidAdb.tls(AndroidAdb.identity(app))
        val cache = object : PortCache {
            override fun load() = prefs.adbPort.takeIf { it > 0 }
            override fun save(port: Int) { prefs.adbPort = port }
        }
        val connect = AdbConnectPort({ null }, finder, cache)
        return try {
            runBlocking { connect.connect { port -> AdbConnection.open("127.0.0.1", port, tls) } }
        } catch (e: AdbConnectionException) {
            // The phone no longer knows our key (a revoked authorisation): pairing must be done again.
            if (e.reason == AdbConnectionException.Reason.UNAUTHORIZED) prefs.paired = false
            throw e
        }
    }

    /**
     * A connected helper of this app's version: reuses the running one, else starts it through
     * Wireless debugging (turning that back on first when this app may). Throws when it cannot.
     */
    fun ensureHelper(): HelperClient = synchronized(helperLock) {
        client?.takeIf { !it.isClosed }?.let { return it }
        val endpoint = HelperEndpoint.load(app)
        HelperClient.connect(endpoint)?.let { existing ->
            if (adopt(existing)) return existing
        }
        if (!prefs.paired) throw IllegalStateException("not paired")
        ensureWirelessDebugging()
        val started = connectAdb().use { adb ->
            // An older helper (another version, or one a cleared app lost the token for) must not hold the display or the port.
            adb.exec("pkill -f 'mewla-phone-hel[p]er'").use { it.readAll() }
            val helper = HelperLauncher.start(app, adb)
            grantSelfHealing(adb)
            helper
        }
        if (!adopt(started)) throw IllegalStateException("the helper does not speak this version")
        started
    }

    /** Keeps [candidate] when it is this App install's helper; otherwise shuts it down so a fresh one can start. */
    private fun adopt(candidate: HelperClient): Boolean {
        val hello = runCatching { candidate.call("hello", timeoutMs = 5_000) }.getOrNull()
        if (hello?.optInt("protocol") != PhoneHelper.PROTOCOL || hello.optLong("build") != HelperLauncher.installStamp(app)) {
            runCatching { candidate.call("shutdown", timeoutMs = 2_000) }
            candidate.close()
            return false
        }
        candidate.onFrame = { jpeg -> listener?.onFrame(jpeg) }
        candidate.onClosed = {
            if (client === candidate) {
                client = null
                display = null
                publish()
            }
        }
        client = candidate
        if (hello.optInt("w") > 0) display = intArrayOf(hello.optInt("w"), hello.optInt("h"))
        publish()
        // Views mounted while the link was down asked for frames that could not be requested then.
        PhoneLiveViews.resume()
        return true
    }

    /** With WRITE_SECURE_SETTINGS (granted once through the helper) Wireless debugging is turned back on without the person. */
    private fun ensureWirelessDebugging() {
        if (Settings.Global.getInt(app.contentResolver, "adb_wifi_enabled", 0) == 1) {
            strikes = 0
            return
        }
        if (!selfHealing) {
            throw IllegalStateException("Wireless debugging is off")
        }
        // On a network Android has not been told to allow, switching it on raises the system's prompt, and
        // if that is dismissed the switch falls back. Twice in a row means this network is the person's to
        // approve: stop raising the prompt for a while rather than nag.
        val now = System.currentTimeMillis()
        if (now < pausedUntil) throw IllegalStateException("Wireless debugging is off on this network")
        strikes = if (switchedOnAt > 0 && now - switchedOnAt < 90_000) strikes + 1 else 0
        if (strikes >= 2) {
            pausedUntil = now + 30 * 60_000
            strikes = 0
            throw IllegalStateException("Wireless debugging is off on this network")
        }
        switchedOnAt = now
        Settings.Global.putInt(app.contentResolver, "adb_wifi_enabled", 1)
        Thread.sleep(2_500)
    }

    private var strikes = 0
    private var switchedOnAt = 0L
    @Volatile private var pausedUntil = 0L

    /** Once, through the helper's own shell: lets this App turn Wireless debugging back on and its accessibility service on without the person. */
    private fun grantSelfHealing(adb: AdbConnection) {
        if (app.checkSelfPermission(WRITE_SECURE) == android.content.pm.PackageManager.PERMISSION_GRANTED) return
        runCatching { adb.exec("pm grant ${app.packageName} $WRITE_SECURE").use { it.readText() } }
            .onSuccess { Log.i(TAG, "pm grant WRITE_SECURE_SETTINGS: '${it.trim()}' granted=${selfHealing}") }
            .onFailure { Log.w(TAG, "pm grant WRITE_SECURE_SETTINGS failed", it) }
    }

    /** Whether this App may write secure settings (the grant above happened). */
    val selfHealing: Boolean
        get() = app.checkSelfPermission(WRITE_SECURE) == android.content.pm.PackageManager.PERMISSION_GRANTED

    private fun stopHelper() {
        val c = client ?: return
        client = null
        display = null
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
        listener?.onEvent(JSONObject().put("type", "event").put("kind", kind).apply { id?.let { put("id", it) } })
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

    private fun failure(id: String, code: String, error: String) =
        JSONObject().put("type", "result").put("id", id).put("ok", false).put("code", code).put("error", error)

    /** Runs one request from the daemon and returns its result frame; never throws. */
    fun execute(request: JSONObject): JSONObject {
        val started = System.currentTimeMillis()
        val result = executeTimed(request)
        val took = System.currentTimeMillis() - started
        if (took > 3_000) Log.i(TAG, "${request.optString("op")} ${if (result.optBoolean("ok")) "ok" else result.optString("code")} took $took ms")
        return result
    }

    private fun executeTimed(request: JSONObject): JSONObject {
        val id = request.optString("id")
        val op = request.optString("op")
        try {
            if (stopped) return failure(id, "stopped", "The person stopped you on their phone. Stop and report.")
            if (personInControl && op != "release") {
                return failure(id, "person_in_control", "The person is using the agent's screen on their phone. Wait, or report.")
            }
            val helper = try {
                ensureHelper()
            } catch (e: Exception) {
                return failure(id, "not_ready", "Phone use is not ready on the phone: ${e.message}")
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
                    else JSONObject().put("type", "result").put("id", id).put("ok", true).put("apps", reply.getJSONArray("apps").also { dropSelf(it) })
                }
                "release" -> {
                    helper.call("display_release")
                    display = null
                    activeWorker = null
                    publish()
                    JSONObject().put("type", "result").put("id", id).put("ok", true)
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

    /** Mewla itself is never something an agent should open on its own screen. */
    private fun dropSelf(apps: JSONArray) {
        for (i in apps.length() - 1 downTo 0) {
            if (apps.getJSONObject(i).optString("package") == app.packageName) apps.remove(i)
        }
    }

    private fun act(id: String, op: String, helper: HelperClient, args: JSONObject, wantImage: Boolean): JSONObject {
        ensureDisplay(helper)
        var settleQuiet = 300L
        var settleMax = 2_500L
        var settleMin = 700L
        when (op) {
            "screen" -> {}
            "launch" -> {
                if (args.optString("package") == app.packageName) return failure(id, "failed", "Mewla's own screen is not for agents.")
                val reply = helper.call("launch", JSONObject().put("package", args.getString("package")), 30_000)
                if (!reply.optBoolean("ok")) return failure(id, reply.optString("code", "failed").ifEmpty { "failed" }, reply.optString("error"))
                settleQuiet = 500
                settleMax = 5_000
                settleMin = 2_000
            }
            "tap" -> helper.call("tap", JSONObject().put("x", args.getInt("x")).put("y", args.getInt("y")).put("long", args.optBoolean("long")))
                .let { if (!it.optBoolean("ok")) return failure(id, "failed", it.optString("error")) }
            "swipe" -> helper.call(
                "swipe",
                JSONObject().put("x1", args.getInt("x1")).put("y1", args.getInt("y1")).put("x2", args.getInt("x2")).put("y2", args.getInt("y2"))
                    .put("ms", args.optInt("ms", 300)),
            ).let { if (!it.optBoolean("ok")) return failure(id, "failed", it.optString("error")) }
            "text" -> helper.call("text", JSONObject().put("value", args.getString("value")).put("replace", args.optBoolean("replace")))
                .let { if (!it.optBoolean("ok")) return failure(id, "failed", it.optString("error")) }
            "key" -> helper.call("key", JSONObject().put("name", args.getString("name")))
                .let { if (!it.optBoolean("ok")) return failure(id, "failed", it.optString("error")) }
            "wait" -> {
                val ms = args.optLong("ms", 0)
                if (ms > 0) Thread.sleep(ms.coerceAtMost(10_000)) else settleMax = 4_000
            }
            else -> return failure(id, "failed", "Unknown phone op $op")
        }
        if (op != "screen") helper.call("settle", JSONObject().put("quiet_ms", settleQuiet).put("timeout_ms", settleMax).put("min_wait_ms", settleMin), settleMax + 3_000)
        return snapshot(id, helper, wantImage)
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

    /** The agent's display at roughly the size of this phone's, so apps lay out as they do for the person. */
    private fun ensureDisplay(helper: HelperClient) {
        // Always asked, never assumed: the helper answers at once for a live display of this size and makes
        // a new one when the system dropped the old (a link drop or the screen going off can leave a stale one).
        val bounds = app.getSystemService(WindowManager::class.java).maximumWindowMetrics.bounds
        val width = minOf(bounds.width(), bounds.height()).toDouble()
        val height = maxOf(bounds.width(), bounds.height()).toDouble()
        val scale = minOf(1.0, 720.0 / width)
        val w = even(width * scale)
        val h = even(height * scale)
        val dpi = (app.resources.displayMetrics.densityDpi * scale).toInt().coerceAtLeast(160)
        val reply = helper.call("display", JSONObject().put("w", w).put("h", h).put("dpi", dpi))
        if (!reply.optBoolean("ok")) throw IllegalStateException(reply.optString("error", "no display"))
        display = intArrayOf(w, h)
        publish()
        PhoneLiveViews.resume()
    }

    private fun even(v: Double) = (Math.round(v / 2.0) * 2).toInt()

    /** What is on the agent's display now, in the daemon's Screen shape, with a picture when asked or needed. */
    private fun snapshot(id: String, helper: HelperClient, wantImage: Boolean): JSONObject {
        val tree = helper.call("tree")
        if (!tree.optBoolean("ok")) return failure(id, "failed", tree.optString("error"))
        val windows = tree.optJSONArray("windows") ?: JSONArray()
        val interactive = tree.optBoolean("interactive", true)
        val result = JSONObject().put("type", "result").put("id", id).put("ok", true)
        val screenOff = windows.length() == 0 && !interactive
        if (screenOff) {
            result.put("ok", false).put("code", "screen_off")
                .put("error", "The phone's own screen is off, so the screen cannot be read as elements. The picture is current; the person can wake the phone.")
        } else {
            result.put("screen", JSONObject().put("w", tree.optInt("w")).put("h", tree.optInt("h")).put("windows", windows))
        }
        if (wantImage || screenOff) {
            val shot = helper.call("screenshot", JSONObject().put("maxWidth", tree.optInt("w", 720)).put("quality", 60), 15_000)
            if (shot.optBoolean("ok")) result.put("image", shot.getString("jpeg"))
        }
        return result
    }
}
