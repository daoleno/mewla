package expo.modules.mewlalive

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/** The local alarm of one alert came due. Looks the alert up by key: a cancelled alarm finds nothing. */
class CalendarAlarmReceiver : BroadcastReceiver() {
  override fun onReceive(context: Context, intent: Intent) {
    val key = intent.getStringExtra("key") ?: return
    val alert = CalendarAlertStore(context).armed().firstOrNull { it.key == key } ?: return
    CalendarAlerts.fire(context, alert)
  }
}

/** Reboot, update, clock or time zone change: the system dropped the alarms, so arm them again. */
class CalendarBootReceiver : BroadcastReceiver() {
  override fun onReceive(context: Context, intent: Intent) {
    CalendarAlerts.rearm(context)
  }
}

/** Done or Snooze pressed on the notification itself. */
class CalendarActionReceiver : BroadcastReceiver() {
  override fun onReceive(context: Context, intent: Intent) {
    val alert = CalendarAlerts.alertOf(intent) ?: return
    CalendarAlerts.answer(
      context,
      intent.getStringExtra(CalendarAlerts.EXTRA_TYPE).orEmpty(),
      intent.getIntExtra(CalendarAlerts.EXTRA_MINUTES, 0),
      alert,
    )
  }
}
