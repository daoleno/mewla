package com.daoleno.mewla.files

import android.content.ContentResolver
import android.net.Uri
import android.provider.DocumentsContract
import android.provider.OpenableColumns
import java.io.BufferedInputStream
import java.io.BufferedOutputStream
import java.io.File
import java.io.InputStream
import java.io.OutputStream
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicBoolean

private const val BUFFER_BYTES = 1024 * 1024
private const val PROGRESS_INTERVAL_MS = 200L
private const val TIMEOUT_MS = 60_000

/**
 * The bytes of `files.upload` and `files.download`, streamed a megabyte at a time: no file
 * is ever held whole. Each runs on the caller's (worker) thread; cancel disconnects it.
 */
internal class Transfers(
  private val resolver: ContentResolver,
  private val progress: (uploadId: String, bytesSent: Long, totalBytes: Long) -> Unit,
) {
  class UploadRequest(val uploadId: String, val url: URL, val fileUri: Uri, val expectedSize: Long?, val headers: Map<String, String>) {
    companion object {
      fun from(data: Any?): UploadRequest {
        val m = data as? Map<*, *> ?: throw IllegalArgumentException("Upload request is invalid.")
        val id = (m["uploadId"] as? String)?.takeIf { it.isNotBlank() } ?: throw IllegalArgumentException("Upload ID is required.")
        val url = httpUrl(m["url"], "Upload URL is invalid.")
        val uri = Uri.parse(m["fileUri"] as? String ?: "")
        require(uri.scheme == ContentResolver.SCHEME_CONTENT || uri.scheme == ContentResolver.SCHEME_FILE) { "Upload file URI is invalid." }
        val size = (m["expectedSize"] as? Number)?.toLong()
        require(size == null || size >= 0) { "Upload size is invalid." }
        return UploadRequest(id, url, uri, size, headers(m["headers"]))
      }
    }
  }

  class DownloadRequest(
    val downloadId: String,
    val url: URL,
    val directory: String,
    val names: List<String>,
    val mimeType: String,
    val expectedSize: Long?,
    val maxBytes: Long,
    val headers: Map<String, String>,
  ) {
    companion object {
      fun from(data: Any?): DownloadRequest {
        val m = data as? Map<*, *> ?: throw IllegalArgumentException("Download request is invalid.")
        val id = (m["downloadId"] as? String)?.takeIf { it.isNotBlank() } ?: throw IllegalArgumentException("Download ID is required.")
        val names = (m["names"] as? List<*>)?.mapNotNull { (it as? String)?.takeIf(String::isNotBlank) }.orEmpty()
        require(names.isNotEmpty()) { "Download name is required." }
        val maxBytes = (m["maxBytes"] as? Number)?.toLong() ?: 0
        require(maxBytes > 0) { "Download byte limit is invalid." }
        val size = (m["expectedSize"] as? Number)?.toLong()
        require(size == null || size in 0..maxBytes) { "Download size is invalid." }
        return DownloadRequest(
          id,
          httpUrl(m["url"], "Download URL is invalid."),
          m["directory"] as? String ?: "",
          names,
          (m["mimeType"] as? String)?.takeIf { it.isNotBlank() } ?: "application/octet-stream",
          size,
          maxBytes,
          headers(m["headers"]),
        )
      }
    }
  }

  private val uploads = ConcurrentHashMap<String, Active>()
  private val downloads = ConcurrentHashMap<String, Active>()

  fun cancelUpload(id: String): Boolean = uploads[id]?.cancel() ?: false
  fun cancelDownload(id: String): Boolean = downloads[id]?.cancel() ?: false

  fun upload(request: UploadRequest): Map<String, Any> {
    val length = request.expectedSize ?: contentLength(request.fileUri)
    val connection = (request.url.openConnection() as HttpURLConnection).apply {
      connectTimeout = TIMEOUT_MS
      readTimeout = TIMEOUT_MS
      requestMethod = "POST"
      doOutput = true
      useCaches = false
      instanceFollowRedirects = false
      request.headers.forEach { (name, value) -> if (!name.equals("Content-Length", ignoreCase = true)) setRequestProperty(name, value) }
      setRequestProperty("X-Mewla-Upload-Transport", "android-native-v1")
      if (length >= 0) setFixedLengthStreamingMode(length) else setChunkedStreamingMode(BUFFER_BYTES)
    }
    val active = Active(connection, "Attachment upload cancelled.")
    check(uploads.putIfAbsent(request.uploadId, active) == null) { "An upload with this ID is already active." }
    try {
      openInput(request.fileUri).use { raw ->
        BufferedOutputStream(connection.outputStream, BUFFER_BYTES).use { output ->
          var sent = 0L
          var reported = 0L
          copy(BufferedInputStream(raw, BUFFER_BYTES), output, active) { read ->
            sent += read
            val now = System.currentTimeMillis()
            if (now - reported >= PROGRESS_INTERVAL_MS) {
              reported = now
              progress(request.uploadId, sent, length)
            }
          }
          output.flush()
          progress(request.uploadId, sent, if (length >= 0) length else sent)
        }
      }
      active.check()
      val status = connection.responseCode
      val body = (if (status >= 400) connection.errorStream else connection.inputStream)?.bufferedReader()?.use { it.readText() } ?: ""
      return mapOf("status" to status, "body" to body)
    } catch (error: Throwable) {
      throw IllegalStateException(if (active.cancelled.get()) "Attachment upload cancelled." else "Could not upload the attachment.", error)
    } finally {
      uploads.remove(request.uploadId, active)
      connection.disconnect()
    }
  }

  /** Into the first free name of `request.names` in `tree`; the file it made goes again when it fails. */
  fun download(request: DownloadRequest, tree: Uri): Map<String, Any> {
    val connection = (request.url.openConnection() as HttpURLConnection).apply {
      connectTimeout = TIMEOUT_MS
      readTimeout = TIMEOUT_MS
      requestMethod = "GET"
      useCaches = false
      instanceFollowRedirects = false
      request.headers.forEach { (name, value) -> setRequestProperty(name, value) }
      setRequestProperty("X-Mewla-Download-Transport", "android-native-v1")
    }
    val active = Active(connection, "Session file download was cancelled.")
    check(downloads.putIfAbsent(request.downloadId, active) == null) { "A download with this ID is already active." }
    var created: Uri? = null
    try {
      val status = connection.responseCode
      if (status !in 200..299) throw IllegalStateException("Session file download failed (HTTP $status).")
      if (connection.contentLengthLong > request.maxBytes) throw IllegalStateException("Session file download exceeded the ${request.maxBytes} byte limit.")
      active.check()
      val (document, name) = Folders.create(resolver, tree, request.names, request.mimeType)
      created = document
      var received = 0L
      (resolver.openOutputStream(document, "w") ?: throw IllegalStateException("Could not open the selected download destination.")).use { raw ->
        BufferedOutputStream(raw, BUFFER_BYTES).use { output ->
          copy(BufferedInputStream(connection.inputStream, BUFFER_BYTES), output, active) { read ->
            received += read
            if (received > request.maxBytes) throw IllegalStateException("Session file download exceeded the ${request.maxBytes} byte limit.")
          }
          output.flush()
        }
      }
      request.expectedSize?.let { expected ->
        if (received != expected) throw IllegalStateException("Session file download was truncated (expected $expected bytes, received $received).")
      }
      created = null
      return mapOf("bytesWritten" to received, "name" to name)
    } catch (error: Throwable) {
      if (active.cancelled.get()) throw IllegalStateException("Session file download was cancelled.", error)
      throw error
    } finally {
      created?.let { runCatching { DocumentsContract.deleteDocument(resolver, it) } }
      downloads.remove(request.downloadId, active)
      connection.disconnect()
    }
  }

  private inline fun copy(input: InputStream, output: OutputStream, active: Active, onRead: (Int) -> Unit) {
    val buffer = ByteArray(BUFFER_BYTES)
    while (true) {
      active.check()
      val read = input.read(buffer)
      if (read < 0) break
      onRead(read)
      output.write(buffer, 0, read)
    }
  }

  private fun openInput(uri: Uri): InputStream = when (uri.scheme) {
    ContentResolver.SCHEME_FILE -> File(requireNotNull(uri.path)).inputStream()
    else -> resolver.openInputStream(uri) ?: throw IllegalStateException("Could not open the selected file.")
  }

  /** The size a provider reports, else -1 (sent chunked); opening the stream still checks access. */
  private fun contentLength(uri: Uri): Long {
    if (uri.scheme == ContentResolver.SCHEME_FILE) return File(requireNotNull(uri.path)).length()
    return runCatching {
      resolver.query(uri, arrayOf(OpenableColumns.SIZE), null, null, null)?.use { cursor ->
        val index = cursor.getColumnIndex(OpenableColumns.SIZE)
        if (cursor.moveToFirst() && index >= 0 && !cursor.isNull(index)) cursor.getLong(index).takeIf { it >= 0 } else null
      }
    }.getOrNull() ?: -1
  }

  private class Active(private val connection: HttpURLConnection, private val cancelled_: String) {
    val cancelled = AtomicBoolean(false)
    fun cancel(): Boolean {
      val changed = cancelled.compareAndSet(false, true)
      connection.disconnect()
      return changed
    }
    fun check() = check(!cancelled.get()) { cancelled_ }
  }
}

private fun httpUrl(value: Any?, message: String): URL {
  val url = runCatching { URL(value as String) }.getOrNull() ?: throw IllegalArgumentException(message)
  require(url.protocol == "http" || url.protocol == "https") { message }
  return url
}

private fun headers(value: Any?): Map<String, String> =
  (value as? Map<*, *>)?.entries?.mapNotNull { (k, v) -> if (k is String && v is String) k to v else null }?.toMap().orEmpty()

/** A folder the person chose (ACTION_OPEN_DOCUMENT_TREE) and the files made in it. */
internal object Folders {
  fun name(resolver: ContentResolver, tree: Uri): String? = runCatching {
    val document = DocumentsContract.buildDocumentUriUsingTree(tree, DocumentsContract.getTreeDocumentId(tree))
    resolver.query(document, arrayOf(DocumentsContract.Document.COLUMN_DISPLAY_NAME), null, null, null)?.use { c ->
      if (c.moveToFirst()) c.getString(0) else null
    }
  }.getOrNull()

  /** Makes the first of `names` the folder does not have ("name (n).ext" candidates come from JS). */
  fun create(resolver: ContentResolver, tree: Uri, names: List<String>, mimeType: String): Pair<Uri, String> {
    val treeId = DocumentsContract.getTreeDocumentId(tree)
    val parent = DocumentsContract.buildDocumentUriUsingTree(tree, treeId)
    val taken = mutableSetOf<String>()
    resolver.query(DocumentsContract.buildChildDocumentsUriUsingTree(tree, treeId), arrayOf(DocumentsContract.Document.COLUMN_DISPLAY_NAME), null, null, null)?.use { c ->
      while (c.moveToNext()) c.getString(0)?.let { taken.add(it.lowercase()) }
    }
    val name = names.firstOrNull { it.lowercase() !in taken }
      ?: throw IllegalStateException("This folder already has files with this name. Choose another folder.")
    val document = DocumentsContract.createDocument(resolver, parent, mimeType, name)
      ?: throw IllegalStateException("Could not create the file in the selected folder.")
    // A provider may still rename it; say the name it has.
    val actual = runCatching {
      resolver.query(document, arrayOf(DocumentsContract.Document.COLUMN_DISPLAY_NAME), null, null, null)?.use { c -> if (c.moveToFirst()) c.getString(0) else null }
    }.getOrNull() ?: name
    return document to actual
  }
}
