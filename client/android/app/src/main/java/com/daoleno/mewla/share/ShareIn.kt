package com.daoleno.mewla.share

import android.content.ContentResolver
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.os.StatFs
import android.provider.OpenableColumns
import android.util.Log
import android.webkit.MimeTypeMap
import androidx.activity.ComponentActivity
import androidx.core.content.IntentCompat
import com.longbridge.nsh.NshView
import java.io.File
import java.io.IOException
import java.util.concurrent.Executors

/**
 * What other apps hand to Mewla, as attachments (client/ui/more/files/shareIn.ts):
 * the share sheet (ACTION_SEND, ACTION_SEND_MULTIPLE: files, pictures, text and links) and Open with (ACTION_VIEW of a
 * content:// or file:// URI, the path WeChat's 「用其他应用打开」 takes).
 *
 * A sender's read grant lasts for the receiving activity, so each file is copied into the app's cache at once,
 * streamed, never read whole (ShareStore). The app takes what is waiting:
 * - `share.take` → `[{id, text, problem, assets: [{uri, name, mimeType, size, selectionError?, selectionRetryable?}]}]`,
 *   the shares not yet acknowledged, oldest first; each asset's `uri` is a file:// URI of the copy;
 * - `share.ack {ids}`: these shares are in a composer, no longer offered;
 * - `share.release {uris}`: these copies are no longer wanted (a chip was removed or sent): they are deleted;
 * - `share.arrived` (event): a share was made while the app runs; take it.
 * Nothing is lost while the app is not ready: a share waits on disk until it is acknowledged.
 */
class ShareIn(private val activity: ComponentActivity, private val nsh: NshView) {
  private val main = Handler(Looper.getMainLooper())
  private val store = Shared.store(activity)

  init {
    nsh.handle("share.take") { _, reply -> Shared.io.execute { reply.resolve(runCatching { store.pending() }.getOrDefault(emptyList())) } }
    nsh.handle("share.ack") { data, reply ->
      val ids = ((data as? Map<*, *>)?.get("ids") as? List<*>)?.filterIsInstance<String>().orEmpty()
      Shared.io.execute { runCatching { store.ack(ids) }; reply.resolve(true) }
    }
    nsh.handle("share.release") { data, reply ->
      val uris = ((data as? Map<*, *>)?.get("uris") as? List<*>)?.filterIsInstance<String>().orEmpty()
      Shared.io.execute { runCatching { store.release(uris) }; reply.resolve(true) }
    }
  }

  /**
   * The activity starts: what the last run left that no chip holds goes, then the intent that started it is read
   * (a share). Recents and a restored activity hand the same intent again: it is not read twice.
   */
  fun start(intent: Intent?, restored: Boolean) {
    Shared.io.execute { runCatching { store.sweep(System.currentTimeMillis(), MAX_WAIT_MS) } }
    if (restored || intent == null || intent.flags and Intent.FLAG_ACTIVITY_LAUNCHED_FROM_HISTORY != 0) return
    receive(intent)
  }

  /** An intent that reached the running activity. */
  fun receive(intent: Intent?) {
    val source = intent?.let(::read) ?: return
    // Read once: nothing that asks for the activity's intent again finds the share.
    intent.action = null
    intent.data = null
    intent.clipData = null
    intent.replaceExtras(null as android.os.Bundle?)
    val context = activity.applicationContext
    Shared.io.execute {
      try {
        make(context, source)
      } catch (error: Throwable) {
        Log.w(TAG, "share failed: ${error.javaClass.simpleName}")
      }
      main.post { runCatching { nsh.send("share.arrived", emptyMap<String, Any>()) } }
    }
  }

  private fun make(context: Context, source: ShareSource) {
    val batch = store.begin()
    val resolver = context.contentResolver
    val own = context.packageName
    val roots = privateRoots(context)
    val uris = ShareRules.files(source)
    val assets = uris.mapIndexed { index, text -> copy(resolver, own, roots, batch, index, text, source.type) }
    val text = ShareRules.text(source)
    val problem = if (assets.isEmpty() && text.isEmpty()) ShareRules.NOTHING else null
    store.commit(batch, text, problem, assets, System.currentTimeMillis())
  }

  /** One file of a share, copied; a file that cannot be read becomes an asset that says why (a failed chip). */
  private fun copy(resolver: ContentResolver, own: String, roots: List<String>, batch: ShareStore.Batch, index: Int, text: String, intentType: String?): Map<String, Any?> {
    val uri = Uri.parse(text)
    fun failed(name: String, message: String, retry: Boolean = false) = mapOf(
      "uri" to text, "name" to name, "mimeType" to "application/octet-stream", "size" to null, "selectionError" to message, "selectionRetryable" to retry,
    )
    val scheme = uri.scheme
    if (scheme != ContentResolver.SCHEME_CONTENT && scheme != ContentResolver.SCHEME_FILE) return failed("Unreadable file", "Cannot read this file.")
    if (scheme == ContentResolver.SCHEME_CONTENT && ShareRules.isOwnAuthority(uri.authority, own)) return failed("Unreadable file", "Cannot read this file.")
    if (scheme == ContentResolver.SCHEME_FILE) {
      val canonical = runCatching { File(uri.path ?: "").canonicalPath }.getOrNull()
      if (canonical == null || ShareRules.isPrivatePath(canonical, roots)) return failed("Unreadable file", "Cannot read this file.")
    }
    var providerName: String? = null
    var size: Long? = null
    if (scheme == ContentResolver.SCHEME_CONTENT) {
      runCatching {
        resolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE), null, null, null)?.use { cursor ->
          if (!cursor.moveToFirst()) return@use
          cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME).takeIf { it >= 0 && !cursor.isNull(it) }?.let { providerName = cursor.getString(it) }
          cursor.getColumnIndex(OpenableColumns.SIZE).takeIf { it >= 0 && !cursor.isNull(it) }?.let { size = cursor.getLong(it).takeIf { s -> s >= 0 } }
        }
      }
    } else {
      size = File(uri.path ?: "").length().takeIf { it >= 0 }
    }
    val providerType = runCatching { resolver.getType(uri) }.getOrNull()
    val extension = { type: String? -> type?.let { MimeTypeMap.getSingleton().getExtensionFromMimeType(it) } }
    val name = ShareRules.displayName(providerName, uri.path, index, extension(ShareRules.mimeType(providerType, intentType, null).takeIf { ShareRules.isSpecificType(it) }))
    val byName = MimeTypeMap.getSingleton().getMimeTypeFromExtension(name.substringAfterLast('.', "").lowercase())
    val mimeType = ShareRules.mimeType(providerType, intentType, byName)
    // Over the limit before a byte moves: the app's own message answers (a chip that fails at once).
    if (size != null && size!! > ShareRules.MAX_BYTES) {
      return mapOf("uri" to text, "name" to name, "mimeType" to mimeType, "size" to size, "selectionRetryable" to false, "selectionError" to null)
    }
    if (size != null && !ShareRules.fits(size!!, StatFs(store.root.parentFile!!.path).availableBytes)) {
      return failed(name, "Not enough free space on this phone to receive this file.")
    }
    val target = batch.file(index)
    return try {
      val copied = (resolver.openInputStream(uri) ?: throw IOException("no stream")).use { input ->
        target.outputStream().buffered(BUFFER).use { output ->
          val buffer = ByteArray(BUFFER)
          var total = 0L
          while (true) {
            val read = input.read(buffer)
            if (read < 0) break
            total += read
            if (total > ShareRules.MAX_BYTES) break
            output.write(buffer, 0, read)
          }
          total
        }
      }
      if (copied > ShareRules.MAX_BYTES) {
        target.delete()
        // Its size was not told: the app's size limit answers all the same.
        return mapOf("uri" to text, "name" to name, "mimeType" to mimeType, "size" to ShareRules.MAX_BYTES + 1, "selectionRetryable" to false, "selectionError" to null)
      }
      mapOf("uri" to Uri.fromFile(target).toString(), "name" to name, "mimeType" to mimeType, "size" to copied, "selectionError" to null, "selectionRetryable" to null)
    } catch (error: Exception) {
      target.delete()
      Log.w(TAG, "shared file not copied: ${error.javaClass.simpleName}")
      val full = error is IOException && error.message?.contains("ENOSPC") == true
      failed(name, if (full) "Not enough free space on this phone to receive this file." else "Cannot read this file. Access was denied or it is gone.")
    }
  }

  /** What an intent holds, if it is a share (not a link: `mewla://` is `links.open`'s). */
  private fun read(intent: Intent): ShareSource? {
    val action = intent.action
    if (!ShareRules.isShare(action, intent.data?.scheme)) return null
    val streams = LinkedHashSet<String>()
    if (action == Intent.ACTION_SEND_MULTIPLE) {
      IntentCompat.getParcelableArrayListExtra(intent, Intent.EXTRA_STREAM, Uri::class.java)?.forEach { streams.add(it.toString()) }
    } else {
      IntentCompat.getParcelableExtra(intent, Intent.EXTRA_STREAM, Uri::class.java)?.let { streams.add(it.toString()) }
    }
    val clip = intent.clipData
    val clipUris = clip?.let { c -> (0 until c.itemCount).mapNotNull { i -> c.getItemAt(i).uri?.toString() } }.orEmpty()
    val texts = mutableListOf<String>()
    if (action == Intent.ACTION_SEND_MULTIPLE) {
      intent.getCharSequenceArrayListExtra(Intent.EXTRA_TEXT)?.forEach { texts.add(it.toString()) }
    } else {
      intent.getCharSequenceExtra(Intent.EXTRA_TEXT)?.let { texts.add(it.toString()) }
    }
    // A sender that only fills the clip with text.
    if (texts.isEmpty() && streams.isEmpty() && clipUris.isEmpty() && clip != null) {
      (0 until clip.itemCount).forEach { i -> clip.getItemAt(i).text?.let { texts.add(it.toString()) } }
    }
    val subject = intent.getCharSequenceExtra(Intent.EXTRA_SUBJECT)?.toString() ?: intent.getCharSequenceExtra(Intent.EXTRA_TITLE)?.toString()
    // The VIEW's own data is what is opened; a SEND's `type` says what the streams are.
    return ShareSource(action, intent.type, intent.data?.toString().takeIf { action == Intent.ACTION_VIEW }, streams.toList(), clipUris, texts, subject)
  }

  /** The app's own folders (internal, device-protected, external app-specific): never read from a sender's URI. */
  private fun privateRoots(context: Context): List<String> {
    val dirs = mutableListOf(context.applicationInfo.dataDir, context.applicationInfo.deviceProtectedDataDir, "/data/data/${context.packageName}", "/data/user/0/${context.packageName}")
    dirs.addAll(context.getExternalFilesDirs(null).filterNotNull().mapNotNull { it.parentFile?.path })
    dirs.addAll(context.externalCacheDirs.filterNotNull().mapNotNull { it.parentFile?.path })
    return dirs.filterNotNull().flatMap { dir -> listOf(dir, runCatching { File(dir).canonicalPath }.getOrDefault(dir)) }.distinct()
  }

  /** One thread for every file operation of every activity, so a start's sweep never meets a share being copied. */
  private object Shared {
    val io = Executors.newSingleThreadExecutor { Thread(it, "mewla-share").apply { isDaemon = true } }
    @Volatile private var made: ShareStore? = null
    fun store(context: Context): ShareStore = made ?: synchronized(this) { made ?: ShareStore(File(context.cacheDir, "share-in")).also { made = it; it.root.mkdirs() } }
  }

  private companion object {
    const val TAG = "MewlaShare"
    const val BUFFER = 1024 * 1024
    const val MAX_WAIT_MS = 24L * 60 * 60 * 1000
  }
}
