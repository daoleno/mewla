package com.daoleno.mewla

import android.app.Activity
import android.app.ActivityManager
import android.app.Application
import android.app.ApplicationExitInfo
import android.content.Context
import android.os.Build
import android.os.Bundle
import android.os.SystemClock
import android.util.Log
import com.longbridge.nsh.NshView
import java.io.File
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.TimeZone

/**
 * What took the app down, kept in app storage until the person has seen it (`app.crashReports`,
 * `app.clearCrashReports`): an uncaught Java/Kotlin exception (any thread, any way the process
 * started), a native crash or an ANR (read back from ApplicationExitInfo on the next start;
 * Android 11+), and the app's own JS errors. At most [KEEP] reports, newest kept.
 */
object CrashLog {
  private const val TAG = "Mewla"
  private const val KEEP = 8
  private const val LIMIT = 64 * 1024

  /** Whether an activity of the app is resumed (the app in front), and since when ([SystemClock.elapsedRealtime]). */
  @Volatile private var inFront = false
  @Volatile private var frontChangedAt = SystemClock.elapsedRealtime()
  @Volatile private var everInFront = false

  /** Called first thing in [MewlaApplication.onCreate]. */
  fun install(context: Context) {
    val app = context.applicationContext
    (app as? Application)?.registerActivityLifecycleCallbacks(FrontWatch)
    val previous = Thread.getDefaultUncaughtExceptionHandler()
    Thread.setDefaultUncaughtExceptionHandler { thread, error ->
      runCatching { write(app, "java", "Uncaught exception on thread \"${thread.name}\"\n\n${Log.getStackTraceString(error)}", live = true) }
      previous?.uncaughtException(thread, error)
    }
    runCatching { readExits(app) }.onFailure { Log.w(TAG, "exit reasons not read", it) }
  }

  /** An error the app's JS reported (NSH's error listener in a release build). */
  fun js(context: Context, message: String) {
    runCatching { write(context.applicationContext, "js", message, live = true) }
  }

  /** The JS stopped answering and [JsWatchdog] reopens the app: shown as "Stopped responding". */
  fun stalled(context: Context, silentMs: Long) {
    runCatching { write(context.applicationContext, "anr", "The app stopped answering for ${silentMs / 1000} s (its JS thread) and was reopened.\n", live = true) }
  }

  /** The host calls the app reads them with. */
  fun register(context: Context, nsh: NshView) {
    nsh.handle("app.crashReports") { _, reply -> reply.resolve(list(context)) }
    nsh.handle("app.clearCrashReports") { data, reply ->
      val ids = ((data as? Map<*, *>)?.get("ids") as? List<*>)?.filterIsInstance<String>().orEmpty()
      for (id in ids) dir(context).resolve(id).takeIf { it.name == id && it.extension == "txt" }?.delete()
      reply.resolve(null)
    }
  }

  private fun dir(context: Context) = File(context.filesDir, "crashes").apply { mkdirs() }

  private fun list(context: Context): List<Map<String, Any>> =
    dir(context).listFiles { file -> file.extension == "txt" }.orEmpty().sortedBy { it.name }.map { file ->
      val (kind, at) = file.nameWithoutExtension.split("-", limit = 3).let { (it.getOrNull(1) ?: "java") to (it.getOrNull(0)?.toLongOrNull() ?: file.lastModified()) }
      mapOf("id" to file.name, "kind" to kind, "at" to at, "text" to file.readText())
    }

  /** `live`: it happened in this process, now, so whether the app was in front then is known (not for the last run's exits). */
  private fun write(context: Context, kind: String, body: String, at: Long = System.currentTimeMillis(), live: Boolean = false) {
    val folder = dir(context)
    val text = header(context, kind, at, live) + body
    folder.resolve("$at-$kind-${(Math.random() * 1e6).toLong()}.txt").writeText(if (text.length > LIMIT) text.take(LIMIT) + "\n…" else text)
    folder.listFiles { file -> file.extension == "txt" }.orEmpty().sortedByDescending { it.name }.drop(KEEP).forEach { it.delete() }
  }

  private fun header(context: Context, kind: String, at: Long, live: Boolean): String {
    val info = runCatching { context.packageManager.getPackageInfo(context.packageName, 0) }.getOrNull()
    val time = SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss'Z'", Locale.ROOT).apply { timeZone = TimeZone.getTimeZone("UTC") }.format(Date(at))
    return buildString {
      append("Mewla ${info?.versionName ?: "?"} (${info?.longVersionCode ?: "?"}, ${if (BuildConfig.DEBUG) "debug" else "release"}) ${context.packageName}\n")
      append("${Build.MANUFACTURER} ${Build.MODEL}, Android ${Build.VERSION.RELEASE} (API ${Build.VERSION.SDK_INT}), ${Build.SUPPORTED_ABIS.firstOrNull() ?: "?"}\n")
      append("$kind at $time\n")
      if (live) append(frontLine()).append('\n')
      append('\n')
    }
  }

  /**
   * Whether the app was in front, and for how long it had been (or how long since it was last): what tells a call
   * NSH cut off because the system froze the app in the background from one that ran too long (core/crash.ts reads it).
   */
  private fun frontLine(): String {
    val seconds = (SystemClock.elapsedRealtime() - frontChangedAt) / 1000
    return when {
      inFront -> "In front for $seconds s"
      everInFront -> "In the background for $seconds s"
      else -> "In the background for $seconds s, not in front since it started"
    }
  }

  private object FrontWatch : Application.ActivityLifecycleCallbacks {
    override fun onActivityResumed(activity: Activity) {
      inFront = true
      everInFront = true
      frontChangedAt = SystemClock.elapsedRealtime()
    }

    override fun onActivityPaused(activity: Activity) {
      inFront = false
      frontChangedAt = SystemClock.elapsedRealtime()
    }

    override fun onActivityCreated(activity: Activity, state: Bundle?) {}
    override fun onActivityStarted(activity: Activity) {}
    override fun onActivityStopped(activity: Activity) {}
    override fun onActivitySaveInstanceState(activity: Activity, state: Bundle) {}
    override fun onActivityDestroyed(activity: Activity) {}
  }

  /** Native crashes and ANRs the system recorded since the last start (Java crashes are already written). */
  private fun readExits(context: Context) {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.R) return
    val prefs = context.getSharedPreferences("mewla.crash", Context.MODE_PRIVATE)
    val seen = prefs.getLong("exit_seen", 0L)
    val exits = (context.getSystemService(Context.ACTIVITY_SERVICE) as ActivityManager)
      .getHistoricalProcessExitReasons(context.packageName, 0, 16)
    for (exit in exits.filter { it.timestamp > seen }.sortedBy { it.timestamp }) {
      val kind = when (exit.reason) {
        ApplicationExitInfo.REASON_CRASH_NATIVE -> "native"
        ApplicationExitInfo.REASON_ANR -> "anr"
        else -> continue
      }
      val trace = runCatching { exit.traceInputStream?.use { it.readBytes() } }.getOrNull()
      val body = buildString {
        append(if (kind == "native") "Native crash" else "Not responding (ANR)")
        exit.description?.let { append(": $it") }
        append("\n\n")
        // An ANR's trace is text; a native crash's is a tombstone protobuf: its readable strings
        // (signal, abort message, frames' libraries and functions) are what a person can hand over.
        if (trace != null) append(if (kind == "anr") String(trace) else readableStrings(trace))
      }
      write(context, kind, body, exit.timestamp)
    }
    exits.maxOfOrNull { it.timestamp }?.let { if (it > seen) prefs.edit().putLong("exit_seen", it).apply() }
  }

  private fun readableStrings(bytes: ByteArray): String {
    val out = StringBuilder()
    val run = StringBuilder()
    for (byte in bytes) {
      val c = byte.toInt() and 0xff
      if (c in 0x20..0x7e) run.append(c.toChar()) else {
        if (run.length >= 6) out.append(run).append('\n')
        run.setLength(0)
      }
    }
    if (run.length >= 6) out.append(run)
    return out.toString()
  }
}
