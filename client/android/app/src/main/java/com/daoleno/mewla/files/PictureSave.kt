package com.daoleno.mewla.files

import android.content.ClipData
import android.content.ContentValues
import android.content.Context
import android.content.Intent
import android.media.MediaScannerConnection
import android.os.Build
import android.os.Environment
import android.provider.MediaStore
import android.util.Base64
import android.webkit.MimeTypeMap
import com.longbridge.nsh.FilesProvider
import java.io.File
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL

/**
 * A picture a chat shows (client/ui/chat/pictureActions.tsx), handed over as the app holds its original:
 * `source` is a data URL (the computer's file read whole) or a web address, decoded or downloaded here so
 * the bytes never pass through JS.
 *
 * - `files.saveImage {source, name, mimeType}` → `{name, where}`: copied byte for byte into the gallery,
 *   Pictures/Mewla. Android 10 and later need no permission for it (MediaStore); Android 9 asks for
 *   storage (FilesHost, at the moment of saving) and writes the folder itself.
 * - `files.shareImage {source, name, mimeType}`: the file, staged in the app's cache (one at a time),
 *   in the system share sheet through NSH's FilesProvider (declared in the manifest).
 */
object PictureSave {
  const val FOLDER = "Mewla"
  val WHERE = "${Environment.DIRECTORY_PICTURES}/$FOLDER"
  private const val MAX_BYTES = 50 * 1024 * 1024

  /** Only Android 9 needs the storage permission to save into Pictures. */
  val needsPermission: Boolean get() = Build.VERSION.SDK_INT < Build.VERSION_CODES.Q

  data class Request(val source: String, val name: String, val mimeType: String)

  class Picture(val bytes: ByteArray, val name: String, val mimeType: String)

  fun request(data: Any?): Request {
    val map = data as? Map<*, *> ?: throw IllegalArgumentException("No picture to save.")
    val source = (map["source"] as? String)?.takeIf { it.isNotBlank() } ?: throw IllegalArgumentException("No picture to save.")
    if (!source.startsWith("data:image/", ignoreCase = true) && !source.startsWith("https://", ignoreCase = true) && !source.startsWith("http://", ignoreCase = true)) {
      throw IllegalArgumentException("No picture to save.")
    }
    val name = ((map["name"] as? String) ?: "").replace(Regex("[\\u0000-\\u001f/\\\\]"), "_").trim().ifEmpty { "picture" }
    val type = (map["mimeType"] as? String)?.substringBefore(';')?.trim()?.lowercase().orEmpty()
    return Request(source, name, type)
  }

  /** The original's bytes and its image type: a data URL decoded, a web address downloaded (off the main thread). */
  fun read(request: Request): Picture {
    val (bytes, given) = if (request.source.startsWith("data:", ignoreCase = true)) decode(request.source) else download(request.source)
    val type = request.mimeType.ifEmpty { given }.ifEmpty {
      MimeTypeMap.getSingleton().getMimeTypeFromExtension(request.name.substringAfterLast('.', "").lowercase()).orEmpty()
    }
    if (!type.startsWith("image/")) throw IOException("That is not a picture.")
    return Picture(bytes, request.name, type)
  }

  private fun decode(source: String): Pair<ByteArray, String> {
    val comma = source.indexOf(',')
    val header = if (comma > 5) source.substring(5, comma) else throw IOException("The picture is unreadable.")
    if (!header.endsWith(";base64", ignoreCase = true)) throw IOException("The picture is unreadable.")
    val bytes = try { Base64.decode(source.substring(comma + 1), Base64.DEFAULT) } catch (error: IllegalArgumentException) { throw IOException("The picture is unreadable.") }
    return bytes to header.substringBefore(';').trim().lowercase()
  }

  private fun download(source: String): Pair<ByteArray, String> {
    val connection = URL(source).openConnection() as HttpURLConnection
    connection.connectTimeout = 15_000
    connection.readTimeout = 30_000
    try {
      val status = connection.responseCode
      if (status !in 200..299) throw IOException("The picture did not download (HTTP $status).")
      val bytes = connection.inputStream.use { input ->
        val out = java.io.ByteArrayOutputStream()
        val buffer = ByteArray(64 * 1024)
        while (true) {
          val read = input.read(buffer)
          if (read < 0) break
          out.write(buffer, 0, read)
          if (out.size() > MAX_BYTES) throw IOException("The picture is too large to save.")
        }
        out.toByteArray()
      }
      return bytes to (connection.contentType ?: "").substringBefore(';').trim().lowercase()
    } finally {
      connection.disconnect()
    }
  }

  /** Copies the picture into Pictures/Mewla; the name it got there (the gallery adds " (1)" to a taken one). */
  fun save(context: Context, picture: Picture): String =
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) saveToMediaStore(context, picture) else saveToFolder(context, picture)

  private fun saveToMediaStore(context: Context, picture: Picture): String {
    val resolver = context.contentResolver
    val values = ContentValues().apply {
      put(MediaStore.Images.Media.DISPLAY_NAME, picture.name)
      put(MediaStore.Images.Media.MIME_TYPE, picture.mimeType)
      put(MediaStore.Images.Media.RELATIVE_PATH, WHERE)
      put(MediaStore.Images.Media.IS_PENDING, 1)
    }
    val uri = resolver.insert(MediaStore.Images.Media.getContentUri(MediaStore.VOLUME_EXTERNAL_PRIMARY), values)
      ?: throw IOException("The gallery did not take the picture.")
    try {
      val out = resolver.openOutputStream(uri) ?: throw IOException("The gallery did not take the picture.")
      out.use { it.write(picture.bytes) }
      resolver.update(uri, ContentValues().apply { put(MediaStore.Images.Media.IS_PENDING, 0) }, null, null)
    } catch (error: Exception) {
      runCatching { resolver.delete(uri, null, null) }
      throw error
    }
    return resolver.query(uri, arrayOf(MediaStore.Images.Media.DISPLAY_NAME), null, null, null)?.use { cursor ->
      if (cursor.moveToFirst()) cursor.getString(0) else null
    } ?: picture.name
  }

  @Suppress("DEPRECATION")
  private fun saveToFolder(context: Context, picture: Picture): String {
    val folder = File(Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_PICTURES), FOLDER)
    if (!folder.isDirectory && !folder.mkdirs()) throw IOException("Could not make the Pictures/$FOLDER folder.")
    val target = freeName(folder, picture.name) ?: throw IOException("Pictures/$FOLDER has too many pictures called ${picture.name}.")
    try {
      target.writeBytes(picture.bytes)
    } catch (error: Exception) {
      target.delete()
      throw error
    }
    MediaScannerConnection.scanFile(context, arrayOf(target.path), arrayOf(picture.mimeType), null)
    return target.name
  }

  private fun freeName(folder: File, name: String): File? {
    val dot = name.lastIndexOf('.')
    val stem = if (dot > 0) name.substring(0, dot) else name
    val extension = if (dot > 0) name.substring(dot) else ""
    return (0..999).asSequence().map { File(folder, if (it == 0) name else "$stem ($it)$extension") }.firstOrNull { !it.exists() }
  }

  /** The picture staged in the app's cache, the last one there replaced, as the share sheet's intent. */
  fun shareIntent(context: Context, picture: Picture): Intent {
    val folder = File(context.cacheDir, "pictures")
    folder.listFiles()?.forEach { it.delete() }
    if (!folder.isDirectory && !folder.mkdirs()) throw IOException("Could not stage the picture.")
    val file = File(folder, picture.name)
    file.writeBytes(picture.bytes)
    val uri = FilesProvider.uriFor(context, file)
    val send = Intent(Intent.ACTION_SEND)
      .setType(picture.mimeType)
      .putExtra(Intent.EXTRA_STREAM, uri)
      .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
    send.clipData = ClipData.newRawUri(picture.name, uri)
    return Intent.createChooser(send, null).addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
  }
}
