package com.daoleno.mewla

import android.app.Activity
import android.content.Context
import android.content.Intent
import android.os.Handler
import android.os.Looper
import android.os.Process
import android.os.SystemClock
import android.util.Log
import com.longbridge.nsh.NshView

/**
 * Reopens the app when its JS stops answering, instead of leaving a frozen screen the person has to kill.
 * NSH's engine thread can spin in native code with no way to stop it (NSH 0.5.0's freeze, longbridge/nsh#1984, is
 * fixed in 0.5.1 by #1990 and #1991; this stays for whatever else stalls it); the main thread stays free. The app beats (`app.alive`, App.tsx) every [WatchdogRules.BEAT_MS].
 * While the activity is resumed, [WatchdogRules.STALL_MS] without a beat relaunches the app and ends
 * this process, at most [WatchdogRules.MAX_RESTARTS] times in [WatchdogRules.WINDOW_MS]; past that the
 * app is left as it is rather than restarting in a loop.
 */
class JsWatchdog(private val activity: Activity, nsh: NshView) {
  private val main = Handler(Looper.getMainLooper())
  private val stall = WatchdogRules.Stall()
  private var running = false
  private var gaveUp = false

  private val check = object : Runnable {
    override fun run() {
      if (!running) return
      val silent = stall.silentFor(SystemClock.uptimeMillis())
      if (silent != null) restart(silent) else main.postDelayed(this, WatchdogRules.BEAT_MS)
    }
  }

  init {
    nsh.handle("app.alive") { _, reply ->
      stall.beat(SystemClock.uptimeMillis())
      reply.resolve(null)
    }
  }

  fun resumed() {
    stall.resumed(SystemClock.uptimeMillis())
    if (running) return
    running = true
    main.postDelayed(check, WatchdogRules.BEAT_MS)
  }

  fun paused() {
    running = false
    main.removeCallbacks(check)
  }

  private fun restart(silentMs: Long) {
    val prefs = activity.getSharedPreferences("mewla.watchdog", Context.MODE_PRIVATE)
    val now = System.currentTimeMillis()
    val history = WatchdogRules.readHistory(prefs.getString("restarts", null))
    if (!WatchdogRules.mayRestart(history, now)) {
      if (!gaveUp) Log.w(TAG, "js stalled ${silentMs / 1000}s; restarted ${history.size} times lately, leaving it")
      gaveUp = true
      main.postDelayed(check, WatchdogRules.BEAT_MS)
      return
    }
    // commit, not apply: the process ends right after.
    prefs.edit().putString("restarts", WatchdogRules.writeHistory(WatchdogRules.recent(history, now) + now)).commit()
    Log.w(TAG, "js stalled ${silentMs / 1000}s, restarting")
    CrashLog.stalled(activity, silentMs)
    val launch = activity.packageManager.getLaunchIntentForPackage(activity.packageName)
    if (launch == null) {
      main.postDelayed(check, WatchdogRules.BEAT_MS)
      return
    }
    launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TASK)
    activity.startActivity(launch)
    Process.killProcess(Process.myPid())
  }

  private companion object {
    const val TAG = "MewlaWatchdog"
  }
}

/** The watchdog's decisions, apart from Android. */
object WatchdogRules {
  const val BEAT_MS = 2_000L
  const val STALL_MS = 10_000L
  const val MAX_RESTARTS = 2
  const val WINDOW_MS = 5 * 60_000L

  /**
   * How long the JS has been silent while the app is in front. Armed by the first beat (a slow cold start is
   * not a stall); counted from the later of the last beat and the last resume (timers may wait in the background).
   */
  class Stall {
    @Volatile private var lastBeat = 0L
    @Volatile private var since = 0L

    fun beat(now: Long) { lastBeat = now }
    fun resumed(now: Long) { since = now }

    /** The silence in ms when it is a stall, else null. */
    fun silentFor(now: Long): Long? {
      if (lastBeat == 0L) return null
      return (now - maxOf(lastBeat, since)).takeIf { it > STALL_MS }
    }
  }

  fun recent(history: List<Long>, now: Long): List<Long> = history.filter { now - it in 0 until WINDOW_MS }
  fun mayRestart(history: List<Long>, now: Long): Boolean = recent(history, now).size < MAX_RESTARTS
  fun readHistory(text: String?): List<Long> = text.orEmpty().split(',').mapNotNull { it.trim().toLongOrNull() }
  fun writeHistory(history: List<Long>): String = history.joinToString(",")
}
