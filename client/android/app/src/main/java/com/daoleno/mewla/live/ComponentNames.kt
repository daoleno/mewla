// The components the system knows by class name, under the names the Expo app
// gave them (its manifest, v0.3.0). A home-screen widget is bound to its
// provider's class: renamed, it disappears from the home screen after the
// in-place upgrade. Armed alarms and posted notifications hold explicit intents
// to these receivers too. So the names stay; the work is in com.daoleno.mewla.live.
package expo.modules.mewlalive

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import com.daoleno.mewla.live.AlertActivity
import com.daoleno.mewla.live.BaseWidgetProvider
import com.daoleno.mewla.live.CalendarAlertStore
import com.daoleno.mewla.live.CalendarAlerts
import com.daoleno.mewla.live.LivePrefs

class SmallWidgetProvider : BaseWidgetProvider()

class MediumWidgetProvider : BaseWidgetProvider()

/** Over the lock screen on its own: the app's main activity never is. */
class CalendarAlertActivity : AlertActivity()

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

/** The user swiped the live notification away: keep it away until a different run or question. */
class LiveDismissReceiver : BroadcastReceiver() {
  override fun onReceive(context: Context, intent: Intent) {
    val key = intent.getStringExtra("run_key").orEmpty()
    if (key.isNotEmpty()) LivePrefs(context).dismiss(key)
  }
}
