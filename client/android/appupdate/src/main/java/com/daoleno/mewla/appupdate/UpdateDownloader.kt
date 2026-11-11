package com.daoleno.mewla.appupdate

import java.io.File
import java.io.FileInputStream
import java.io.IOException
import java.io.InputStream
import java.net.HttpURLConnection
import java.net.URL
import java.security.MessageDigest

/** One release APK the signed release manifest vouches for (the app checked the signature). */
data class UpdateRequest(val url: String, val fileName: String, val sha256: String, val size: Long)

/** An open HTTP response: its status and body. */
interface UpdateSource {
  val status: Int
  val body: InputStream
  /** Stops it from any thread: a read in progress fails. */
  fun close()
}

/**
 * Downloads the release APK into [dir] (the app's cache) and keeps it only when its size and SHA-256
 * match the request. A verified file from before is reused; any other file there is removed, since an
 * older download is never needed again. Plain JVM, so the checks run in unit tests.
 */
class UpdateDownloader(
  private val dir: File,
  private val open: (URL) -> UpdateSource = ::httpSource,
  private val clock: () -> Long = System::currentTimeMillis,
) {
  private val lock = Any()
  private var active: UpdateSource? = null
  private var cancelled = false

  /** Blocks until the file is verified, reporting progress about every 200 ms; throws with what went wrong. */
  fun download(request: UpdateRequest, progress: (received: Long, total: Long) -> Unit): File {
    val url = URL(request.url)
    require(url.protocol == "https") { "Update URL must use HTTPS." }
    require(APK_NAME.matches(request.fileName)) { "Update file name is invalid." }
    val expected = request.sha256.lowercase()
    require(SHA256_HEX.matches(expected)) { "Update checksum is invalid." }
    require(request.size in 1..MAX_APK_BYTES) { "Update size is invalid." }

    dir.mkdirs()
    val target = File(dir, request.fileName)
    dir.listFiles()?.forEach { if (it.name != target.name) it.delete() }
    if (target.isFile && target.length() == request.size && sha256(target) == expected) {
      progress(request.size, request.size)
      return target
    }
    target.delete()

    val partial = File(dir, "${request.fileName}.part")
    synchronized(lock) {
      check(active == null) { "An update is already downloading." }
      cancelled = false
    }
    val source = open(url)
    synchronized(lock) {
      if (cancelled) {
        source.close()
        throw IOException("Download was cancelled.")
      }
      active = source
    }
    try {
      check(source.status in 200..299) { "Download failed (HTTP ${source.status})." }
      val digest = MessageDigest.getInstance("SHA-256")
      var received = 0L
      var lastReport = 0L
      source.body.use { input ->
        partial.outputStream().use { output ->
          val buffer = ByteArray(BUFFER_BYTES)
          while (true) {
            val read = input.read(buffer)
            if (read < 0) break
            received += read
            check(received <= request.size) { "Download is larger than the release says." }
            digest.update(buffer, 0, read)
            output.write(buffer, 0, read)
            val now = clock()
            if (now - lastReport >= PROGRESS_INTERVAL_MS) {
              lastReport = now
              progress(received, request.size)
            }
          }
        }
      }
      check(received == request.size) { "Download stopped early." }
      check(hex(digest.digest()) == expected) { "Download doesn't match the signed release." }
      check(partial.renameTo(target)) { "Couldn't save the update." }
      progress(received, request.size)
      return target
    } catch (error: Throwable) {
      if (synchronized(lock) { cancelled }) throw IOException("Download was cancelled.", error)
      throw error
    } finally {
      synchronized(lock) { if (active === source) active = null }
      source.close()
      partial.delete()
    }
  }

  /** Stops the download in progress; whether there was one. */
  fun cancel(): Boolean {
    val source = synchronized(lock) {
      cancelled = true
      active.also { active = null }
    }
    source?.close()
    return source != null
  }

  /** A file a finished download left, by the name it answered; throws when it is gone or the name is not one. */
  fun downloaded(fileName: String): File {
    require(APK_NAME.matches(fileName)) { "Update file name is invalid." }
    val file = File(dir, fileName)
    check(file.isFile) { "The downloaded update is gone. Download it again." }
    return file
  }

  companion object {
    const val BUFFER_BYTES = 256 * 1024
    const val PROGRESS_INTERVAL_MS = 200L
    const val MAX_APK_BYTES = 512L shl 20
    val APK_NAME = Regex("^mewla-android-arm64-v[0-9A-Za-z.+-]+\\.apk$")
    val SHA256_HEX = Regex("^[0-9a-f]{64}$")

    fun sha256(file: File): String {
      val digest = MessageDigest.getInstance("SHA-256")
      FileInputStream(file).use { input ->
        val buffer = ByteArray(BUFFER_BYTES)
        while (true) {
          val read = input.read(buffer)
          if (read < 0) break
          digest.update(buffer, 0, read)
        }
      }
      return hex(digest.digest())
    }

    fun hex(bytes: ByteArray): String = bytes.joinToString("") { "%02x".format(it) }
  }
}

private const val CONNECT_TIMEOUT_MS = 30_000
private const val READ_TIMEOUT_MS = 60_000

/** GitHub's release download, following its redirect to the file host. */
fun httpSource(url: URL): UpdateSource {
  val connection = (url.openConnection() as HttpURLConnection).apply {
    connectTimeout = CONNECT_TIMEOUT_MS
    readTimeout = READ_TIMEOUT_MS
    instanceFollowRedirects = true
    useCaches = false
    setRequestProperty("Accept", "application/octet-stream")
    setRequestProperty("User-Agent", "mewla-app-update")
  }
  return object : UpdateSource {
    override val status: Int get() = connection.responseCode
    override val body: InputStream get() = connection.inputStream
    override fun close() = connection.disconnect()
  }
}
