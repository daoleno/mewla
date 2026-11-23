package com.daoleno.mewla.files

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.util.Log
import androidx.activity.ComponentActivity
import androidx.activity.result.contract.ActivityResultContracts
import com.longbridge.nsh.NshHostReply
import com.longbridge.nsh.NshView
import java.util.concurrent.Executors

/**
 * `files.*` (client/ui/more/files/host.ts): the system picker for attachments, a photo taken
 * now (`files.canCapture`, `files.capture`: Camera), a folder
 * for downloads, the streaming upload and download, a chat's picture saved to the gallery or
 * shared (`files.saveImage`, `files.shareImage`, PictureSave), and the `mewla.pdf` component.
 * Transfers run on their own threads; replies may come from any thread, events are
 * sent from the main one.
 */
class FilesHost(private val activity: ComponentActivity, private val nsh: NshView) {
  private val main = Handler(Looper.getMainLooper())
  private val work = Executors.newCachedThreadPool()
  private val transfers = Transfers(activity.contentResolver) { uploadId, sent, total ->
    main.post { nsh.send("files.uploadProgress", mapOf("uploadId" to uploadId, "bytesSent" to sent, "totalBytes" to total)) }
  }
  private val camera = Camera(activity)
  // The folders the person chose while the app ran: a download goes only into one of them.
  private val folders = mutableMapOf<String, Uri>()

  private var picking: Pair<NshHostReply, Int>? = null
  private val picker = activity.registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
    val (reply, maxCount) = picking ?: return@registerForActivityResult
    picking = null
    work.execute {
      try {
        reply.resolve(Picker.read(activity.contentResolver, result.resultCode, result.data, maxCount))
      } catch (error: Exception) {
        reply.reject(error.message ?: "Could not read the selected files.")
      }
    }
  }

  private var choosingFolder: NshHostReply? = null
  private val folderPicker = activity.registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
    val reply = choosingFolder ?: return@registerForActivityResult
    choosingFolder = null
    val tree = result.data?.data
    if (result.resultCode != Activity.RESULT_OK || tree == null) return@registerForActivityResult reply.resolve(null)
    val token = tree.toString()
    folders[token] = tree
    reply.resolve(mapOf("token" to token, "name" to Folders.name(activity.contentResolver, tree)))
  }

  // Android 9's storage prompt, asked when a picture is saved; the save waiting on it.
  private var savingPicture: Pair<NshHostReply, PictureSave.Request>? = null
  private val storagePermission = activity.registerForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
    val (reply, request) = savingPicture ?: return@registerForActivityResult
    savingPicture = null
    if (granted) savePicture(reply, request) else reply.reject("Storage permission denied.")
  }

  private fun savePicture(reply: NshHostReply, request: PictureSave.Request) =
    run(reply) { mapOf("name" to PictureSave.save(activity, PictureSave.read(request)), "where" to PictureSave.WHERE) }

  init {
    // This build saves and shares pictures (NSH's FilesProvider is declared): the UI shows Save and Share.
    nsh.handle("files.canSaveImage") { _, reply -> reply.resolve(true) }
    nsh.handle("files.saveImage") { data, reply ->
      val request = try { PictureSave.request(data) } catch (error: Exception) { return@handle reply.reject(error.message ?: "No picture to save.") }
      if (!PictureSave.needsPermission || activity.checkSelfPermission(Manifest.permission.WRITE_EXTERNAL_STORAGE) == PackageManager.PERMISSION_GRANTED) {
        return@handle savePicture(reply, request)
      }
      savingPicture?.first?.reject("Another picture is being saved.")
      savingPicture = reply to request
      storagePermission.launch(Manifest.permission.WRITE_EXTERNAL_STORAGE)
    }
    nsh.handle("files.shareImage") { data, reply ->
      val request = try { PictureSave.request(data) } catch (error: Exception) { return@handle reply.reject(error.message ?: "No picture to share.") }
      work.execute {
        try {
          val chooser = PictureSave.shareIntent(activity, PictureSave.read(request))
          main.post {
            try {
              activity.startActivity(chooser)
              reply.resolve(true)
            } catch (error: Exception) {
              reply.reject("Could not open the share sheet.")
            }
          }
        } catch (error: Exception) {
          Log.w(TAG, "picture share failed: ${error.javaClass.simpleName}")
          reply.reject(error.message ?: "Could not share the picture.")
        }
      }
    }
    nsh.handle("files.pick") { data, reply ->
      val maxCount = (field(data, "maxCount") as? Number)?.toInt() ?: 0
      if (maxCount !in 1..8) return@handle reply.reject("Choose between one and eight files.")
      picking?.first?.resolve(emptyList<Any>())
      picking = reply to maxCount
      try {
        picker.launch(Picker.intent())
      } catch (error: Exception) {
        picking = null
        reply.reject("Could not open the file picker.")
      }
    }
    // This build takes photos for the composer: "+" shows Camera when the device has one.
    nsh.handle("files.canCapture") { _, reply -> reply.resolve(camera.available()) }
    nsh.handle("files.capture") { _, reply -> camera.capture(reply) }
    nsh.handle("files.upload") { data, reply ->
      val request = try { Transfers.UploadRequest.from(data) } catch (error: Exception) { return@handle reply.reject(error.message ?: "Upload request is invalid.") }
      run(reply) { transfers.upload(request) }
    }
    nsh.handle("files.cancel") { data, reply ->
      val id = field(data, "uploadId") as? String
      reply.resolve(id?.let(transfers::cancelUpload) ?: false)
    }
    nsh.handle("files.pickDirectory") { _, reply ->
      choosingFolder?.resolve(null)
      choosingFolder = reply
      try {
        folderPicker.launch(Intent(Intent.ACTION_OPEN_DOCUMENT_TREE).addFlags(Intent.FLAG_GRANT_WRITE_URI_PERMISSION or Intent.FLAG_GRANT_READ_URI_PERMISSION))
      } catch (error: Exception) {
        choosingFolder = null
        reply.reject("Could not open the folder picker.")
      }
    }
    nsh.handle("files.download") { data, reply ->
      val request = try { Transfers.DownloadRequest.from(data) } catch (error: Exception) { return@handle reply.reject(error.message ?: "Download request is invalid.") }
      val tree = folders[request.directory] ?: return@handle reply.reject("Choose the folder again.")
      run(reply) { transfers.download(request, tree) }
    }
    nsh.handle("files.cancelDownload") { data, reply ->
      val id = field(data, "downloadId") as? String
      reply.resolve(id?.let(transfers::cancelDownload) ?: false)
    }
    nsh.handle("files.thumbnail") { data, reply ->
      val uri = (field(data, "uri") as? String)?.let(Uri::parse) ?: return@handle reply.resolve(null)
      val maxPixels = ((field(data, "maxPixels") as? Number)?.toInt() ?: 256).coerceIn(16, 4096)
      work.execute {
        val url = runCatching { Thumbnails.dataUrl(activity.contentResolver, uri, maxPixels) }.getOrNull()
        reply.resolve(url?.let { mapOf("dataUrl" to it) })
      }
    }
    nsh.registerComponent("mewla.pdf") { events -> PdfComponent(activity, events) }
  }

  private fun run(reply: NshHostReply, task: () -> Any?) {
    work.execute {
      try {
        reply.resolve(task())
      } catch (error: Exception) {
        Log.w(TAG, "files transfer failed: ${error.javaClass.simpleName}")
        reply.reject(error.message ?: "The transfer failed.")
      }
    }
  }

  private fun field(data: Any?, name: String): Any? = (data as? Map<*, *>)?.get(name)

  private companion object {
    const val TAG = "MewlaFiles"
  }
}
