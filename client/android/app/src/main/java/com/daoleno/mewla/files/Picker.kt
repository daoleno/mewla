package com.daoleno.mewla.files

import android.app.Activity
import android.content.ContentResolver
import android.content.Intent
import android.net.Uri
import android.provider.DocumentsContract
import android.provider.OpenableColumns
import java.io.FileNotFoundException
import java.io.IOException

/**
 * Attachment selection: the system's ACTION_GET_CONTENT (any app that offers files, not
 * only document providers), several at once. Keeps each picked content:// URI and its read
 * grant as they are; copies nothing. A file it cannot read comes back with `selectionError`.
 */
internal object Picker {
  fun intent(): Intent = Intent.createChooser(
    Intent(Intent.ACTION_GET_CONTENT).apply {
      addCategory(Intent.CATEGORY_OPENABLE)
      type = "*/*"
      putExtra(Intent.EXTRA_ALLOW_MULTIPLE, true)
      addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
    },
    "Attach files",
  )

  fun read(resolver: ContentResolver, resultCode: Int, intent: Intent?, maxCount: Int): List<Map<String, Any?>> {
    if (resultCode == Activity.RESULT_CANCELED) return emptyList()
    if (resultCode != Activity.RESULT_OK) throw IOException("Could not select this file. Try again.")
    val clip = intent?.clipData
    val uris = if (clip != null && clip.itemCount > 0) {
      (0 until clip.itemCount).map { clip.getItemAt(it).uri ?: throw IOException("The picker returned an invalid file.") }
    } else listOf(intent?.data ?: throw IOException("No file was selected. Choose files again."))
    if (uris.size > maxCount) throw IOException("Choose up to $maxCount files. Remove an attachment to make room.")
    return uris.map { uri ->
      try {
        describe(resolver, uri)
      } catch (error: Exception) {
        mapOf(
          "uri" to uri.toString(),
          "name" to "Unreadable file",
          "mimeType" to "application/octet-stream",
          "size" to null,
          "selectionError" to (error.message ?: "Cannot read this file."),
          "selectionRetryable" to (error !is FolderException && uri.scheme == ContentResolver.SCHEME_CONTENT),
        )
      }
    }
  }

  private fun describe(resolver: ContentResolver, uri: Uri): Map<String, Any?> {
    if (uri.scheme != ContentResolver.SCHEME_CONTENT || uri.authority.isNullOrBlank()) {
      throw IOException("Cannot read this file. The picker returned an invalid document URI.")
    }
    var mimeType = runCatching { resolver.getType(uri) }.getOrNull()
    var name: String? = null
    var size: Long? = null
    runCatching {
      resolver.query(uri, null, null, null, null)?.use { cursor ->
        if (!cursor.moveToFirst()) return@use
        cursor.getColumnIndex(DocumentsContract.Document.COLUMN_MIME_TYPE).takeIf { it >= 0 && !cursor.isNull(it) }?.let {
          val type = cursor.getString(it)
          if (type == DocumentsContract.Document.MIME_TYPE_DIR) mimeType = type
          if (mimeType.isNullOrBlank()) mimeType = type
        }
        cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME).takeIf { it >= 0 && !cursor.isNull(it) }?.let { name = cursor.getString(it) }
        cursor.getColumnIndex(OpenableColumns.SIZE).takeIf { it >= 0 && !cursor.isNull(it) }?.let { size = cursor.getLong(it).takeIf { s -> s >= 0 } }
      }
    }
    if (mimeType == DocumentsContract.Document.MIME_TYPE_DIR) throw FolderException()
    // Reading a byte is what proves access; metadata may fail where the stream works.
    try {
      resolver.openAssetFileDescriptor(uri, "r")?.use { it.createInputStream().use { input -> input.read() } }
        ?: throw IOException("Cannot read this file. The document provider returned no stream. Select the file again.")
    } catch (error: SecurityException) {
      throw IOException("Cannot read this file. Access was denied. Select the file again.", error)
    } catch (error: FileNotFoundException) {
      throw IOException("Cannot read this file. The document provider could not open the selected content.", error)
    } catch (error: IOException) {
      throw if (error.message?.startsWith("Cannot read this file.") == true) error else IOException("Cannot read this file. The document provider could not read it.", error)
    }
    return mapOf(
      "uri" to uri.toString(),
      "name" to (name?.takeIf { it.isNotBlank() } ?: "upload"),
      "mimeType" to (mimeType?.takeIf { it.isNotBlank() } ?: "application/octet-stream"),
      "size" to size,
    )
  }

  private class FolderException : IOException("Cannot read this file. Choose a file, not a folder.")
}
