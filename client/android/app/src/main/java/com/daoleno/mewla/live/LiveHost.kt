package com.daoleno.mewla.live

import android.app.DatePickerDialog
import android.app.TimePickerDialog
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.Settings
import android.text.format.DateFormat
import android.util.Log
import androidx.activity.ComponentActivity
import androidx.core.app.NotificationManagerCompat
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner
import com.longbridge.nsh.NshHostReply
import com.longbridge.nsh.NshView
import org.json.JSONArray
import org.json.JSONObject
import java.time.Instant
import java.time.ZoneId

/**
 * `live.*`: the live notification and the home-screen widgets of the current computer.
 * `calendar.*`: the phone's own calendar alerts (exact alarms, the lock-screen alert),
 * time zones (QuickJS knows none but UTC) and the system's date and time pickers.
 */
class LiveHost(private val activity: ComponentActivity, nsh: NshView) {
  init {
    // While the app is in front an alert marks itself fired but posts nothing: the in-app card shows it.
    activity.lifecycle.addObserver(object : DefaultLifecycleObserver {
      override fun onResume(owner: LifecycleOwner) { CalendarAlerts.foreground = true }
      override fun onPause(owner: LifecycleOwner) { CalendarAlerts.foreground = false }
    })
    val context = activity.applicationContext

    nsh.handle("live.apply") { data, reply ->
      answer(reply) {
        // The daemon pushes only while the phone is away, so with the socket up the widgets learn here.
        LiveController.apply(context, field(data, "state"), "").also { if (it) WidgetSync.refreshAsync(context) }
      }
    }
    nsh.handle("live.setCurrentServer") { data, reply -> answer(reply) { LiveController.switchServer(context, field(data, "serverId")); null } }
    nsh.handle("live.configureWidget") { data, reply ->
      answer(reply) {
        val prefs = LivePrefs(context)
        prefs.widgetBaseUrl = field(data, "baseUrl").trim()
        prefs.widgetTokenSealed = SecretBox.seal(field(data, "token"))
        prefs.widgetRevoked = false
        WidgetSync.refreshAsync(context)
        null
      }
    }
    // A computer reached through Mewla Relay has no address a widget can fetch: its widgets show the pushed state.
    nsh.handle("live.configureWidgetPushed") { _, reply ->
      answer(reply) {
        val prefs = LivePrefs(context)
        prefs.widgetMode = WIDGET_PUSHED
        prefs.widgetRevoked = false
        WidgetRender.renderAll(context)
        null
      }
    }
    nsh.handle("live.clearWidget") { _, reply -> answer(reply) { LivePrefs(context).clearWidget(); WidgetRender.renderAll(context); null } }
    nsh.handle("live.refreshWidgets") { _, reply -> answer(reply) { WidgetSync.refreshAsync(context); null } }
    nsh.handle("live.widgetState") { _, reply ->
      answer(reply) {
        val prefs = LivePrefs(context)
        when {
          prefs.widgetRevoked -> "revoked"
          prefs.widgetMode == WIDGET_PUSHED -> "ok"
          prefs.widgetTokenSealed.isNotEmpty() && prefs.widgetBaseUrl.isNotEmpty() -> "ok"
          else -> "none"
        }
      }
    }
    nsh.handle("live.supported") { _, reply -> reply.resolve(true) }
    nsh.handle("live.diagnostics") { _, reply ->
      answer(reply) {
        mapOf(
          "sdk" to Build.VERSION.SDK_INT,
          "notificationsEnabled" to NotificationManagerCompat.from(context).areNotificationsEnabled(),
          "promotionAllowed" to LiveNotifier.promotionStatus(context),
        )
      }
    }

    // Replaces the armed alerts with the app's synced set: [{key, itemId, serverId, title, body, atMs}].
    nsh.handle("calendar.alertsSchedule") { data, reply ->
      answer(reply) {
        val list = (data as? Map<*, *>)?.get("alerts") as? List<*> ?: throw IllegalArgumentException("alerts is required")
        CalendarAlerts.schedule(context, list.map { CalendarAlertStore.alertOf(JSONObject(it as Map<*, *>)) })
      }
    }
    nsh.handle("calendar.alertsCancelItem") { data, reply -> answer(reply) { CalendarAlerts.cancelItem(context, field(data, "itemId")); null } }
    nsh.handle("calendar.alertsSetStrong") { data, reply ->
      answer(reply) { CalendarAlertStore(context).strong = (data as? Map<*, *>)?.get("strong") == true; null }
    }
    nsh.handle("calendar.alertsCapabilities") { _, reply -> answer(reply) { CalendarAlerts.capabilities(context) } }
    nsh.handle("calendar.alertsOpenSettings") { _, reply -> answer(reply) { openFullScreenSettings(); null } }
    // Done and Snooze taken on a notification or the lock-screen alert with no app running, then cleared.
    nsh.handle("calendar.alertsTakeActions") { _, reply ->
      answer(reply) {
        val array = JSONArray(CalendarAlertStore(context).takeActions())
        (0 until array.length()).map { i -> array.getJSONObject(i).let { o -> o.keys().asSequence().associateWith { o.get(it) } } }
      }
    }
    nsh.handle("calendar.zones") { data, reply -> answer(reply) { zones(data as? Map<*, *> ?: emptyMap<String, Any>()) } }
    nsh.handle("calendar.pickDate") { data, reply -> pickDate(data as? Map<*, *> ?: emptyMap<String, Any>(), reply) }
    nsh.handle("calendar.pickTime") { data, reply -> pickTime(data as? Map<*, *> ?: emptyMap<String, Any>(), reply) }
  }

  /**
   * The device's zone and clock style, and for each zone asked its offset from `fromMs`
   * with every change until `toMs`: `[{atMs, offsetMinutes, name}]`, `name` ICU's short
   * English name ("PDT", "GMT+8"), as the browser's Intl gives it.
   */
  private fun zones(data: Map<*, *>): Map<String, Any?> {
    val from = (data["fromMs"] as? Number)?.toLong() ?: System.currentTimeMillis()
    val to = (data["toMs"] as? Number)?.toLong() ?: (from + 400L * 86_400_000)
    val names = android.icu.text.SimpleDateFormat("z", android.icu.util.ULocale.US)
    val result = mutableMapOf<String, Any?>()
    for (id in (data["zones"] as? List<*>).orEmpty().filterIsInstance<String>()) {
      val rules = try { ZoneId.of(id).rules } catch (_: Exception) { null }
      if (rules == null) { result[id] = null; continue }
      names.timeZone = android.icu.util.TimeZone.getTimeZone(id)
      val spans = mutableListOf<Map<String, Any>>()
      var at = Instant.ofEpochMilli(from)
      while (true) {
        spans += mapOf("atMs" to at.toEpochMilli(), "offsetMinutes" to rules.getOffset(at).totalSeconds / 60, "name" to names.format(java.util.Date(at.toEpochMilli())))
        val next = rules.nextTransition(at) ?: break
        at = next.instant
        if (at.toEpochMilli() > to || spans.size > 64) break
      }
      result[id] = spans
    }
    return mapOf("device" to ZoneId.systemDefault().id, "hour12" to !DateFormat.is24HourFormat(activity), "zones" to result)
  }

  /** The system's date picker on `{year, month (1-12), day}`; resolves the picked date, or null. */
  private fun pickDate(data: Map<*, *>, reply: NshHostReply) {
    fun int(name: String) = (data[name] as? Number)?.toInt()
    val today = java.time.LocalDate.now()
    var answered = false
    val dialog = DatePickerDialog(activity, { _, year, month, day ->
      answered = true
      reply.resolve(mapOf("year" to year, "month" to month + 1, "day" to day))
    }, int("year") ?: today.year, (int("month") ?: today.monthValue) - 1, int("day") ?: today.dayOfMonth)
    dialog.setOnDismissListener { if (!answered) reply.resolve(null) }
    dialog.show()
  }

  /** The system's time picker on `{hour, minute}`, in the phone's 12/24-hour style; resolves the picked time, or null. */
  private fun pickTime(data: Map<*, *>, reply: NshHostReply) {
    var answered = false
    val dialog = TimePickerDialog(activity, { _, hour, minute ->
      answered = true
      reply.resolve(mapOf("hour" to hour, "minute" to minute))
    }, (data["hour"] as? Number)?.toInt() ?: 9, (data["minute"] as? Number)?.toInt() ?: 0, DateFormat.is24HourFormat(activity))
    dialog.setOnDismissListener { if (!answered) reply.resolve(null) }
    dialog.show()
  }

  private fun openFullScreenSettings() {
    val uri = Uri.fromParts("package", activity.packageName, null)
    val intents = listOfNotNull(
      if (Build.VERSION.SDK_INT >= 34) Intent(Settings.ACTION_MANAGE_APP_USE_FULL_SCREEN_INTENT, uri) else null,
      Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS).putExtra(Settings.EXTRA_APP_PACKAGE, activity.packageName),
    )
    for (intent in intents) {
      try {
        activity.startActivity(intent)
        return
      } catch (_: Exception) {
        continue
      }
    }
  }

  private fun field(data: Any?, name: String): String =
    (data as? Map<*, *>)?.get(name) as? String ?: throw IllegalArgumentException("$name is required")

  private fun answer(reply: NshHostReply, work: () -> Any?) {
    try {
      reply.resolve(work())
    } catch (error: Exception) {
      Log.w("MewlaLive", "host call failed", error)
      reply.reject(error.message ?: error.javaClass.simpleName)
    }
  }
}
