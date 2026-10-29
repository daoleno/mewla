package expo.modules.mewlalive

import android.app.AlarmManager
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.media.AudioAttributes
import android.media.RingtoneManager
import android.net.Uri
import android.os.Build
import android.util.Log
import androidx.core.app.NotificationCompat

/**
 * Calendar alerts on the phone: a local alarm per upcoming alert, and the daemon's push as the backup.
 * Whichever arrives first posts the notification; the other finds its key fired and drops. The alarm
 * receiver and the FCM service share [LOCK], so they never both post.
 */
internal object CalendarAlerts {
  private const val TAG = "MewlaLive"
  val LOCK = Any()

  /** Set by the app from its AppState: with the app open the in-app card shows instead of a notification. */
  @Volatile
  var foreground = false

  const val CHANNEL_ORDINARY = "mewla-calendar"
  const val CHANNEL_ALARM = "mewla-calendar-alarm"
  const val ACTION_ALARM = "expo.modules.mewlalive.CALENDAR_ALARM"
  const val ACTION_ANSWER = "expo.modules.mewlalive.CALENDAR_ANSWER"
  const val EXTRA_TYPE = "type"
  const val EXTRA_MINUTES = "minutes"

  /** Replaces the armed set with what the app synced; keys that already fired are never armed again. */
  fun schedule(context: Context, synced: List<CalendarAlert>): Int = synchronized(LOCK) {
    val store = CalendarAlertStore(context)
    val now = System.currentTimeMillis()
    val fired = store.fired(now)
    val before = store.armed()
    val after = CalendarAlertRules.mergeArmed(
      before, synced.filterNot { CalendarAlertRules.alreadyFired(fired, it.key) }, now,
    )
    before.filter { old -> after.none { it.key == old.key } }.forEach { cancelAlarm(context, it.key) }
    after.forEach { arm(context, it) }
    store.setArmed(after)
    after.size
  }

  /** Arms one alert and remembers it, for a snooze pressed with no app running. */
  fun armOne(context: Context, alert: CalendarAlert) = synchronized(LOCK) {
    val store = CalendarAlertStore(context)
    store.setArmed(store.armed().filter { it.key != alert.key } + alert)
    arm(context, alert)
  }

  fun cancelItem(context: Context, itemId: String) = synchronized(LOCK) {
    val store = CalendarAlertStore(context)
    val armed = store.armed()
    armed.filter { it.itemId == itemId }.forEach { cancelAlarm(context, it.key) }
    store.setArmed(armed.filter { it.itemId != itemId })
    dismissPosted(context, itemId)
  }

  /** Re-arms what survived a reboot or an update; an alarm missed within the window fires now. */
  fun rearm(context: Context) {
    val store = CalendarAlertStore(context)
    val now = System.currentTimeMillis()
    val late = mutableListOf<CalendarAlert>()
    synchronized(LOCK) {
      val kept = store.armed().filter { alert ->
        when (CalendarAlertRules.onBoot(alert.atMs, now)) {
          BootDecision.Arm -> { arm(context, alert); true }
          BootDecision.FireNow -> { late += alert; true }
          BootDecision.Drop -> false
        }
      }
      store.setArmed(kept)
    }
    late.forEach { fire(context, it) }
  }

  /**
   * Fires an alert once. Returns false when it already fired (an alarm and a push for the same alert,
   * or a snooze both here and from the daemon).
   */
  fun fire(context: Context, alert: CalendarAlert, source: String = "alarm"): Boolean {
    synchronized(LOCK) {
      val store = CalendarAlertStore(context)
      val now = System.currentTimeMillis()
      if (CalendarAlertRules.alreadyFired(store.fired(now), alert.key)) {
        Log.d(TAG, "calendar alert $source key=${alert.key} dropped: already fired")
        return false
      }
      store.markFired(alert.key, now)
      store.setArmed(store.armed().filter { it.key != alert.key })
      cancelAlarm(context, alert.key)
    }
    Log.d(TAG, "calendar alert $source key=${alert.key} fired foreground=$foreground")
    if (!foreground) post(context, alert)
    return true
  }

  /** What Done and Snooze do, from the notification or the full-screen alert, with no app running. */
  fun answer(context: Context, type: String, minutes: Int, alert: CalendarAlert) {
    val now = System.currentTimeMillis()
    manager(context).cancel(alert.key, CalendarAlertRules.notificationId(alert.key))
    CalendarAlertStore(context).pushAction(type, alert, minutes, now)
    if (type != "snooze") return
    cancelItem(context, alert.itemId)
    val at = CalendarAlertRules.snoozeAt(now, minutes)
    armOne(context, alert.copy(key = CalendarAlertRules.key(alert.itemId, at), atMs = at, native = true))
  }

  fun capabilities(context: Context): Map<String, Boolean> = mapOf(
    "exactAlarms" to canExact(context),
    "fullScreen" to canFullScreen(context),
    "notifications" to androidx.core.app.NotificationManagerCompat.from(context).areNotificationsEnabled(),
  )

  fun canFullScreen(context: Context): Boolean =
    Build.VERSION.SDK_INT < 34 || manager(context).canUseFullScreenIntent()

  fun deepLink(alert: CalendarAlert): Uri =
    Uri.Builder().scheme("mewla").authority("calendar")
      .appendQueryParameter("id", alert.itemId).appendQueryParameter("serverId", alert.serverId).build()

  fun openIntent(context: Context, alert: CalendarAlert): Intent =
    Intent(Intent.ACTION_VIEW, deepLink(alert)).setPackage(context.packageName)
      .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP)

  fun alertExtras(intent: Intent, alert: CalendarAlert): Intent = intent.putExtra("key", alert.key)
    .putExtra("itemId", alert.itemId).putExtra("serverId", alert.serverId)
    .putExtra("title", alert.title).putExtra("body", alert.body).putExtra("atMs", alert.atMs)

  fun alertOf(intent: Intent): CalendarAlert? {
    val key = intent.getStringExtra("key") ?: return null
    return CalendarAlert(
      key = key,
      itemId = intent.getStringExtra("itemId") ?: CalendarAlertRules.itemOf(key),
      serverId = intent.getStringExtra("serverId").orEmpty(),
      title = intent.getStringExtra("title").orEmpty(),
      body = intent.getStringExtra("body").orEmpty(),
      atMs = intent.getLongExtra("atMs", CalendarAlertRules.atOf(key) ?: 0L),
    )
  }

  private fun manager(context: Context) = context.getSystemService(NotificationManager::class.java)

  private fun canExact(context: Context): Boolean =
    Build.VERSION.SDK_INT < 31 || context.getSystemService(AlarmManager::class.java).canScheduleExactAlarms()

  private fun alarmIntent(context: Context, key: String): PendingIntent {
    val intent = Intent(context, CalendarAlarmReceiver::class.java).setAction(ACTION_ALARM)
      .setData(Uri.parse("mewla-alarm://alert/${Uri.encode(key)}")).putExtra("key", key)
    return PendingIntent.getBroadcast(
      context, CalendarAlertRules.notificationId(key), intent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
    )
  }

  private fun arm(context: Context, alert: CalendarAlert) {
    val alarms = context.getSystemService(AlarmManager::class.java)
    val pending = alarmIntent(context, alert.key)
    try {
      // An exact alarm when the system allows it; otherwise the system may batch it a little, still
      // through Doze. Never setAlarmClock: that would show every reminder as the next alarm.
      if (canExact(context)) alarms.setExactAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, alert.atMs, pending)
      else alarms.setAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, alert.atMs, pending)
    } catch (_: SecurityException) {
      alarms.setAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, alert.atMs, pending)
    }
  }

  private fun cancelAlarm(context: Context, key: String) {
    val pending = alarmIntent(context, key)
    context.getSystemService(AlarmManager::class.java).cancel(pending)
    pending.cancel()
  }

  private fun dismissPosted(context: Context, itemId: String) {
    val manager = manager(context)
    manager.activeNotifications.filter { it.tag?.startsWith("$itemId@") == true }
      .forEach { manager.cancel(it.tag, it.id) }
  }

  private fun ensureChannels(context: Context) {
    val manager = manager(context)
    // Created once and never raised: a channel's importance belongs to the user after that.
    if (manager.getNotificationChannel(CHANNEL_ORDINARY) == null) {
      manager.createNotificationChannel(
        NotificationChannel(CHANNEL_ORDINARY, "Calendar reminders", NotificationManager.IMPORTANCE_HIGH),
      )
    }
    if (manager.getNotificationChannel(CHANNEL_ALARM) == null) {
      manager.createNotificationChannel(
        NotificationChannel(CHANNEL_ALARM, "Calendar alerts", NotificationManager.IMPORTANCE_HIGH).apply {
          setSound(
            RingtoneManager.getDefaultUri(RingtoneManager.TYPE_NOTIFICATION),
            AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_NOTIFICATION).build(),
          )
          enableVibration(true)
          vibrationPattern = longArrayOf(0, 300, 200, 300)
          lockscreenVisibility = android.app.Notification.VISIBILITY_PUBLIC
        },
      )
    }
  }

  private fun smallIcon(context: Context): Int {
    val id = context.resources.getIdentifier("notification_icon", "drawable", context.packageName)
    return if (id != 0) id else context.applicationInfo.icon
  }

  private fun answerIntent(context: Context, alert: CalendarAlert, type: String, minutes: Int): PendingIntent {
    val intent = alertExtras(Intent(context, CalendarActionReceiver::class.java).setAction(ACTION_ANSWER), alert)
      .setData(Uri.parse("mewla-alarm://answer/${Uri.encode(alert.key)}/$type/$minutes"))
      .putExtra(EXTRA_TYPE, type).putExtra(EXTRA_MINUTES, minutes)
    return PendingIntent.getBroadcast(
      context, "${alert.key}/$type/$minutes".hashCode(), intent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
    )
  }

  private fun post(context: Context, alert: CalendarAlert) {
    val strong = CalendarAlertStore(context).strong
    ensureChannels(context)
    val id = CalendarAlertRules.notificationId(alert.key)
    val builder = NotificationCompat.Builder(context, if (strong) CHANNEL_ALARM else CHANNEL_ORDINARY)
      .setSmallIcon(smallIcon(context))
      .setContentTitle(alert.title)
      .setContentText(alert.body)
      .setStyle(NotificationCompat.BigTextStyle().bigText(alert.body))
      .setWhen(alert.atMs)
      .setShowWhen(true)
      .setCategory(if (strong) NotificationCompat.CATEGORY_ALARM else NotificationCompat.CATEGORY_EVENT)
      .setPriority(NotificationCompat.PRIORITY_HIGH)
      .setVisibility(NotificationCompat.VISIBILITY_PUBLIC)
      .setOngoing(false)
      .setAutoCancel(true)
      .setContentIntent(
        PendingIntent.getActivity(context, id, openIntent(context, alert), PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE),
      )
      .addAction(0, "Done", answerIntent(context, alert, "done", 0))
      .addAction(0, "Snooze 5 min", answerIntent(context, alert, "snooze", 5))
      .addAction(0, "Snooze 15 min", answerIntent(context, alert, "snooze", 15))
    if (strong && canFullScreen(context)) {
      val screen = alertExtras(Intent(context, CalendarAlertActivity::class.java), alert)
        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
      builder.setFullScreenIntent(
        PendingIntent.getActivity(context, id, screen, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE), true,
      )
    }
    try {
      manager(context).notify(alert.key, id, builder.build())
    } catch (_: SecurityException) {
      // Notification permission was revoked; the in-app card still shows the alert.
    }
  }
}
