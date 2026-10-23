package expo.modules.mewlalive

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat

/**
 * The one live surface: an ongoing notification. On Android 16 QPR1+ it asks to be promoted
 * (status-bar chip, pinned card); everywhere else it is a plain ongoing notification with the
 * same content.
 */
internal object LiveNotifier {
  private const val TAG = "MewlaLive"
  const val CHANNEL_ID = "mewla-live"
  const val NOTIFICATION_ID = 7101

  fun post(context: Context, state: LiveState) {
    if (!canNotify(context)) return
    ensureChannel(context)
    val notification = build(context, state)
    try {
      NotificationManagerCompat.from(context).notify(NOTIFICATION_ID, notification)
    } catch (e: SecurityException) {
      Log.w(TAG, "notify denied", e)
    }
  }

  fun cancel(context: Context) {
    NotificationManagerCompat.from(context).cancel(NOTIFICATION_ID)
  }

  /** What Android says about promotion for this app right now; null when the platform cannot tell. */
  fun promotionStatus(context: Context): Boolean? {
    if (Build.VERSION.SDK_INT < 36) return null
    return try {
      val manager = context.getSystemService(NotificationManager::class.java)
      NotificationManager::class.java.getMethod("canPostPromotedNotifications").invoke(manager) as? Boolean
    } catch (_: Throwable) {
      null
    }
  }

  internal fun build(context: Context, state: LiveState): android.app.Notification {
    val needs = state.needsYou
    val title = if (needs) "Brain needs you" else state.title.ifBlank { "Mewla" }
    val text = if (needs) state.question?.text.orEmpty() else state.phase

    val builder = NotificationCompat.Builder(context, CHANNEL_ID)
      .setSmallIcon(smallIcon(context))
      .setContentTitle(title)
      .setContentText(text.ifBlank { null })
      .setOngoing(true)
      .setOnlyAlertOnce(true)
      .setSilent(true)
      .setCategory(if (needs) NotificationCompat.CATEGORY_STATUS else NotificationCompat.CATEGORY_PROGRESS)
      .setVisibility(NotificationCompat.VISIBILITY_PRIVATE)
      .setContentIntent(openIntent(context, state))
      .setDeleteIntent(dismissIntent(context, state))
      .setRequestPromotedOngoing(true)

    if (needs) {
      // The count is the chip text; chip colour belongs to the system.
      builder.setShortCriticalText(state.needsCount.coerceAtLeast(1).toString())
      if (text.isNotBlank()) builder.setStyle(NotificationCompat.BigTextStyle().bigText(text))
      val question = state.question
      if (question != null) {
        LiveRules.actionChoices(state).forEachIndexed { index, choice ->
          builder.addAction(0, choice, answerIntent(context, question.id, index))
        }
      }
    } else {
      // Elapsed time is the system's chronometer: no update per second, only per phase change.
      if (state.startedAt > 0) {
        builder.setShowWhen(true).setWhen(state.startedAt).setUsesChronometer(true)
      }
      if (state.total > 0) {
        val style = NotificationCompat.ProgressStyle()
        val segments = ArrayList<NotificationCompat.ProgressStyle.Segment>()
        repeat(state.total.coerceAtMost(12)) { segments.add(NotificationCompat.ProgressStyle.Segment(1)) }
        style.setProgressSegments(segments).setProgress(state.done.coerceIn(0, segments.size))
        builder.setStyle(style)
      }
    }
    return builder.build()
  }

  private fun canNotify(context: Context): Boolean {
    if (Build.VERSION.SDK_INT >= 33 &&
      context.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
    ) return false
    return NotificationManagerCompat.from(context).areNotificationsEnabled()
  }

  private fun ensureChannel(context: Context) {
    val manager = context.getSystemService(NotificationManager::class.java)
    if (manager.getNotificationChannel(CHANNEL_ID) != null) return
    // DEFAULT, silent: a live surface is a glance, never a sound. (IMPORTANCE_MIN would block promotion.)
    val channel = NotificationChannel(CHANNEL_ID, "Live activity", NotificationManager.IMPORTANCE_DEFAULT).apply {
      setSound(null, null)
      enableVibration(false)
      setShowBadge(false)
    }
    manager.createNotificationChannel(channel)
  }

  private fun smallIcon(context: Context): Int {
    val id = context.resources.getIdentifier("notification_icon", "drawable", context.packageName)
    return if (id != 0) id else context.applicationInfo.icon
  }

  private fun deepLink(context: Context, path: String, query: Map<String, String>, requestCode: Int): PendingIntent {
    val uri = Uri.Builder().scheme("mewla").authority("live").appendPath(path).apply {
      query.forEach { (k, v) -> appendQueryParameter(k, v) }
    }.build()
    val intent = Intent(Intent.ACTION_VIEW, uri).setPackage(context.packageName)
      .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP)
    return PendingIntent.getActivity(
      context, requestCode, intent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
    )
  }

  fun openIntent(context: Context, state: LiveState): PendingIntent =
    deepLink(context, "open", mapOf("k" to state.kind), 1)

  /** A tap opens the app on that question; the app then sends the answer once. */
  fun answerIntent(context: Context, questionId: String, choiceIndex: Int): PendingIntent =
    deepLink(context, "answer", mapOf("q" to questionId, "c" to choiceIndex.toString()), 10 + choiceIndex)

  private fun dismissIntent(context: Context, state: LiveState): PendingIntent {
    val intent = Intent(context, LiveDismissReceiver::class.java).putExtra("run_key", state.runKey)
    return PendingIntent.getBroadcast(
      context, 2, intent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
    )
  }
}
