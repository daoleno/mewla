package com.daoleno.mewla.live

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
 * The live update: an ongoing notification while something runs or needs you. On Android 16 QPR1+ it
 * asks to be promoted (status-bar chip, ColorOS's 流体云 capsule, the pinned card); everywhere else it is
 * a plain ongoing notification with the same content. Promotion allows no custom views and no colour of
 * our own, so the logo and its ring are the large icon's bitmap.
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

  /**
   * Option A: the one thing that matters now. What needs you (newest) else what started last: its title,
   * what it does or asks, its agent's logo with its progress ring as the large icon, the header's words.
   * The status-bar chip (流体云 on ColorOS) is the system's: our mask and either the running item's clock
   * or the request's tag ("Answer", "Approve"). Choices answer only a Work's question; a tap opens the item.
   */
  internal fun build(context: Context, state: LiveState): android.app.Notification {
    val focus = LiveView.focus(state)
    val needs = focus?.asks == true
    val title = focus?.title?.ifBlank { null } ?: "Mewla"
    val text = focus?.line.orEmpty()

    val builder = NotificationCompat.Builder(context, CHANNEL_ID)
      .setSmallIcon(smallIcon(context))
      .setContentTitle(title)
      .setContentText(text.ifBlank { null })
      .setSubText(LiveView.headerWords(state))
      .setOngoing(true)
      .setOnlyAlertOnce(true)
      .setSilent(true)
      .setCategory(if (needs) NotificationCompat.CATEGORY_STATUS else NotificationCompat.CATEGORY_PROGRESS)
      .setVisibility(NotificationCompat.VISIBILITY_PRIVATE)
      .setContentIntent(openIntent(context, focus))
      .setDeleteIntent(dismissIntent(context, state))
      .setRequestPromotedOngoing(true)
    if (focus != null) builder.setLargeIcon(LiveArt.logo(context, focus, 48f))

    if (needs && focus != null) {
      builder.setShortCriticalText(LiveView.tag(focus))
      builder.setShowWhen(false)
      if (text.isNotBlank()) builder.setStyle(NotificationCompat.BigTextStyle().bigText(text))
      val question = state.question
      if (question != null) {
        LiveView.choices(state, focus).forEachIndexed { index, choice ->
          builder.addAction(0, choice, answerIntent(context, question.id, index))
        }
      }
    } else if (focus != null && focus.startedAt > 0) {
      // The system ticks the clock: one post per change, never one per second.
      builder.setShowWhen(true).setWhen(focus.startedAt).setUsesChronometer(true)
    } else {
      builder.setShowWhen(false)
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

  /** A tap opens the item: its Session or Work detail, Brain's thread for Brain's own question. */
  fun openIntent(context: Context, item: LiveItem?): PendingIntent =
    link(context, LiveView.link(item, LivePrefs(context).currentServerRef), 1)

  fun link(context: Context, url: String, requestCode: Int): PendingIntent {
    val intent = Intent(Intent.ACTION_VIEW, Uri.parse(url)).setPackage(context.packageName)
      .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP)
    return PendingIntent.getActivity(context, requestCode, intent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
  }

  /** A tap opens the app on that question; the app then sends the answer once. */
  fun answerIntent(context: Context, questionId: String, choiceIndex: Int): PendingIntent =
    deepLink(context, "answer", mapOf("q" to questionId, "c" to choiceIndex.toString()), 10 + choiceIndex)

  private fun dismissIntent(context: Context, state: LiveState): PendingIntent {
    val intent = Intent(context, expo.modules.mewlalive.LiveDismissReceiver::class.java).putExtra("run_key", state.runKey)
    return PendingIntent.getBroadcast(
      context, 2, intent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
    )
  }
}
