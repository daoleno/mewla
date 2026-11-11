package com.daoleno.mewla.appupdate

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.core.content.FileProvider
import java.io.File

/** Its own subclass, so this provider never collides with another merged FileProvider. */
class AppUpdateFileProvider : FileProvider()

/**
 * The GitHub APK updating itself: the verified download (in the cache's `app-updates/`), whether this
 * app may install packages, the "install unknown apps" settings, and the system installer for the
 * downloaded file through [AppUpdateFileProvider] (authority `<applicationId>.appupdate`).
 */
class AppUpdater(private val context: Context) {
  private val downloader = UpdateDownloader(File(context.cacheDir, UPDATES_DIR))

  /** Blocks until the APK is downloaded and verified; call it off the main thread. */
  fun download(request: UpdateRequest, progress: (received: Long, total: Long) -> Unit): File =
    downloader.download(request, progress)

  fun cancel(): Boolean = downloader.cancel()

  fun canInstallPackages(): Boolean = context.packageManager.canRequestPackageInstalls()

  /** This app's "install unknown apps" switch; Android asks once per app. */
  fun installSettingsIntent(): Intent =
    Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:${context.packageName}"))

  /** The system installer for a file [download] left. */
  fun installIntent(fileName: String): Intent {
    val file = downloader.downloaded(fileName)
    val uri = FileProvider.getUriForFile(context, "${context.packageName}.appupdate", file)
    return Intent(Intent.ACTION_VIEW)
      .setDataAndType(uri, APK_MIME)
      .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
  }

  companion object {
    const val UPDATES_DIR = "app-updates"
    const val APK_MIME = "application/vnd.android.package-archive"
    const val ABI = "arm64-v8a"

    /** The release APK is arm64 only: other devices cannot install it. */
    fun deviceTakesReleaseApk(): Boolean = Build.SUPPORTED_ABIS.orEmpty().contains(ABI)
  }
}
