package com.daoleno.mewla.phone

import android.app.Activity
import android.app.PendingIntent
import android.app.PictureInPictureParams
import android.app.RemoteAction
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.drawable.Icon
import android.net.Uri
import android.os.Build
import android.provider.Settings
import android.util.Rational
import com.daoleno.mewla.phone.link.LinkTarget
import expo.modules.mewlaphone.PairingAccessibilityService
import expo.modules.mewlaphone.SettingsReading
import org.json.JSONObject

/**
 * What the app asks of Phone use, the person's controls and the computer's requests aside: the
 * switch, the link's computer, the guided setup, the system screens setup opens and the floating
 * window. The work is in [PhoneController]; the app's adapter (src/github/.../phone) puts this
 * behind `phone.*`. Calls that block (pairing, the helper) must not run on the main thread.
 */
object PhoneUse {
    fun init(context: Context) = PhoneController.init(context)

    /** The live checks and state (see [PhoneController.status]). */
    fun status(): JSONObject = PhoneController.status()

    /** Called with the status whenever it changes, from any thread; returns how to stop. */
    fun watch(listener: (JSONObject) -> Unit): () -> Unit {
        PhoneController.statusListeners.add(listener)
        return { PhoneController.statusListeners.remove(listener) }
    }

    /** Phone use on or off: the foreground service, the helper, the link and the pairing reader follow. */
    fun setEnabled(context: Context, enabled: Boolean) {
        setAccessibilityComponent(context, enabled)
        PhoneController.setEnabled(enabled)
        // Once the secure-settings grant exists, nothing is asked of the person again.
        if (enabled) runCatching { PhoneController.enableAccessibility() }
    }

    /** The one current computer the link follows; null when there is none. */
    fun follow(target: LinkTarget?) = PhoneController.follow(target)

    /** The app is in front: a link waiting out its backoff tries now. */
    fun foreground() = PhoneController.retryLinkNow()

    /** Pairs with this phone's own Wireless debugging; the code is read off the screen when null. Blocks for minutes. */
    fun pair(context: Context, code: String?): Boolean {
        // Without the accessibility reading the person types the digits into a notification.
        if (code == null && !SettingsReading.serviceRunning) PhoneNotifier.pairingCode(context)
        val ok = PhoneController.pair(code)
        PhoneNotifier.clearPairingCode(context)
        if (ok) runCatching { PhoneController.ensureHelper() }
        PhoneController.publish()
        return ok
    }

    /** Starts the helper because the person asked; false when it does not come up (the status says why). Blocks. */
    fun ensureHelper(): Boolean = try {
        PhoneController.startedByPerson()
        PhoneController.ensureHelper()
        true
    } catch (e: Exception) {
        false
    } finally {
        PhoneController.publish()
    }

    /** Switches the accessibility service on by itself when it may; false when the person must do it in Settings. */
    fun enableAccessibility(): Boolean = PhoneController.enableAccessibility()

    fun takeOver() = PhoneController.takeOver()
    fun giveBack() = PhoneController.giveBack()
    fun stop() = PhoneController.stop()
    fun handoffDone() = PhoneController.handoffDone()

    fun showPairingCodeBox(context: Context) = PhoneNotifier.pairingCode(context)

    /** The Wireless debugging page when the phone has a direct route to it, else Developer options. */
    fun openWirelessDebugging(context: Context) {
        val direct = Intent("android.settings.ADB_WIFI_SETTINGS").addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        val target = if (direct.resolveActivity(context.packageManager) != null) direct
        else Intent(Settings.ACTION_APPLICATION_DEVELOPMENT_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        context.startActivity(target)
    }

    /** About phone, where Build number is. */
    fun openDeveloperSettings(context: Context) {
        context.startActivity(Intent(Settings.ACTION_DEVICE_INFO_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    }

    /** App info, where "Allow restricted settings" is. */
    fun openAppSettings(context: Context) {
        context.startActivity(
            Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:" + context.packageName))
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
        )
    }

    fun openAccessibilitySettings(context: Context) {
        // The page for Mewla's own entry resolves on some phones yet is not exported for apps to start
        // (a SecurityException): the list, with Mewla's entry highlighted, is the way that always works.
        val component = ComponentName(context, PairingAccessibilityService::class.java).flattenToString()
        val own = Intent("android.settings.ACCESSIBILITY_DETAILS_SETTINGS")
            .putExtra(Intent.EXTRA_COMPONENT_NAME, component)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        val opened = own.resolveActivity(context.packageManager) != null && runCatching { context.startActivity(own) }.isSuccess
        if (!opened) {
            context.startActivity(
                Intent(Settings.ACTION_ACCESSIBILITY_SETTINGS)
                    .putExtra(":settings:fragment_args_key", component)
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
            )
        }
    }

    // ---- the floating window -----------------------------------------------------------------

    /** Leaving the app floats the screen by itself while an agent works: armed by the app, with the display's shape. */
    @Volatile private var pipAuto: IntArray? = null

    /** Floats the agent's screen in a small window over other apps; false when the phone cannot. */
    fun enterPip(activity: Activity, w: Int, h: Int): Boolean =
        runCatching { activity.enterPictureInPictureMode(pipParams(activity, w, h, false)) }.getOrDefault(false)

    /** While an agent works, leaving the app floats the screen by itself (Android 12+ does it; older ones on the leave hint). */
    fun setPipAuto(activity: Activity, on: Boolean, w: Int, h: Int) {
        pipAuto = if (on) intArrayOf(w, h) else null
        runCatching { activity.setPictureInPictureParams(pipParams(activity, w, h, on)) }
    }

    /** The person is leaving the app (Home, Recents): before Android 12 the auto float is started here. */
    fun leaving(activity: Activity) {
        val shape = pipAuto ?: return
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.S) enterPip(activity, shape[0], shape[1])
    }

    /** The window started or stopped floating: only the agent's screen is shown in it. */
    fun pipChanged(activity: Activity, floating: Boolean) = PipOverlay.sync(activity, floating)

    /** The intent that opened the app says the person tapped Take over; it is applied once. */
    fun takeOverFrom(intent: Intent?) {
        if (intent?.getBooleanExtra(PhoneNotifier.EXTRA_TAKE_OVER, false) != true) return
        intent.removeExtra(PhoneNotifier.EXTRA_TAKE_OVER)
        PhoneController.takeOver()
    }

    /** The floating window keeps the display's proportions and carries Take over and Stop. */
    private fun pipParams(context: Context, w: Int, h: Int, auto: Boolean): PictureInPictureParams {
        val ratio = if (w > 0 && h > 0) (w.toDouble() / h).coerceIn(1 / 2.39, 2.39) else 9.0 / 20
        val builder = PictureInPictureParams.Builder().setAspectRatio(Rational((ratio * 1000).toInt(), 1000))
        builder.setActions(
            listOf(
                pipAction(context, "Take over", PhoneNotifier.takeOver(context, 20), android.R.drawable.ic_menu_edit),
                pipAction(context, "Stop", PhoneNotifier.action(context, PhoneNotifier.ACTION_STOP, 21), android.R.drawable.ic_menu_close_clear_cancel),
            ),
        )
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) builder.setAutoEnterEnabled(auto)
        return builder.build()
    }

    private fun pipAction(context: Context, title: String, pending: PendingIntent, icon: Int) =
        RemoteAction(Icon.createWithResource(context, icon), title, title, pending)

    /** The accessibility service is in the manifest but off; it only exists for the person while Phone use is on. */
    private fun setAccessibilityComponent(context: Context, enabled: Boolean) {
        val component = ComponentName(context, PairingAccessibilityService::class.java)
        val state = if (enabled) PackageManager.COMPONENT_ENABLED_STATE_ENABLED else PackageManager.COMPONENT_ENABLED_STATE_DISABLED
        context.packageManager.setComponentEnabledSetting(component, state, PackageManager.DONT_KILL_APP)
    }
}
