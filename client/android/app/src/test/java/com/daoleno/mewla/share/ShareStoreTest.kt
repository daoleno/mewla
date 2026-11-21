package com.daoleno.mewla.share

import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.io.File
import java.nio.file.Files

class ShareStoreTest {
  private lateinit var root: File
  private lateinit var store: ShareStore

  @Before fun make() {
    root = Files.createTempDirectory("share-in-test").toFile()
    store = ShareStore(File(root, "share-in").also { it.mkdirs() })
  }

  @After fun clean() { root.deleteRecursively() }

  private fun share(now: Long, text: String = "", files: Int = 1, problem: String? = null): ShareStore.Batch {
    val batch = store.begin()
    val assets = (0 until files).map { i ->
      batch.file(i).writeText("bytes $i")
      mapOf("uri" to batch.file(i).toURI().toString(), "name" to "f$i.txt", "mimeType" to "text/plain", "size" to 7L, "selectionError" to null)
    }
    store.commit(batch, text, problem, assets, now)
    return batch
  }

  @Test fun aShareWaitsUntilItIsAcknowledged() {
    val batch = share(10, "hello", files = 2)
    val pending = store.pending()
    assertEquals(1, pending.size)
    assertEquals(batch.id, pending[0]["id"])
    assertEquals("hello", pending[0]["text"])
    assertEquals(2, (pending[0]["assets"] as List<*>).size)
    store.ack(listOf(batch.id))
    assertEquals(0, store.pending().size)
    // The files stay for the chips.
    assertTrue(batch.file(0).isFile)
  }

  @Test fun anUnfinishedShareIsNotOffered() {
    val batch = store.begin()
    batch.file(0).writeText("half")
    assertEquals(0, store.pending().size)
  }

  @Test fun sharesComeOldestFirst() {
    val newer = share(200)
    val older = share(100)
    assertEquals(listOf(older.id, newer.id), store.pending().map { it["id"] })
  }

  @Test fun aRemovedChipsFileGoesAndItsEmptyFolder() {
    val batch = share(1, files = 2)
    store.ack(listOf(batch.id))
    val first = batch.file(0).toURI().toString()
    val second = batch.file(1).toURI().toString()
    store.release(listOf(first))
    assertFalse(batch.file(0).exists())
    assertTrue(batch.file(1).exists())
    store.release(listOf(second))
    assertFalse(batch.dir.exists())
  }

  @Test fun releaseOnlyTouchesSharedFiles() {
    val outside = File(root, "keep.txt").also { it.writeText("mine") }
    val inside = File(store.root, "notes").also { it.writeText("x") }
    store.release(listOf(outside.toURI().toString(), inside.toURI().toString(), "file:///etc/hostname", "not a uri"))
    assertTrue(outside.exists())
    assertTrue(inside.exists())
  }

  @Test fun releaseDoesNotFollowADotDotOut() {
    val batch = share(1)
    val outside = File(root, "keep.txt").also { it.writeText("mine") }
    store.release(listOf("file://${batch.dir.path}/../../keep.txt"))
    assertTrue(outside.exists())
  }

  @Test fun aStartKeepsWhatWaitsAndSweepsTheRest() {
    val waiting = share(1_000)
    val delivered = share(1_000)
    store.ack(listOf(delivered.id))
    val stale = share(1)
    val empty = store.begin()
    store.sweep(now = 5_000, maxAgeMs = 4_500)
    assertTrue(waiting.dir.exists())
    assertFalse(delivered.dir.exists())
    assertFalse(stale.dir.exists())
    assertFalse(empty.dir.exists())
    assertEquals(listOf(waiting.id), store.pending().map { it["id"] })
  }

  @Test fun aShareWithOnlyAProblemIsStillDelivered() {
    val batch = share(1, files = 0, problem = ShareRules.NOTHING)
    val pending = store.pending().single()
    assertEquals(ShareRules.NOTHING, pending["problem"])
    assertEquals(batch.id, pending["id"])
  }

  @Test fun anIdThatIsNotOursIsIgnored() {
    share(1)
    store.ack(listOf("../x", ""))
    assertEquals(1, store.pending().size)
  }
}
