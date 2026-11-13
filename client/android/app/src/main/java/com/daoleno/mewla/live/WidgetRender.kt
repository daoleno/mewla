package com.daoleno.mewla.live

import com.daoleno.mewla.R

import android.appwidget.AppWidgetManager
import android.content.ComponentName
import android.content.Context
import android.os.SystemClock
import android.view.View
import android.widget.RemoteViews

/**
 * Small (2x2) and medium (4x2) home-screen widgets, drawn from the cached live state (option A): the
 * small one is the one thing that matters now, the medium one the tray. Each row opens its Session or
 * Work. With nothing to show (all quiet, not set up, refused, or nothing heard for 12 hours) the cat naps.
 */
internal object WidgetRender {
  private val ROWS = intArrayOf(R.id.row0, R.id.row1, R.id.row2)
  private val ROW_LOGO = intArrayOf(R.id.row0_logo, R.id.row1_logo, R.id.row2_logo)
  private val ROW_TITLE = intArrayOf(R.id.row0_title, R.id.row1_title, R.id.row2_title)
  private val ROW_LINE = intArrayOf(R.id.row0_line, R.id.row1_line, R.id.row2_line)
  private val ROW_LIVE = intArrayOf(R.id.row0_live, R.id.row1_live, R.id.row2_live)
  private val ROW_TIME = intArrayOf(R.id.row0_time, R.id.row1_time, R.id.row2_time)
  private val ROW_TAG = intArrayOf(R.id.row0_tag, R.id.row1_tag, R.id.row2_tag)

  fun renderAll(context: Context) {
    val manager = AppWidgetManager.getInstance(context)
    for (cls in listOf(expo.modules.mewlalive.SmallWidgetProvider::class.java, expo.modules.mewlalive.MediumWidgetProvider::class.java)) {
      val ids = manager.getAppWidgetIds(ComponentName(context, cls))
      if (ids.isEmpty()) continue
      val views = views(context, cls == expo.modules.mewlalive.MediumWidgetProvider::class.java)
      for (id in ids) manager.updateAppWidget(id, views)
    }
  }

  private fun views(context: Context, medium: Boolean): RemoteViews {
    val prefs = LivePrefs(context)
    val fresh = LiveRules.widgetFresh(prefs.widgetFetchedAt, System.currentTimeMillis())
    val state = if (prefs.widgetRevoked || !fresh) null else LiveState.parse(prefs.widgetCache)
    return views(context, medium, state, prefs.currentServerRef)
  }

  /** The widget for [state]; null is "knows nothing" (not set up, refused, stale). */
  fun views(context: Context, medium: Boolean, state: LiveState?, server: String): RemoteViews {
    val rv = RemoteViews(context.packageName, if (medium) R.layout.mewla_widget_medium else R.layout.mewla_widget_small)
    val items = state?.let { LiveView.items(it) }.orEmpty()
    if (state == null || items.isEmpty()) {
      // Nothing to show: the cat naps; it is greyed when the widget knows nothing (not set up, refused, stale).
      rv.setViewVisibility(R.id.focus, View.GONE)
      rv.setViewVisibility(R.id.quiet, View.VISIBLE)
      rv.setImageViewResource(R.id.quiet_cat, if (state == null) R.drawable.mewla_pet_offline else R.drawable.mewla_pet_idle)
      val words = state?.let { LiveView.headerWords(it) }.orEmpty()
      rv.setTextViewText(R.id.quiet_words, words)
      rv.setViewVisibility(R.id.quiet_words, if (words.isEmpty()) View.GONE else View.VISIBLE)
      rv.setOnClickPendingIntent(R.id.root, LiveNotifier.link(context, LiveView.link(null, server), 30))
      return rv
    }
    rv.setViewVisibility(R.id.quiet, View.GONE)
    rv.setViewVisibility(R.id.focus, View.VISIBLE)
    val behind = context.getColor(R.color.mewla_surface)
    if (medium) {
      val needs = items.first().asks
      rv.setImageViewResource(R.id.header_cat, if (needs) R.drawable.mewla_pet_attention else R.drawable.mewla_pet_working)
      rv.setImageViewResource(R.id.header_dot, if (needs) R.drawable.mewla_dot_seal else R.drawable.mewla_dot_ink)
      rv.setViewVisibility(R.id.header_dot, if (needs || LiveView.runningCount(state) > 0) View.VISIBLE else View.GONE)
      rv.setTextViewText(R.id.header_words, LiveView.headerWords(state))
      // The header opens the tray.
      rv.setOnClickPendingIntent(R.id.header, LiveNotifier.link(context, "mewla://sessions", 31))
      rv.setOnClickPendingIntent(R.id.root, LiveNotifier.link(context, LiveView.link(items.first(), server), 32))
      ROWS.forEachIndexed { i, row ->
        val item = items.getOrNull(i)
        if (item == null) {
          rv.setViewVisibility(row, View.GONE)
          return@forEachIndexed
        }
        rv.setViewVisibility(row, View.VISIBLE)
        rv.setImageViewBitmap(ROW_LOGO[i], LiveArt.logo(context, item, 28f, behind))
        rv.setTextViewText(ROW_TITLE[i], item.title)
        rv.setTextViewText(ROW_LINE[i], item.line)
        rv.setViewVisibility(ROW_LINE[i], if (item.line.isBlank()) View.GONE else View.VISIBLE)
        bindEnd(rv, item, ROW_LIVE[i], ROW_TIME[i], ROW_TAG[i])
        rv.setOnClickPendingIntent(row, LiveNotifier.link(context, LiveView.link(item, server), 40 + i))
      }
    } else {
      val focus = items.first()
      rv.setImageViewBitmap(R.id.logo, LiveArt.logo(context, focus, 48f, behind))
      rv.setTextViewText(R.id.title, focus.title)
      // Asking: what it asks. Running: how many more run beside it.
      val line = if (focus.asks) focus.line else LiveView.othersRunning(state, focus)
      rv.setTextViewText(R.id.line, line)
      rv.setViewVisibility(R.id.line, if (line.isBlank()) View.GONE else View.VISIBLE)
      bindEnd(rv, focus, R.id.live, R.id.time, R.id.tag)
      rv.setOnClickPendingIntent(R.id.root, LiveNotifier.link(context, LiveView.link(focus, server), 33))
    }
    return rv
  }

  /** A row's end: the red tag when it asks, else a live dot and its clock, ticked by the system. */
  private fun bindEnd(rv: RemoteViews, item: LiveItem, live: Int, time: Int, tag: Int) {
    if (item.asks) {
      rv.setViewVisibility(live, View.GONE)
      rv.setViewVisibility(tag, View.VISIBLE)
      rv.setTextViewText(tag, LiveView.tag(item))
      return
    }
    rv.setViewVisibility(tag, View.GONE)
    if (item.startedAt <= 0) {
      rv.setViewVisibility(live, View.GONE)
      return
    }
    val base = SystemClock.elapsedRealtime() - (System.currentTimeMillis() - item.startedAt).coerceAtLeast(0)
    rv.setChronometer(time, base, null, true)
    rv.setViewVisibility(live, View.VISIBLE)
  }
}

/** Shared update path: paint the cache now, then fetch and repaint. */
abstract class BaseWidgetProvider : android.appwidget.AppWidgetProvider() {
  override fun onUpdate(context: Context, manager: AppWidgetManager, ids: IntArray) {
    WidgetRender.renderAll(context)
    val pending = goAsync()
    WidgetSync.refreshAsync(context) { pending.finish() }
  }
}

