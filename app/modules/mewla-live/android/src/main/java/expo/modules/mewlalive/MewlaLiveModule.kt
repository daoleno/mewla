package expo.modules.mewlalive

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.provider.Settings
import org.json.JSONArray
import android.os.Build
import androidx.core.app.NotificationManagerCompat
import expo.modules.kotlin.exception.CodedException
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition

class MewlaLiveModule : Module() {
  private val context: Context
    get() = appContext.reactContext ?: throw CodedException("ERR_NO_CONTEXT", "React context is not available", null)

  override fun definition() = ModuleDefinition {
    Name("MewlaLive")

    /** A live_state the app received on its socket. Same rules as a push: ordered, deduped, once. */
    Function("apply") { raw: String ->
      val applied = LiveController.apply(context, raw, "")
      // The daemon pushes only while the phone is away, so with the socket up the widgets learn here.
      if (applied) WidgetSync.refreshAsync(context)
      applied
    }

    /** The server whose live state the phone shows; changing it clears the old server's surface and widgets. */
    Function("setCurrentServer") { serverRef: String ->
      LiveController.switchServer(context, serverRef)
    }

    Function("configureWidget") { baseUrl: String, token: String ->
      val prefs = LivePrefs(context)
      prefs.widgetBaseUrl = baseUrl.trim()
      prefs.widgetTokenSealed = SecretBox.seal(token)
      prefs.widgetRevoked = false
      WidgetSync.refreshAsync(context)
    }

    /** A server reached through Mewla Link has no address a widget can fetch: its widgets show the pushed state. */
    Function("configureWidgetPushed") {
      val prefs = LivePrefs(context)
      prefs.widgetMode = WIDGET_PUSHED
      prefs.widgetRevoked = false
      WidgetRender.renderAll(context)
    }

    Function("clearWidget") {
      LivePrefs(context).clearWidget()
      WidgetRender.renderAll(context)
    }

    Function("refreshWidgets") { WidgetSync.refreshAsync(context) }

    /**
     * Replaces the armed calendar alarms with the app's synced set: `[{key, itemId, serverId, title,
     * body, atMs}]`. Alerts that already fired stay fired.
     */
    Function("calendarAlertsSchedule") { json: String ->
      val array = JSONArray(json)
      CalendarAlerts.schedule(
        context,
        (0 until array.length()).map { CalendarAlertStore.alertOf(array.getJSONObject(it)) },
      )
    }

    Function("calendarAlertsCancelItem") { itemId: String -> CalendarAlerts.cancelItem(context, itemId) }

    /** The person's own "Strong calendar alerts" setting; it decides the channel and the lock-screen takeover. */
    Function("calendarAlertsSetStrong") { strong: Boolean -> CalendarAlertStore(context).strong = strong }

    /** With the app open an alert marks itself fired but posts nothing: the in-app card shows it. */
    Function("calendarAlertsSetForeground") { foreground: Boolean -> CalendarAlerts.foreground = foreground }

    Function("calendarAlertsCapabilities") { CalendarAlerts.capabilities(context) }

    Function("calendarAlertsOpenFullScreenSettings") {
      val uri = Uri.fromParts("package", context.packageName, null)
      val intents = listOfNotNull(
        if (Build.VERSION.SDK_INT >= 34) Intent(Settings.ACTION_MANAGE_APP_USE_FULL_SCREEN_INTENT, uri) else null,
        Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS).putExtra(Settings.EXTRA_APP_PACKAGE, context.packageName),
      )
      for (intent in intents) {
        try {
          context.startActivity(intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
          break
        } catch (_: Exception) {
          continue
        }
      }
    }

    /** What the person did to an alert with no app running: `[{type, itemId, serverId, minutes, atMs}]`, then cleared. */
    Function("calendarAlertsTakeActions") { CalendarAlertStore(context).takeActions() }

    Function("isSupported") { true }

    Function("widgetState") {
      val prefs = LivePrefs(context)
      when {
        prefs.widgetRevoked -> "revoked"
        prefs.widgetMode == WIDGET_PUSHED -> "ok"
        prefs.widgetTokenSealed.isNotEmpty() && prefs.widgetBaseUrl.isNotEmpty() -> "ok"
        else -> "none"
      }
    }

    Function("diagnostics") {
      mapOf(
        "sdk" to Build.VERSION.SDK_INT,
        "notificationsEnabled" to NotificationManagerCompat.from(context).areNotificationsEnabled(),
        "promotionAllowed" to LiveNotifier.promotionStatus(context),
      )
    }
  }
}
