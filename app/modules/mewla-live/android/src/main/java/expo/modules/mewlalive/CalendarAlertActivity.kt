package expo.modules.mewlalive

import android.app.Activity
import android.app.KeyguardManager
import android.app.NotificationManager
import android.graphics.Color
import android.graphics.Typeface
import android.os.Build
import android.os.Bundle
import android.os.VibrationEffect
import android.os.Vibrator
import android.os.VibratorManager
import android.view.Gravity
import android.view.ViewGroup
import android.view.WindowManager
import android.widget.Button
import android.widget.LinearLayout
import android.widget.TextView
import java.text.DateFormat
import java.util.Date

/**
 * The alarm-style alert over the lock screen. It is its own activity, shown when locked, and does not make
 * the app's main activity visible there: Open asks for the unlock first.
 */
class CalendarAlertActivity : Activity() {
  private var alert: CalendarAlert? = null

  override fun onCreate(savedInstanceState: Bundle?) {
    super.onCreate(savedInstanceState)
    val current = CalendarAlerts.alertOf(intent) ?: run { finish(); return }
    alert = current
    if (Build.VERSION.SDK_INT >= 27) {
      setShowWhenLocked(true)
      setTurnScreenOn(true)
    } else {
      @Suppress("DEPRECATION")
      window.addFlags(
        WindowManager.LayoutParams.FLAG_SHOW_WHEN_LOCKED or WindowManager.LayoutParams.FLAG_TURN_SCREEN_ON,
      )
    }
    window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
    setContentView(content(current))
    vibrate()
  }

  private fun content(current: CalendarAlert): LinearLayout {
    val pad = (24 * resources.displayMetrics.density).toInt()
    fun text(value: String, size: Float, bold: Boolean, alpha: Int) = TextView(this).apply {
      this.text = value
      textSize = size
      setTextColor(Color.argb(alpha, 255, 255, 255))
      gravity = Gravity.CENTER
      if (bold) typeface = Typeface.DEFAULT_BOLD
      setPadding(0, pad / 2, 0, pad / 2)
    }
    fun button(label: String, onClick: () -> Unit) = Button(this).apply {
      this.text = label
      isAllCaps = false
      setOnClickListener { onClick() }
      layoutParams = LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
        .apply { topMargin = pad / 3 }
    }
    return LinearLayout(this).apply {
      orientation = LinearLayout.VERTICAL
      gravity = Gravity.CENTER
      setBackgroundColor(Color.parseColor("#1B1814"))
      setPadding(pad, pad, pad, pad)
      addView(text(DateFormat.getTimeInstance(DateFormat.SHORT).format(Date(current.atMs)), 18f, false, 170))
      addView(text(current.title, 32f, true, 255))
      if (current.body.isNotEmpty()) addView(text(current.body, 18f, false, 200))
      addView(button("Done") { answer("done", 0) })
      addView(button("Snooze 5 min") { answer("snooze", 5) })
      addView(button("Snooze 15 min") { answer("snooze", 15) })
      addView(button("Open in Calendar") { open() })
    }
  }

  private fun answer(type: String, minutes: Int) {
    val current = alert ?: return
    CalendarAlerts.answer(this, type, minutes, current)
    finish()
  }

  private fun open() {
    val current = alert ?: return
    val launch = {
      getSystemService(NotificationManager::class.java)?.cancel(current.key, CalendarAlertRules.notificationId(current.key))
      startActivity(CalendarAlerts.openIntent(this, current))
      finish()
    }
    val keyguard = getSystemService(KeyguardManager::class.java)
    if (Build.VERSION.SDK_INT >= 26 && keyguard.isKeyguardLocked) {
      keyguard.requestDismissKeyguard(this, object : KeyguardManager.KeyguardDismissCallback() {
        override fun onDismissSucceeded() = launch()
      })
    } else {
      launch()
    }
  }

  private fun vibrate() {
    val pattern = longArrayOf(0, 300, 200, 300)
    if (Build.VERSION.SDK_INT >= 31) {
      getSystemService(VibratorManager::class.java).defaultVibrator
        .vibrate(VibrationEffect.createWaveform(pattern, -1))
    } else {
      @Suppress("DEPRECATION")
      (getSystemService(VIBRATOR_SERVICE) as? Vibrator)?.vibrate(VibrationEffect.createWaveform(pattern, -1))
    }
  }

  override fun onDestroy() {
    if (Build.VERSION.SDK_INT >= 31) getSystemService(VibratorManager::class.java).cancel()
    else @Suppress("DEPRECATION") (getSystemService(VIBRATOR_SERVICE) as? Vibrator)?.cancel()
    super.onDestroy()
  }
}
