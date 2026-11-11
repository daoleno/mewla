package com.daoleno.mewla.update

import android.app.Activity
import android.os.Handler
import android.os.Looper
import android.util.Log
import com.daoleno.mewla.BuildConfig
import com.daoleno.mewla.appupdate.AppUpdater
import com.daoleno.mewla.appupdate.UpdateRequest
import com.longbridge.nsh.NshHostReply
import com.longbridge.nsh.NshView
import java.util.concurrent.Executors

/**
 * `update.*`, the GitHub APK's own update (client/ui/update): what this build is, the verified download
 * of the release APK with its progress as the event `update.progress {received, total}`, cancelling it,
 * whether it may install packages, the "install unknown apps" settings, and the system installer.
 * The app checks the release manifest's signature; this checks the file against it.
 */
class UpdateHost(private val activity: Activity, private val nsh: NshView) {
  private val updater = AppUpdater(activity)
  private val worker = Executors.newSingleThreadExecutor { Thread(it, "mewla-app-update") }
  private val main = Handler(Looper.getMainLooper())
  // A cancelled download still answers; nothing may reach the NshView once the activity destroyed it.
  @Volatile private var destroyed = false

  init {
    nsh.handle("update.info") { _, reply ->
      answer(reply) {
        mapOf(
          "distribution" to "github-apk",
          "appId" to activity.packageName,
          "version" to BuildConfig.VERSION_NAME,
          "build" to BuildConfig.VERSION_CODE.toString(),
          "arm64" to AppUpdater.deviceTakesReleaseApk(),
        )
      }
    }
    nsh.handle("update.download") { data, reply ->
      val request = try {
        request(data)
      } catch (error: IllegalArgumentException) {
        reply.reject(error.message ?: "The update request is invalid.")
        return@handle
      }
      worker.execute {
        try {
          val file = updater.download(request) { received, total ->
            onMain { nsh.send("update.progress", mapOf("received" to received, "total" to total)) }
          }
          onMain { reply.resolve(mapOf("fileName" to file.name)) }
        } catch (error: Exception) {
          Log.w(TAG, "download failed", error)
          onMain { reply.reject(error.message ?: error.javaClass.simpleName) }
        }
      }
    }
    nsh.handle("update.cancel") { _, reply -> answer(reply) { updater.cancel() } }
    nsh.handle("update.canInstall") { _, reply -> answer(reply) { updater.canInstallPackages() } }
    nsh.handle("update.openInstallSettings") { _, reply ->
      onMain { answer(reply) { activity.startActivity(updater.installSettingsIntent()); null } }
    }
    nsh.handle("update.install") { data, reply ->
      onMain { answer(reply) { activity.startActivity(updater.installIntent(field(data, "fileName"))); null } }
    }
  }

  fun onDestroy() {
    destroyed = true
    updater.cancel()
    worker.shutdownNow()
  }

  private fun onMain(work: () -> Unit) {
    main.post { if (!destroyed) work() }
  }

  private fun request(data: Any?): UpdateRequest {
    val map = data as? Map<*, *> ?: throw IllegalArgumentException("The update request is missing.")
    val size = (map["size"] as? Number)?.toLong() ?: throw IllegalArgumentException("size is required")
    return UpdateRequest(field(data, "url"), field(data, "fileName"), field(data, "sha256"), size)
  }

  private fun field(data: Any?, name: String): String =
    (data as? Map<*, *>)?.get(name) as? String ?: throw IllegalArgumentException("$name is required")

  private fun answer(reply: NshHostReply, work: () -> Any?) {
    try {
      reply.resolve(work())
    } catch (error: Exception) {
      Log.w(TAG, "update call failed", error)
      reply.reject(error.message ?: error.javaClass.simpleName)
    }
  }

  private companion object {
    const val TAG = "MewlaUpdate"
  }
}
