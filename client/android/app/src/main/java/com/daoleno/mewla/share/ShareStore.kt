package com.daoleno.mewla.share

import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.net.URI
import java.util.UUID

/**
 * What was shared to Mewla, copied into the app's cache, one folder per share: the files named by their number
 * (`0`, `1`, …; the name the person sees is in the manifest) and `batch.json` while the share waits for the app
 * to take it. The app acknowledges a share once its files are in a composer (`ack`: the manifest goes, the files
 * stay for the chips), and gives each file back when its chip is removed or sent (`release`). A start sweeps
 * what no chip of a living app can hold any more.
 */
internal class ShareStore(val root: File) {
  /** A share being made: a folder with no manifest yet. */
  class Batch(val id: String, val dir: File) {
    fun file(index: Int) = File(dir, index.toString())
  }

  fun begin(): Batch {
    val id = UUID.randomUUID().toString().replace("-", "").take(16)
    val dir = File(root, id)
    check(dir.mkdirs() || dir.isDirectory) { "Could not make a folder for the shared files." }
    return Batch(id, dir)
  }

  /** The share is whole: from now on `pending` lists it. Written aside and renamed, so a crash leaves no half manifest. */
  fun commit(batch: Batch, text: String, problem: String?, assets: List<Map<String, Any?>>, now: Long) {
    val manifest = JSONObject()
      .put("id", batch.id)
      .put("createdAt", now)
      .put("text", text)
      .put("problem", problem ?: JSONObject.NULL)
      .put("assets", JSONArray(assets.map { JSONObject(it.mapValues { (_, v) -> v ?: JSONObject.NULL }) }))
    val temp = File(batch.dir, "batch.json.tmp")
    temp.writeText(manifest.toString())
    check(temp.renameTo(File(batch.dir, MANIFEST))) { "Could not keep the shared files." }
  }

  /** The shares waiting for the app, oldest first. */
  fun pending(): List<Map<String, Any?>> =
    folders().mapNotNull { dir ->
      val manifest = File(dir, MANIFEST).takeIf { it.isFile } ?: return@mapNotNull null
      runCatching { JSONObject(manifest.readText()) }.getOrNull()
    }.sortedBy { it.optLong("createdAt") }.map { json ->
      mapOf(
        "id" to json.getString("id"),
        "text" to json.optString("text", ""),
        "problem" to json.optString("problem", "").takeIf { it.isNotEmpty() },
        "assets" to (0 until json.getJSONArray("assets").length()).map { i ->
          val asset = json.getJSONArray("assets").getJSONObject(i)
          asset.keys().asSequence().associateWith { key -> asset.get(key).takeUnless { it == JSONObject.NULL } }
        },
      )
    }

  /** These shares are in a composer: no longer offered, their files stay. */
  fun ack(ids: Collection<String>) {
    ids.forEach { id ->
      val dir = folder(id) ?: return@forEach
      File(dir, MANIFEST).delete()
      dropIfEmpty(dir)
    }
  }

  /** A shared file that is no longer wanted (its chip was removed, sent or dropped). Only files of this store go. */
  fun release(uris: Collection<String>) {
    uris.forEach { uri ->
      val file = runCatching { File(URI(uri)).canonicalFile }.getOrNull() ?: return@forEach
      val dir = file.parentFile ?: return@forEach
      if (dir.parentFile?.canonicalFile != root.canonicalFile || !file.name.all(Char::isDigit)) return@forEach
      file.delete()
      dropIfEmpty(dir)
    }
  }

  /**
   * A start: everything but the shares still waiting goes (a chip of the last run is gone with its app), and a
   * share that waited longer than `maxAgeMs` too. A folder with no manifest and no file is just removed.
   */
  fun sweep(now: Long, maxAgeMs: Long) {
    folders().forEach { dir ->
      val manifest = File(dir, MANIFEST)
      val created = runCatching { JSONObject(manifest.readText()).optLong("createdAt") }.getOrNull()
      if (created != null && now - created <= maxAgeMs) return@forEach
      dir.deleteRecursively()
    }
  }

  fun isEmpty(): Boolean = folders().isEmpty()

  private fun folders(): List<File> = root.listFiles { f -> f.isDirectory }?.toList().orEmpty()

  private fun folder(id: String): File? = id.takeIf { it.all { c -> c.isLetterOrDigit() } }?.let { File(root, it) }?.takeIf { it.isDirectory }

  private fun dropIfEmpty(dir: File) {
    if (dir.listFiles().isNullOrEmpty()) dir.delete()
  }

  companion object {
    const val MANIFEST = "batch.json"
  }
}
