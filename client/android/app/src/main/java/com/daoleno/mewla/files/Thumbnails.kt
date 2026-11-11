package com.daoleno.mewla.files

import android.content.ContentResolver
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.net.Uri
import android.util.Base64
import java.io.ByteArrayOutputStream

/**
 * `files.thumbnail {uri, maxPixels}` (client/ui/more/files/host.ts): a picked picture, read
 * through the picker's grant and scaled so its longer side is at most `maxPixels`, as a JPEG
 * data URL; null for what is not a picture BitmapFactory decodes.
 */
object Thumbnails {
  fun dataUrl(resolver: ContentResolver, uri: Uri, maxPixels: Int): String? {
    if (uri.scheme != ContentResolver.SCHEME_CONTENT && uri.scheme != ContentResolver.SCHEME_FILE) return null
    val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
    resolver.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, bounds) } ?: return null
    if (bounds.outWidth <= 0 || bounds.outHeight <= 0) return null
    var sample = 1
    while (maxOf(bounds.outWidth, bounds.outHeight) / (sample * 2) >= maxPixels) sample *= 2
    val decoded = resolver.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, BitmapFactory.Options().apply { inSampleSize = sample }) } ?: return null
    val scale = maxPixels.toFloat() / maxOf(decoded.width, decoded.height)
    val picture = if (scale < 1f) {
      Bitmap.createScaledBitmap(decoded, (decoded.width * scale).toInt().coerceAtLeast(1), (decoded.height * scale).toInt().coerceAtLeast(1), true).also { if (it !== decoded) decoded.recycle() }
    } else {
      decoded
    }
    val bytes = ByteArrayOutputStream()
    picture.compress(Bitmap.CompressFormat.JPEG, 85, bytes)
    picture.recycle()
    return "data:image/jpeg;base64," + Base64.encodeToString(bytes.toByteArray(), Base64.NO_WRAP)
  }
}
