package expo.modules.appupdate

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.provider.Settings
import androidx.core.content.FileProvider
import expo.modules.kotlin.exception.Exceptions
import expo.modules.kotlin.functions.Coroutine
import expo.modules.kotlin.functions.Queues
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition
import expo.modules.kotlin.records.Field
import expo.modules.kotlin.records.Record
import java.io.File
import java.io.FileInputStream
import java.net.HttpURLConnection
import java.net.URL
import java.security.MessageDigest
import java.util.concurrent.atomic.AtomicReference

private const val BUFFER_BYTES = 256 * 1024
private const val PROGRESS_INTERVAL_MS = 200L
private const val CONNECT_TIMEOUT_MS = 30_000
private const val READ_TIMEOUT_MS = 60_000
private const val UPDATES_DIR = "app-updates"
private const val APK_MIME = "application/vnd.android.package-archive"
private val APK_NAME = Regex("^mewla-android-arm64-v[0-9A-Za-z.+-]+\\.apk$")
private val SHA256_HEX = Regex("^[0-9a-f]{64}$")

class UpdateDownloadRequest : Record {
    @Field var url: String = ""
    @Field var fileName: String = ""
    @Field var sha256: String = ""
    @Field var size: Long = 0
}

/** A dedicated subclass keeps this provider from colliding with other merged FileProviders. */
class AppUpdateFileProvider : FileProvider()

class AppUpdateModule : Module() {
    private val active = AtomicReference<HttpURLConnection?>(null)

    override fun definition() = ModuleDefinition {
        Name("AppUpdate")
        Events("onDownloadProgress")

        // Download into the app's cache and keep the file only when its size
        // and SHA-256 match the signed release manifest.
        AsyncFunction("download") Coroutine { request: UpdateDownloadRequest ->
            download(request)
        }

        Function("cancelDownload") {
            active.getAndSet(null)?.disconnect() != null
        }

        Function("canInstallPackages") {
            context().packageManager.canRequestPackageInstalls()
        }

        AsyncFunction("openInstallSettings") {
            val context = context()
            val intent = Intent(
                Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES,
                Uri.parse("package:${context.packageName}"),
            )
            appContext.throwingActivity.startActivity(intent)
        }.runOnQueue(Queues.MAIN)

        AsyncFunction("install") { fileName: String ->
            val context = context()
            val file = verifiedFile(context, fileName)
            val uri = FileProvider.getUriForFile(context, "${context.packageName}.appupdate", file)
            val intent = Intent(Intent.ACTION_VIEW)
                .setDataAndType(uri, APK_MIME)
                .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
            appContext.throwingActivity.startActivity(intent)
        }.runOnQueue(Queues.MAIN)

        OnDestroy {
            active.getAndSet(null)?.disconnect()
        }
    }

    private fun context(): Context = appContext.reactContext ?: throw Exceptions.ReactContextLost()

    private fun download(request: UpdateDownloadRequest): Map<String, Any> {
        val url = URL(request.url)
        require(url.protocol == "https") { "Update URL must use HTTPS." }
        require(APK_NAME.matches(request.fileName)) { "Update file name is invalid." }
        val expectedSha = request.sha256.lowercase()
        require(SHA256_HEX.matches(expectedSha)) { "Update checksum is invalid." }
        require(request.size > 0) { "Update size is invalid." }

        val dir = updatesDir(context())
        val target = File(dir, request.fileName)
        // Older downloads are never needed again; a matching one is reused.
        dir.listFiles()?.forEach { if (it.name != target.name) it.delete() }
        if (target.isFile && target.length() == request.size && sha256(target) == expectedSha) {
            emitProgress(request.size, request.size)
            return mapOf("fileName" to target.name)
        }
        target.delete()

        val partial = File(dir, "${request.fileName}.part")
        val connection = (url.openConnection() as HttpURLConnection).apply {
            connectTimeout = CONNECT_TIMEOUT_MS
            readTimeout = READ_TIMEOUT_MS
            instanceFollowRedirects = true
            useCaches = false
            setRequestProperty("Accept", "application/octet-stream")
            setRequestProperty("User-Agent", "mewla-app-update")
        }
        check(active.compareAndSet(null, connection)) { "An update is already downloading." }
        try {
            val status = connection.responseCode
            check(status in 200..299) { "Download failed (HTTP $status)." }
            val digest = MessageDigest.getInstance("SHA-256")
            var received = 0L
            var lastEmit = 0L
            connection.inputStream.use { input ->
                partial.outputStream().use { output ->
                    val buffer = ByteArray(BUFFER_BYTES)
                    while (true) {
                        val read = input.read(buffer)
                        if (read < 0) break
                        received += read
                        check(received <= request.size) { "Download is larger than the release says." }
                        digest.update(buffer, 0, read)
                        output.write(buffer, 0, read)
                        val now = System.currentTimeMillis()
                        if (now - lastEmit >= PROGRESS_INTERVAL_MS) {
                            lastEmit = now
                            emitProgress(received, request.size)
                        }
                    }
                }
            }
            check(received == request.size) { "Download stopped early." }
            check(hex(digest.digest()) == expectedSha) { "Download doesn't match the signed release." }
            check(partial.renameTo(target)) { "Couldn't save the update." }
            emitProgress(received, request.size)
            return mapOf("fileName" to target.name)
        } catch (error: Throwable) {
            if (active.get() !== connection) {
                throw IllegalStateException("Download was cancelled.", error)
            }
            throw error
        } finally {
            active.compareAndSet(connection, null)
            connection.disconnect()
            partial.delete()
        }
    }

    private fun emitProgress(received: Long, total: Long) {
        sendEvent("onDownloadProgress", mapOf("received" to received, "total" to total))
    }

    private fun verifiedFile(context: Context, fileName: String): File {
        require(APK_NAME.matches(fileName)) { "Update file name is invalid." }
        val file = File(updatesDir(context), fileName)
        check(file.isFile) { "The downloaded update is gone. Download it again." }
        return file
    }

    private fun updatesDir(context: Context): File =
        File(context.cacheDir, UPDATES_DIR).apply { mkdirs() }

    private fun sha256(file: File): String {
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

    private fun hex(bytes: ByteArray): String = bytes.joinToString("") { "%02x".format(it) }
}
