package com.daoleno.mewla.appupdate

import java.io.ByteArrayInputStream
import java.io.File
import java.io.IOException
import java.io.InputStream
import java.net.URL
import java.nio.file.Files
import java.security.MessageDigest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class UpdateDownloaderTest {
  private val apk = ByteArray(700_000) { (it % 251).toByte() }
  private val sha = UpdateDownloader.hex(MessageDigest.getInstance("SHA-256").digest(apk))
  private val name = "mewla-android-arm64-v0.3.1.apk"
  private val request = UpdateRequest("https://github.com/daoleno/mewla/releases/download/v0.3.1/$name", name, sha, apk.size.toLong())

  private class Served(val status: Int, val bytes: ByteArray) {
    var opened = 0
  }

  private fun downloader(dir: File, served: Served, clock: () -> Long = { 0L }) = UpdateDownloader(dir, {
    served.opened += 1
    object : UpdateSource {
      override val status = served.status
      override val body: InputStream = ByteArrayInputStream(served.bytes)
      override fun close() {}
    }
  }, clock)

  private fun tempDir(): File = Files.createTempDirectory("appupdate").toFile()

  private fun failsWith(message: String, work: () -> Unit) {
    try {
      work()
      fail("expected: $message")
    } catch (error: Exception) {
      assertTrue("${error.message} should say $message", error.message.orEmpty().contains(message))
    }
  }

  @Test
  fun keepsAFileOnlyWhenSizeAndChecksumMatch() {
    val dir = tempDir()
    val reports = mutableListOf<Pair<Long, Long>>()
    val file = downloader(dir, Served(200, apk)).download(request) { received, total -> reports += received to total }
    assertEquals(name, file.name)
    assertTrue(file.readBytes().contentEquals(apk))
    assertEquals(apk.size.toLong() to apk.size.toLong(), reports.last())
    assertEquals(listOf(name), dir.list()!!.toList())
  }

  @Test
  fun reusesAVerifiedFileAndRemovesOlderDownloads() {
    val dir = tempDir()
    File(dir, "mewla-android-arm64-v0.3.0.apk").writeText("old")
    File(dir, name).writeBytes(apk)
    val served = Served(200, apk)
    val reports = mutableListOf<Pair<Long, Long>>()
    downloader(dir, served).download(request) { received, total -> reports += received to total }
    assertEquals(0, served.opened)
    assertEquals(listOf(apk.size.toLong() to apk.size.toLong()), reports)
    assertEquals(listOf(name), dir.list()!!.toList())
  }

  @Test
  fun downloadsAgainOverAFileThatDoesNotVerify() {
    val dir = tempDir()
    File(dir, name).writeBytes(apk.copyOf().also { it[0] = 9 })
    val served = Served(200, apk)
    downloader(dir, served).download(request) { _, _ -> }
    assertEquals(1, served.opened)
    assertTrue(File(dir, name).readBytes().contentEquals(apk))
  }

  @Test
  fun rejectsWhatTheReleaseDoesNotVouchFor() {
    val dir = tempDir()
    failsWith("doesn't match") { downloader(dir, Served(200, apk.copyOf().also { it[5] = 1 })).download(request) { _, _ -> } }
    failsWith("stopped early") { downloader(dir, Served(200, apk.copyOf(apk.size - 1))).download(request) { _, _ -> } }
    failsWith("larger") { downloader(dir, Served(200, apk + byteArrayOf(1))).download(request) { _, _ -> } }
    failsWith("HTTP 404") { downloader(dir, Served(404, ByteArray(0))).download(request) { _, _ -> } }
    // Nothing half-written stays behind to be installed.
    assertEquals(emptyList<String>(), dir.list()!!.toList())
  }

  @Test
  fun checksTheRequestBeforeAnyNetwork() {
    val dir = tempDir()
    val served = Served(200, apk)
    val d = downloader(dir, served)
    failsWith("HTTPS") { d.download(request.copy(url = "http://example.com/$name")) { _, _ -> } }
    failsWith("file name") { d.download(request.copy(fileName = "../evil.apk")) { _, _ -> } }
    failsWith("file name") { d.download(request.copy(fileName = "mewla-android-x86_64-v0.3.1.apk")) { _, _ -> } }
    failsWith("checksum") { d.download(request.copy(sha256 = "abc")) { _, _ -> } }
    failsWith("size") { d.download(request.copy(size = 0)) { _, _ -> } }
    failsWith("size") { d.download(request.copy(size = (512L shl 20) + 1)) { _, _ -> } }
    assertEquals(0, served.opened)
  }

  @Test
  fun acceptsAnUppercaseChecksum() {
    downloader(tempDir(), Served(200, apk)).download(request.copy(sha256 = sha.uppercase())) { _, _ -> }
  }

  @Test
  fun reportsProgressAboutEvery200Ms() {
    var now = 0L
    val reports = mutableListOf<Long>()
    // The clock moves 100 ms per read: every other read reports, then the end.
    downloader(tempDir(), Served(200, apk), clock = { now.also { now += 100 } }).download(request) { received, _ -> reports += received }
    assertTrue(reports.size in 2..5)
    assertEquals(apk.size.toLong(), reports.last())
  }

  @Test
  fun cancelStopsTheDownloadInProgress() {
    val dir = tempDir()
    lateinit var d: UpdateDownloader
    var closed = false
    d = UpdateDownloader(dir, {
      object : UpdateSource {
        override val status = 200
        override val body: InputStream = object : InputStream() {
          var sent = 0
          override fun read(): Int = throw UnsupportedOperationException()
          override fun read(b: ByteArray, off: Int, len: Int): Int {
            if (closed) throw IOException("socket closed")
            if (sent > 0) d.cancel()
            sent += 1
            return minOf(len, 1000)
          }
        }
        override fun close() {
          closed = true
        }
      }
    })
    failsWith("cancelled") { d.download(request) { _, _ -> } }
    assertTrue(closed)
    assertFalse(File(dir, name).exists())
    assertFalse(d.cancel())
  }

  @Test
  fun aFinishedDownloadIsFoundByItsNameOnly() {
    val dir = tempDir()
    val d = downloader(dir, Served(200, apk))
    failsWith("gone") { d.downloaded(name) }
    d.download(request) { _, _ -> }
    assertEquals(File(dir, name), d.downloaded(name))
    failsWith("file name") { d.downloaded("../../databases/x.apk") }
  }
}
