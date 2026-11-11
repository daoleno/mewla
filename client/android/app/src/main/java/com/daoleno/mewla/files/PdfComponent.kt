package com.daoleno.mewla.files

import android.content.Context
import android.graphics.Bitmap
import android.graphics.Color
import android.graphics.pdf.PdfRenderer
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.os.ParcelFileDescriptor
import android.util.LruCache
import android.view.View
import android.view.ViewGroup
import android.widget.AbsListView
import android.widget.BaseAdapter
import android.widget.ImageView
import android.widget.ListView
import com.longbridge.nsh.NshComponent
import com.longbridge.nsh.NshComponentContext
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.util.UUID
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.concurrent.thread

private const val PDF_LIMIT_BYTES = 50L * 1024 * 1024

/**
 * `mewla.pdf` (client/ui/more/files/host.ts): a PDF's pages in a scrolling list, drawn by
 * PdfRenderer a page at a time as they scroll in. An http(s) `source` is fetched with
 * `headers` into the cache first and deleted with the view; a file:// one is read in place.
 */
class PdfComponent(context: Context, private val events: NshComponentContext) : NshComponent {
  private val main = Handler(Looper.getMainLooper())
  private val list = ListView(context).apply {
    divider = null
    dividerHeight = 0
    setBackgroundColor(Color.TRANSPARENT)
  }
  private var source: String? = null
  private var headers: Map<String, String> = emptyMap()
  // The size the file was inspected at (-1: unknown); a stream of any other size is a changed file.
  private var expectedBytes = -1L
  private var scheduled = false
  private var loaded: String? = null
  private var staged: File? = null
  private var descriptor: ParcelFileDescriptor? = null
  private var renderer: PdfRenderer? = null
  private var fetch: Fetch? = null
  private val pages = object : LruCache<Int, Bitmap>(6) {
    override fun entryRemoved(evicted: Boolean, key: Int, old: Bitmap, new: Bitmap?) {
      if (evicted) old.recycle()
    }
  }

  override fun view(): View = list

  override fun setProp(name: String, value: Any?) {
    when (name) {
      "source" -> source = value as? String
      "headers" -> headers = (value as? Map<*, *>)?.entries?.mapNotNull { (k, v) -> if (k is String && v is String) k to v else null }?.toMap().orEmpty()
      "expectedBytes" -> expectedBytes = (value as? Number)?.toLong()?.takeIf { it > 0 } ?: -1
      else -> return
    }
    // The props of one render come together: open once they are all set.
    if (!scheduled) {
      scheduled = true
      main.post {
        scheduled = false
        open()
      }
    }
  }

  private fun open() {
    val target = source ?: return
    if (target == loaded) return
    loaded = target
    close()
    val uri = Uri.parse(target)
    when (uri.scheme) {
      "file" -> show(File(requireNotNull(uri.path)))
      "http", "https" -> {
        val file = File(list.context.cacheDir, "mewla-pdf-${UUID.randomUUID()}.pdf")
        staged = file
        val job = Fetch(URL(target), headers, file, expectedBytes)
        fetch = job
        thread(name = "mewla-pdf") {
          val failure = runCatching { job.run() }.exceptionOrNull()
          main.post {
            if (fetch !== job) return@post
            fetch = null
            if (failure != null) fail(if (job.cancelled.get()) null else failure.message ?: "Could not download the PDF into the secure preview.")
            else show(file)
          }
        }
      }
      else -> fail("This PDF could not be opened.")
    }
  }

  private fun show(file: File) {
    try {
      val fd = ParcelFileDescriptor.open(file, ParcelFileDescriptor.MODE_READ_ONLY)
      descriptor = fd
      val pdf = PdfRenderer(fd)
      renderer = pdf
      list.adapter = Pages(pdf)
      events.emit("load", mapOf("pageCount" to pdf.pageCount))
    } catch (error: Exception) {
      fail("This PDF could not be rendered on this device.")
    }
  }

  private fun fail(message: String?) {
    close()
    if (message != null) events.emit("error", mapOf("message" to message))
  }

  private fun close() {
    fetch?.cancel()
    fetch = null
    list.adapter = null
    pages.evictAll()
    runCatching { renderer?.close() }
    renderer = null
    runCatching { descriptor?.close() }
    descriptor = null
    staged?.delete()
    staged = null
  }

  override fun destroy() {
    loaded = null
    close()
  }

  /** One row per page, as wide as the list, drawn when it scrolls in. */
  private inner class Pages(private val pdf: PdfRenderer) : BaseAdapter() {
    override fun getCount() = pdf.pageCount
    override fun getItem(position: Int) = position
    override fun getItemId(position: Int) = position.toLong()

    override fun getView(position: Int, reused: View?, parent: ViewGroup): View {
      val image = (reused as? ImageView) ?: ImageView(parent.context).apply {
        adjustViewBounds = true
        scaleType = ImageView.ScaleType.FIT_CENTER
        setPadding(0, 0, 0, 8)
      }
      val width = parent.width.takeIf { it > 0 } ?: parent.resources.displayMetrics.widthPixels
      val bitmap = pages.get(position) ?: render(position, width)?.also { pages.put(position, it) }
      image.setImageBitmap(bitmap)
      image.layoutParams = AbsListView.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
      image.contentDescription = "Page ${position + 1} of $count"
      return image
    }

    private fun render(index: Int, width: Int): Bitmap? = runCatching {
      pdf.openPage(index).use { page ->
        val height = (width.toLong() * page.height / page.width.coerceAtLeast(1)).toInt().coerceIn(1, 8192)
        Bitmap.createBitmap(width, height, Bitmap.Config.ARGB_8888).also {
          it.eraseColor(Color.WHITE)
          page.render(it, null, null, PdfRenderer.Page.RENDER_MODE_FOR_DISPLAY)
        }
      }
    }.getOrNull()
  }

  /**
   * The stream into the staged file, with the preview's size limit and, when known, the
   * inspected size: more than it, or a different total, means the file changed (stale).
   */
  private class Fetch(private val url: URL, private val headers: Map<String, String>, private val file: File, private val expected: Long) {
    private val limit = if (expected > 0) minOf(expected, PDF_LIMIT_BYTES) else PDF_LIMIT_BYTES
    val cancelled = AtomicBoolean(false)
    @Volatile private var connection: HttpURLConnection? = null

    fun run() {
      require(url.protocol == "http" || url.protocol == "https") { "This PDF could not be opened." }
      val c = (url.openConnection() as HttpURLConnection).apply {
        connectTimeout = 60_000
        readTimeout = 60_000
        instanceFollowRedirects = false
        useCaches = false
        headers.forEach { (k, v) -> setRequestProperty(k, v) }
      }
      connection = c
      try {
        if (cancelled.get()) return
        val status = c.responseCode
        if (status == 409) throw IllegalStateException("The PDF changed before it could be opened.")
        if (status !in 200..299) throw IllegalStateException("Could not download the PDF into the secure preview.")
        if (c.contentLengthLong > limit) throw IllegalStateException("The PDF stream exceeded its inspected file size.")
        var received = 0L
        c.inputStream.use { input ->
          file.outputStream().use { output ->
            val buffer = ByteArray(256 * 1024)
            while (true) {
              check(!cancelled.get()) { "cancelled" }
              val read = input.read(buffer)
              if (read < 0) break
              received += read
              if (received > limit) throw IllegalStateException("The PDF stream exceeded its inspected file size.")
              output.write(buffer, 0, read)
            }
          }
        }
        if (expected > 0 && received != expected) throw IllegalStateException("The PDF changed while it was being downloaded.")
      } finally {
        c.disconnect()
      }
    }

    fun cancel() {
      cancelled.set(true)
      connection?.disconnect()
    }
  }
}
