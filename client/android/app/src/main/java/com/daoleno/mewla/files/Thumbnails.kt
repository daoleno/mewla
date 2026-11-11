package com.daoleno.mewla.files

import android.content.ContentResolver
import android.graphics.Bitmap
import android.graphics.ImageDecoder
import android.net.Uri
import android.util.Base64
import java.io.ByteArrayOutputStream
import kotlin.math.max
import kotlin.math.roundToInt

/**
 * `files.thumbnail {uri, maxPixels}` (client/ui/more/files/host.ts): a picked picture, read
 * through the picker's grant and decoded by ImageDecoder (which turns it upright from its
 * EXIF orientation) at a size whose longer side is at most `maxPixels`, as a JPEG data URL;
 * null for what is not a picture it decodes.
 */
object Thumbnails {
  fun dataUrl(resolver: ContentResolver, uri: Uri, maxPixels: Int): String? {
    if (uri.scheme != ContentResolver.SCHEME_CONTENT && uri.scheme != ContentResolver.SCHEME_FILE) return null
    val picture = ImageDecoder.decodeBitmap(ImageDecoder.createSource(resolver, uri)) { decoder, info, _ ->
      val longer = max(info.size.width, info.size.height)
      if (longer > maxPixels) {
        val scale = maxPixels.toFloat() / longer
        decoder.setTargetSize((info.size.width * scale).roundToInt().coerceAtLeast(1), (info.size.height * scale).roundToInt().coerceAtLeast(1))
      }
      decoder.allocator = ImageDecoder.ALLOCATOR_SOFTWARE
    }
    val bytes = ByteArrayOutputStream()
    picture.compress(Bitmap.CompressFormat.JPEG, 85, bytes)
    picture.recycle()
    return "data:image/jpeg;base64," + Base64.encodeToString(bytes.toByteArray(), Base64.NO_WRAP)
  }
}
