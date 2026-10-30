package expo.modules.mewlaphone

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.app.PendingIntent
import android.app.PictureInPictureParams
import android.app.RemoteAction
import android.content.pm.PackageManager
import android.graphics.drawable.Icon
import android.os.Build
import android.util.Rational
import android.provider.Settings
import expo.modules.kotlin.exception.CodedException
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition
import org.json.JSONObject

/** The App's door to phone use. The daemon's requests and the person's controls come through here; the work is in [PhoneController]. */
class MewlaPhoneModule : Module() {
    private val context: Context
        get() = appContext.reactContext ?: throw CodedException("ERR_NO_CONTEXT", "React context is not available", null)

    override fun definition() = ModuleDefinition {
        Name("MewlaPhone")
        Events("onStatus", "onEvent")

        OnCreate {
            PhoneController.init(context)
            PhoneController.listener = object : PhoneController.Listener {
                override fun onStatus(status: JSONObject) = sendEvent("onStatus", mapOf("json" to status.toString()))
                override fun onEvent(event: JSONObject) = sendEvent("onEvent", mapOf("json" to event.toString()))
                override fun onFrame(jpeg: ByteArray) = PhoneLiveViews.deliver(jpeg)
            }
        }
        OnDestroy { PhoneController.listener = null }

        // Take over from a notification or the floating window arrives as the intent that opens the page.
        OnNewIntent { intent -> takeOverFrom(intent) }
        OnActivityEntersForeground { appContext.currentActivity?.intent?.let { takeOverFrom(it) } }

        /** The live checks and state, as JSON (see PhoneController.status). */
        Function("status") { PhoneController.status().toString() }

        /** Phone use on or off: the foreground service, the helper and the pairing reader follow. */
        Function("setEnabled") { enabled: Boolean ->
            setAccessibilityComponent(enabled)
            PhoneController.setEnabled(enabled)
            // Once the secure-settings grant exists, nothing is asked of the person again.
            if (enabled) runCatching { PhoneController.enableAccessibility() }
        }

        /** Pairs with this phone's own Wireless debugging; the code is read off the screen when null. */
        AsyncFunction("pair") { code: String? ->
            // Without the accessibility reading the person types the digits into a notification.
            if (code == null && !SettingsReading.serviceRunning) PhoneNotifier.pairingCode(context)
            val ok = PhoneController.pair(code)
            PhoneNotifier.clearPairingCode(context)
            if (ok) runCatching { PhoneController.ensureHelper() }
            PhoneController.publish()
            ok
        }

        /** Turns the accessibility service on by itself when the secure-settings grant is there; false when the person must do it in Settings. */
        Function("enableAccessibility") { PhoneController.enableAccessibility() }

        AsyncFunction("ensureHelper") {
            try {
                PhoneController.ensureHelper()
                true
            } catch (e: Exception) {
                false
            } finally {
                PhoneController.publish()
            }
        }

        /** One request from the daemon (a JSON frame); returns the result frame, never throws. */
        AsyncFunction("execute") { request: String -> PhoneController.execute(JSONObject(request)).toString() }

        /** Whether the App's connection to the computer is up, for the notification. */
        Function("setLink") { online: Boolean -> PhoneController.linkOnline = online }

        Function("takeOver") { PhoneController.takeOver() }
        Function("giveBack") { PhoneController.giveBack() }
        Function("stop") { PhoneController.stop() }
        Function("handoffDone") { PhoneController.handoffDone() }

        /** Floats the agent's screen in a small window over other apps. */
        Function("enterPip") { w: Int, h: Int ->
            val activity = appContext.currentActivity
            if (activity == null || Build.VERSION.SDK_INT < Build.VERSION_CODES.O) false
            else {
                PipOverlay.follow(activity, appContext)
                activity.enterPictureInPictureMode(pipParams(w, h, false))
            }
        }

        /** While an agent works and the Phone page is open, leaving the App floats the screen by itself. */
        Function("setPipAuto") { on: Boolean, w: Int, h: Int ->
            val activity = appContext.currentActivity
            if (activity != null && Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                if (on) PipOverlay.follow(activity, appContext)
                activity.setPictureInPictureParams(pipParams(w, h, on))
            }
        }

        Function("openWirelessDebugging") { openWirelessDebugging() }
        Function("openDeveloperSettings") {
            val about = Intent(Settings.ACTION_DEVICE_INFO_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            context.startActivity(about)
        }
        Function("openAppSettings") {
            context.startActivity(
                Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, android.net.Uri.parse("package:" + context.packageName))
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
            )
        }
        Function("openAccessibilitySettings") {
            // Mewla's own entry when the phone has the page for it, else the list.
            val own = Intent("android.settings.ACCESSIBILITY_DETAILS_SETTINGS")
                .putExtra(Intent.EXTRA_COMPONENT_NAME, ComponentName(context, PairingAccessibilityService::class.java).flattenToString())
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            val target = if (own.resolveActivity(context.packageManager) != null) own
            else Intent(Settings.ACTION_ACCESSIBILITY_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            context.startActivity(target)
        }
        Function("showPairingCodeBox") { PhoneNotifier.pairingCode(context) }

        View(PhoneLiveView::class) {
            Prop("interactive") { view: PhoneLiveView, interactive: Boolean -> view.interactive = interactive }
            Prop("fps") { view: PhoneLiveView, fps: Int -> view.fps = fps.coerceIn(1, 30) }
            Prop("maxWidth") { view: PhoneLiveView, width: Int -> view.maxWidth = width.coerceIn(120, 1080) }
        }
    }

    /** The Wireless debugging page when the phone has a direct route to it, else Developer options. */
    private fun openWirelessDebugging() {
        val direct = Intent("android.settings.ADB_WIFI_SETTINGS").addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        val target = if (direct.resolveActivity(context.packageManager) != null) direct
        else Intent(Settings.ACTION_APPLICATION_DEVELOPMENT_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        context.startActivity(target)
    }

    /** The accessibility service is in the manifest but off; it only exists for the person while Phone use is on. */
    private fun setAccessibilityComponent(enabled: Boolean) {
        val component = ComponentName(context, PairingAccessibilityService::class.java)
        val state = if (enabled) PackageManager.COMPONENT_ENABLED_STATE_ENABLED else PackageManager.COMPONENT_ENABLED_STATE_DISABLED
        context.packageManager.setComponentEnabledSetting(component, state, PackageManager.DONT_KILL_APP)
    }

    /** The floating window keeps the display's proportions and carries Take over and Stop. */
    private fun pipParams(w: Int, h: Int, auto: Boolean): PictureInPictureParams {
        val ratio = if (w > 0 && h > 0) (w.toDouble() / h).coerceIn(1 / 2.39, 2.39) else 9.0 / 20
        val builder = PictureInPictureParams.Builder().setAspectRatio(Rational((ratio * 1000).toInt(), 1000))
        builder.setActions(
            listOf(
                pipAction("Take over", PhoneNotifier.takeOver(context, 20), android.R.drawable.ic_menu_edit),
                pipAction("Stop", broadcast(PhoneNotifier.ACTION_STOP, 21), android.R.drawable.ic_menu_close_clear_cancel),
            ),
        )
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) builder.setAutoEnterEnabled(auto)
        return builder.build()
    }

    private fun broadcast(action: String, request: Int): PendingIntent {
        val intent = Intent(context, PhoneActionReceiver::class.java).setAction(action)
        return PendingIntent.getBroadcast(context, request, intent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
    }

    private fun pipAction(title: String, pending: PendingIntent, icon: Int) =
        RemoteAction(Icon.createWithResource(context, icon), title, title, pending)

    /** The intent that opened the page says the person tapped Take over; it is applied once. */
    private fun takeOverFrom(intent: Intent) {
        if (!intent.getBooleanExtra(PhoneNotifier.EXTRA_TAKE_OVER, false)) return
        intent.removeExtra(PhoneNotifier.EXTRA_TAKE_OVER)
        PhoneController.takeOver()
    }
}
