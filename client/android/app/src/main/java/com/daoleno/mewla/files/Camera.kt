package com.daoleno.mewla.files

import android.Manifest
import android.content.ActivityNotFoundException
import android.content.pm.PackageManager
import android.net.Uri
import androidx.activity.ComponentActivity
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.content.ContextCompat
import androidx.core.content.FileProvider
import com.longbridge.nsh.NshHostReply
import java.io.File
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

/**
 * `files.canCapture` and `files.capture` (client/ui/more/files/host.ts): a photo taken now for the composer's
 * attachments. The system camera app takes it (ACTION_IMAGE_CAPTURE) into the app's cache through a FileProvider of
 * its own (`<package>.camera`, cache/camera only: NSH's FilesProvider opens files read-only); the reply is that
 * file as a picked one (a file:// URI that `files.upload` streams and `files.thumbnail` reads). The manifest holds CAMERA for the pairing scanner, so Android refuses the capture
 * intent until it is granted: the system's prompt comes first, on the first photo. Captures older than a day go
 * when the next one is taken (the cache is the system's to clear too).
 */
internal class Camera(private val activity: ComponentActivity) {
  private var capturing: Pair<NshHostReply, File>? = null
  private var asking: NshHostReply? = null

  private val take = activity.registerForActivityResult(ActivityResultContracts.TakePicture()) { saved ->
    val (reply, file) = capturing ?: return@registerForActivityResult
    capturing = null
    if (!saved || !file.isFile || file.length() == 0L) {
      file.delete()
      return@registerForActivityResult reply.resolve(null)
    }
    reply.resolve(mapOf("uri" to Uri.fromFile(file).toString(), "name" to file.name, "mimeType" to "image/jpeg", "size" to file.length()))
  }

  private val permission = activity.registerForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
    val reply = asking ?: return@registerForActivityResult
    asking = null
    if (granted) launch(reply) else reply.reject(DENIED)
  }

  fun available(): Boolean = activity.packageManager.hasSystemFeature(PackageManager.FEATURE_CAMERA_ANY)

  fun capture(reply: NshHostReply) {
    if (!available()) return reply.reject("This device has no camera.")
    if (ContextCompat.checkSelfPermission(activity, Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED) return launch(reply)
    asking?.resolve(null)
    asking = reply
    permission.launch(Manifest.permission.CAMERA)
  }

  private fun launch(reply: NshHostReply) {
    capturing?.let { (earlier, file) -> file.delete(); earlier.resolve(null) }
    val folder = File(activity.cacheDir, FOLDER).apply { mkdirs() }
    val dayAgo = System.currentTimeMillis() - 24 * 60 * 60 * 1000L
    folder.listFiles()?.filter { it.lastModified() < dayAgo }?.forEach { it.delete() }
    val file = File(folder, "IMG_${SimpleDateFormat("yyyyMMdd_HHmmss", Locale.US).format(Date())}.jpg")
    capturing = reply to file
    try {
      take.launch(FileProvider.getUriForFile(activity, "${activity.packageName}.camera", file))
    } catch (error: ActivityNotFoundException) {
      capturing = null
      reply.reject("No camera app can take a photo.")
    } catch (error: Exception) {
      capturing = null
      reply.reject("Could not open the camera.")
    }
  }

  private companion object {
    const val FOLDER = "camera"
    const val DENIED = "Allow the camera for Mewla in Settings to take a photo."
  }
}
