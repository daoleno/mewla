package com.daoleno.mewla.files

import android.app.Activity
import android.content.Intent
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
 * `files.*` (client/ui/more/files/host.ts): the system picker for attachments, a folder
 * for downloads, the streaming upload and download, and the `mewla.pdf` component.
 * Transfers run on their own threads; replies may come from any thread, events are
 * sent from the main one.
 */
class FilesHost(private val activity: ComponentActivity, private val nsh: NshView) {
  private val main = Handler(Looper.getMainLooper())
  private val work = Executors.newCachedThreadPool()
  private val transfers = Transfers(activity.contentResolver) { uploadId, sent, total ->
    main.post { nsh.send("files.uploadProgress", mapOf("uploadId" to uploadId, "bytesSent" to sent, "totalBytes" to total)) }
  }
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

  init {
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
