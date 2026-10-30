package expo.modules.mewlaphone

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
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

        /** The live checks and state, as JSON (see PhoneController.status). */
        Function("status") { PhoneController.status().toString() }

        /** Phone use on or off: the foreground service, the helper and the pairing reader follow. */
        Function("setEnabled") { enabled: Boolean ->
            setAccessibilityComponent(enabled)
            PhoneController.setEnabled(enabled)
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
            context.startActivity(Intent(Settings.ACTION_ACCESSIBILITY_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        }
        Function("showPairingCodeBox") { PhoneNotifier.pairingCode(context) }

        View(PhoneLiveView::class) {
            Prop("interactive") { view: PhoneLiveView, interactive: Boolean -> view.interactive = interactive }
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
}
