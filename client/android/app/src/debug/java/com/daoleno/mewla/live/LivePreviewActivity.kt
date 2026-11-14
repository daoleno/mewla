package com.daoleno.mewla.live

import android.app.Activity
import android.graphics.Color
import android.os.Bundle
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView

/**
 * Debug builds only: the live surfaces drawn from sample states, to check their native rendering without
 * a daemon or a launcher. The widgets are the real RemoteViews applied at their true size; `--es post
 * running|needs` also posts the real live update.
 *   adb shell am start -n <pkg>/com.daoleno.mewla.live.LivePreviewActivity --es post needs
 */
class LivePreviewActivity : Activity() {
  override fun onCreate(savedInstanceState: Bundle?) {
    super.onCreate(savedInstanceState)
    val density = resources.displayMetrics.density
    val column = LinearLayout(this).apply {
      orientation = LinearLayout.VERTICAL
      setPadding((16 * density).toInt(), (40 * density).toInt(), (16 * density).toInt(), (16 * density).toInt())
      setBackgroundColor(getColor(com.daoleno.mewla.R.color.mewla_canvas))
    }
    fun label(text: String) = column.addView(TextView(this).apply { this.text = text; setTextColor(Color.GRAY); textSize = 11f })
    fun widget(state: LiveState?, medium: Boolean) {
      val host = FrameLayout(this)
      val width = ((if (medium) 338 else 158) * density).toInt()
      val height = (158 * density).toInt()
      host.addView(WidgetRender.views(this, medium, state, "").apply(this, host), FrameLayout.LayoutParams(width, height))
      column.addView(host, LinearLayout.LayoutParams(width, height).apply { bottomMargin = (10 * density).toInt() })
    }
    val running = LiveState.parse(RUNNING)
    val needs = LiveState.parse(NEEDS)
    val quiet = LiveState.parse(QUIET)
    val row = { a: LiveState?, b: LiveState? ->
      val line = LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL }
      column.addView(line)
      for (state in listOf(a, b)) {
        val host = FrameLayout(this)
        val size = (158 * density).toInt()
        host.addView(WidgetRender.views(this, false, state, "").apply(this, host), FrameLayout.LayoutParams(size, size))
        line.addView(host, LinearLayout.LayoutParams(size, size).apply { rightMargin = (10 * density).toInt(); bottomMargin = (10 * density).toInt() })
      }
    }
    label("small: running · needs you")
    row(running, needs)
    label("small: quiet · knows nothing")
    row(quiet, null)
    label("medium: running")
    widget(running, true)
    label("medium: needs you")
    widget(needs, true)
    label("medium: quiet")
    widget(quiet, true)
    setContentView(ScrollView(this).apply { addView(column) })

    when (intent.getStringExtra("post")) {
      "running" -> running?.let { LiveNotifier.post(this, it) }
      "needs" -> needs?.let { LiveNotifier.post(this, it) }
    }
  }

  private companion object {
    val NOW = System.currentTimeMillis()
    val RUNNING = """{"kind":"running","epoch":1,"seq":1,"run_key":"run:a","running":3,"items":[
      {"key":"work:a","open":"session","id":"s1","title":"Quickstart rewrite","line":"Editing docs/quickstart.md","agent":"claude","ring":{"stage":"moving","percent":45},"started_at":${NOW - 12 * 60_000}},
      {"key":"work:b","open":"session","id":"s2","title":"Release notes 0.4","line":"Reporting to Brain","agent":"codex","ring":{"stage":"last"},"started_at":${NOW - 31 * 60_000}},
      {"key":"session:c","open":"session","id":"s3","title":"mewla-site","line":"Run bun run build","agent":"opencode","started_at":${NOW - 4 * 60_000}}]}"""
    val NEEDS = """{"kind":"needs_you","epoch":1,"seq":2,"run_key":"ask:q","needs_count":1,"running":2,
      "question":{"id":"q","kind":"work","work_id":"w-price","text":"14-day or 7-day trial?","choices":["14-day","7-day"]},"items":[
      {"key":"work:w-price","open":"work","id":"w-price","title":"Pricing page","line":"14-day or 7-day trial?","agent":"claude","needs":"answer"},
      {"key":"work:a","open":"session","id":"s1","title":"Quickstart rewrite","line":"Editing docs/quickstart.md","agent":"claude","ring":{"stage":"back","percent":60},"started_at":${NOW - 12 * 60_000}},
      {"key":"work:r","open":"work","id":"r","title":"Release","line":"2 Workers","agent":"brain","started_at":${NOW - 40 * 60_000}}]}"""
    val QUIET = """{"kind":"idle","epoch":1,"seq":3,"queued":1,"items":[]}"""
  }
}
