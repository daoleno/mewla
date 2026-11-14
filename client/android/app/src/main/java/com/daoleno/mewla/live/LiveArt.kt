package com.daoleno.mewla.live

import android.content.Context
import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.RectF
import androidx.core.content.ContextCompat
import com.daoleno.mewla.R
import kotlin.math.PI
import kotlin.math.min
import kotlin.math.roundToInt

/**
 * The live surfaces' one picture: an agent's logo, with the app's progress ring round it (client
 * ui/kit/ProgressHalo.tsx: the track circle, from twelve o'clock an arc in `meter` moving, `warn` sent
 * back, `ok` closed but for a small gap in the last stretch; never a number) or the red dot when it waits
 * for the person. RemoteViews and a notification's large icon can only show bitmaps, so it is drawn here.
 */
internal object LiveArt {
  private val KINDS = setOf("claude", "codex", "cursor", "grok", "pi", "opencode", "dsh")

  /** The logo for a provider: an agent's mark, Brain's mark, or the terminal glyph. */
  fun agentDrawable(agent: String): Int = when {
    agent == "brain" -> R.drawable.mewla_agent_brain
    agent in KINDS -> when (agent) {
      "claude" -> R.drawable.mewla_agent_claude
      "codex" -> R.drawable.mewla_agent_codex
      "cursor" -> R.drawable.mewla_agent_cursor
      "grok" -> R.drawable.mewla_agent_grok
      "pi" -> R.drawable.mewla_agent_pi
      "opencode" -> R.drawable.mewla_agent_opencode
      else -> R.drawable.mewla_agent_dsh
    }
    else -> R.drawable.mewla_agent_terminal
  }

  /**
   * [item]'s logo in a [sizeDp] square: inside the ring when it has one (the logo then shrinks, as a tray
   * row's does), with the red dot at the top right when it asks. [dotBehind] is the colour the dot's
   * cut-out shows (the surface under the picture).
   */
  fun logo(context: Context, item: LiveItem, sizeDp: Float, dotBehind: Int? = null): Bitmap {
    val density = context.resources.displayMetrics.density
    val size = (sizeDp * density).roundToInt().coerceAtLeast(1)
    val bitmap = Bitmap.createBitmap(size, size, Bitmap.Config.ARGB_8888)
    val canvas = Canvas(bitmap)
    val ring = if (item.asks) null else item.ring
    val stroke = (if (sizeDp >= 44f) 3.5f else 2.5f) * density
    val logoSize = if (ring != null) size * 0.62f else size * 0.86f
    if (ring != null) drawRing(context, canvas, size.toFloat(), stroke, ring)
    val drawable = ContextCompat.getDrawable(context, agentDrawable(item.agent))
    if (drawable != null) {
      val inset = ((size - logoSize) / 2f).roundToInt()
      drawable.setBounds(inset, inset, size - inset, size - inset)
      drawable.draw(canvas)
    }
    if (item.asks) {
      val r = min(5f * density, size * 0.12f)
      val cx = size - r - 1f
      val cy = r + 1f
      val paint = Paint(Paint.ANTI_ALIAS_FLAG)
      if (dotBehind != null) {
        paint.color = dotBehind
        canvas.drawCircle(cx, cy, r + 2f * density, paint)
      }
      paint.color = ContextCompat.getColor(context, R.color.mewla_seal)
      canvas.drawCircle(cx, cy, r, paint)
    }
    return bitmap
  }

  private fun drawRing(context: Context, canvas: Canvas, size: Float, stroke: Float, ring: LiveRing) {
    val paint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
      style = Paint.Style.STROKE
      strokeWidth = stroke
      color = ContextCompat.getColor(context, R.color.mewla_track)
    }
    val half = stroke / 2f
    val oval = RectF(half, half, size - half, size - half)
    canvas.drawArc(oval, 0f, 360f, false, paint)
    val r = (size - stroke) / 2f
    val sweep = sweepDegrees(ring, stroke, r)
    if (sweep <= 0f) return
    paint.color = ContextCompat.getColor(
      context,
      when {
        ring.last -> R.color.mewla_ok
        ring.stage == "back" -> R.color.mewla_warn
        else -> R.color.mewla_meter
      },
    )
    paint.strokeCap = Paint.Cap.ROUND
    canvas.drawArc(oval, -90f, sweep, false, paint)
  }

  /** The arc's sweep: as far as the value, or the whole circle less a gap of 1.6 strokes in the last stretch. */
  fun sweepDegrees(ring: LiveRing, stroke: Float, radius: Float): Float =
    if (ring.last) 360f - (stroke * 1.6f / radius) * (180f / PI.toFloat()) else 360f * ring.fraction
}
