package expo.modules.mewlalive

import android.app.PendingIntent
import android.appwidget.AppWidgetManager
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.os.SystemClock
import android.util.TypedValue
import android.view.View
import android.widget.RemoteViews

/** Small (2x2) and medium (4x2) home-screen widgets, drawn from the cached snapshot. */
internal object WidgetRender {
  private val ROWS = intArrayOf(R.id.row0, R.id.row1)
  private val ROW_NAME = intArrayOf(R.id.row0_name, R.id.row1_name)
  private val ROW_PHASE = intArrayOf(R.id.row0_phase, R.id.row1_phase)
  private val ROW_TIME = intArrayOf(R.id.row0_time, R.id.row1_time)
  private val CHIPS = intArrayOf(R.id.chip0, R.id.chip1)

  fun renderAll(context: Context) {
    val manager = AppWidgetManager.getInstance(context)
    for (cls in listOf(SmallWidgetProvider::class.java, MediumWidgetProvider::class.java)) {
      val ids = manager.getAppWidgetIds(ComponentName(context, cls))
      if (ids.isEmpty()) continue
      for (id in ids) manager.updateAppWidget(id, views(context, cls == MediumWidgetProvider::class.java))
    }
  }

  private fun views(context: Context, medium: Boolean): RemoteViews {
    val prefs = LivePrefs(context)
    val state = if (prefs.widgetRevoked) null else LiveState.parse(prefs.widgetCache)
    val rv = RemoteViews(context.packageName, if (medium) R.layout.mewla_widget_medium else R.layout.mewla_widget_small)
    rv.setOnClickPendingIntent(R.id.root, openApp(context))
    if (state == null) {
      // Not provisioned, revoked, or nothing fetched yet: the only honest content is "open Mewla".
      rv.setInt(R.id.root, "setBackgroundResource", R.drawable.mewla_widget_bg)
      rv.setTextViewText(R.id.headline, "Open Mewla")
      // A word, not a count: the count's size would break it across lines.
      rv.setTextViewTextSize(R.id.headline, TypedValue.COMPLEX_UNIT_SP, 16f)
      rv.setTextColor(R.id.headline, color(context, R.color.mewla_text_secondary))
      rv.setViewVisibility(R.id.label, View.GONE)
      if (medium) hideDetail(rv)
      return rv
    }
    val needs = state.needsYou
    rv.setInt(R.id.root, "setBackgroundResource", if (needs) R.drawable.mewla_widget_bg_needs else R.drawable.mewla_widget_bg)
    val number = if (needs) state.needsCount.coerceAtLeast(1) else state.workerCount
    rv.setTextViewText(R.id.headline, number.toString())
    rv.setTextViewTextSize(R.id.headline, TypedValue.COMPLEX_UNIT_SP, 34f)
    rv.setTextColor(
      R.id.headline,
      color(context, if (needs) R.color.mewla_seal_text else if (number == 0) R.color.mewla_text_tertiary else R.color.mewla_text_primary),
    )
    rv.setViewVisibility(R.id.label, View.VISIBLE)
    rv.setTextViewText(R.id.label, if (needs) "Brain needs you" else "running")
    rv.setTextColor(R.id.label, color(context, if (needs) R.color.mewla_seal_text else R.color.mewla_text_secondary))
    if (medium) bindDetail(context, rv, state)
    return rv
  }

  private fun hideDetail(rv: RemoteViews) {
    rv.setViewVisibility(R.id.strip, View.GONE)
    for (row in ROWS) rv.setViewVisibility(row, View.GONE)
  }

  private fun bindDetail(context: Context, rv: RemoteViews, state: LiveState) {
    val question = state.question
    if (state.needsYou && question != null) {
      rv.setViewVisibility(R.id.strip, View.VISIBLE)
      rv.setTextViewText(R.id.strip_text, question.text.ifBlank { "Brain needs you" })
      val choices = LiveRules.actionChoices(state).take(2)
      CHIPS.forEachIndexed { i, chip ->
        if (i < choices.size) {
          rv.setViewVisibility(chip, View.VISIBLE)
          rv.setTextViewText(chip, choices[i])
          rv.setOnClickPendingIntent(chip, LiveNotifier.answerIntent(context, question.id, i))
        } else {
          rv.setViewVisibility(chip, View.GONE)
        }
      }
    } else {
      rv.setViewVisibility(R.id.strip, View.GONE)
    }
    // Room for two rows when nothing is asked, one beside the strip.
    val visibleRows = if (state.needsYou && question != null) 1 else 2
    ROWS.forEachIndexed { i, row ->
      val worker = state.workers.getOrNull(i)
      if (worker == null || i >= visibleRows) {
        rv.setViewVisibility(row, View.GONE)
      } else {
        rv.setViewVisibility(row, View.VISIBLE)
        rv.setTextViewText(ROW_NAME[i], worker.name)
        rv.setTextViewText(ROW_PHASE[i], worker.phase)
        if (state.startedAt > 0 && i == 0) {
          // The system ticks this; the widget is not redrawn every second.
          val base = SystemClock.elapsedRealtime() - (System.currentTimeMillis() - state.startedAt).coerceAtLeast(0)
          rv.setChronometer(ROW_TIME[i], base, null, true)
          rv.setViewVisibility(ROW_TIME[i], View.VISIBLE)
        } else {
          rv.setViewVisibility(ROW_TIME[i], View.GONE)
        }
      }
    }
  }

  private fun openApp(context: Context): PendingIntent {
    val launch = context.packageManager.getLaunchIntentForPackage(context.packageName)
      ?: Intent(Intent.ACTION_MAIN).setPackage(context.packageName)
    return PendingIntent.getActivity(context, 3, launch, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
  }

  private fun color(context: Context, id: Int) = context.getColor(id)
}

/** Shared update path: paint the cache now, then fetch and repaint. */
abstract class BaseWidgetProvider : android.appwidget.AppWidgetProvider() {
  override fun onUpdate(context: Context, manager: AppWidgetManager, ids: IntArray) {
    WidgetRender.renderAll(context)
    val pending = goAsync()
    WidgetSync.refreshAsync(context) { pending.finish() }
  }
}

class SmallWidgetProvider : BaseWidgetProvider()
class MediumWidgetProvider : BaseWidgetProvider()
