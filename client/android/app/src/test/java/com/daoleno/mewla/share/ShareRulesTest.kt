package com.daoleno.mewla.share

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ShareRulesTest {
  private fun source(
    action: String = ShareRules.SEND, type: String? = "*/*", data: String? = null,
    streams: List<String> = emptyList(), texts: List<String> = emptyList(), subject: String? = null,
  ) = ShareSource(action, type, data, streams, texts, subject)

  @Test fun sharesAreSendAndOpenWithOfAFile_notLinks() {
    assertTrue(ShareRules.isShare(ShareRules.SEND, null))
    assertTrue(ShareRules.isShare(ShareRules.SEND_MULTIPLE, null))
    assertTrue(ShareRules.isShare(ShareRules.VIEW, "content"))
    assertTrue(ShareRules.isShare(ShareRules.VIEW, "file"))
    assertFalse(ShareRules.isShare(ShareRules.VIEW, "mewla"))
    assertFalse(ShareRules.isShare(ShareRules.VIEW, "https"))
    assertFalse(ShareRules.isShare("android.intent.action.MAIN", null))
  }

  @Test fun anOpenWithsDataIsItsFile_andAStreamIsNotCountedTwice() {
    val uri = "content://com.tencent.mm.external.fileprovider/external/tencent/MicroMsg/Download/a.pdf"
    assertEquals(listOf(uri), ShareRules.files(source(ShareRules.VIEW, "application/pdf", uri, listOf(uri))))
    assertEquals(listOf("content://a/1", "content://a/2"), ShareRules.files(source(ShareRules.SEND_MULTIPLE, streams = listOf("content://a/1", "content://a/2", "content://a/1"))))
    assertEquals(emptyList<String>(), ShareRules.files(source(texts = listOf("hi"))))
  }

  @Test fun aSendsDataIsNotAFile() {
    assertEquals(emptyList<String>(), ShareRules.files(source(ShareRules.SEND, data = "content://a/1")))
  }

  @Test fun aPagesTitleGoesAboveItsLink() {
    assertEquals("Page title\nhttps://example.com/a", ShareRules.text(source(texts = listOf("https://example.com/a"), subject = "Page title")))
  }

  @Test fun aSubjectTheTextAlreadyHasAddsNothing() {
    assertEquals("Look at this https://x.y", ShareRules.text(source(texts = listOf("Look at this https://x.y"), subject = "Look at this")))
  }

  @Test fun noTextIsNoText_evenWithASubject() {
    assertEquals("", ShareRules.text(source(texts = listOf("  ", "\n"), subject = "Title")))
    assertEquals("", ShareRules.text(source()))
  }

  @Test fun severalTextsJoinAsLines() {
    assertEquals("one\ntwo", ShareRules.text(source(ShareRules.SEND_MULTIPLE, texts = listOf("one ", " two"))))
  }

  @Test fun theProvidersTypeBeatsTheIntentsWildcard() {
    assertEquals("application/pdf", ShareRules.mimeType("application/pdf", "*/*", null))
    assertEquals("image/png", ShareRules.mimeType(null, "image/*", "image/png"))
    assertEquals("image/jpeg", ShareRules.mimeType(null, "image/jpeg", "image/png"))
    assertEquals("application/octet-stream", ShareRules.mimeType(null, "*/*", null))
    assertEquals("video/mp4", ShareRules.mimeType("application/octet-stream", null, "video/mp4"))
  }

  @Test fun theProvidersNameIsKept_asAName() {
    assertEquals("report 2026.pdf", ShareRules.displayName("report 2026.pdf", null, 0, "pdf"))
    assertEquals("a.doc", ShareRules.displayName("../../tencent/a.doc", null, 0, null))
    assertEquals("微信文件.docx", ShareRules.displayName("微信文件.docx", null, 0, null))
  }

  @Test fun noNameFallsBackToThePathThenToShared() {
    assertEquals("111.doc", ShareRules.displayName(null, "/external/tencent/MicroMsg/Download/111.doc", 0, "doc"))
    assertEquals("my file.pdf", ShareRules.displayName("  ", "/x/my%20file.pdf", 0, "pdf"))
    assertEquals("shared.png", ShareRules.displayName(null, "/media/42", 0, "png"))
    assertEquals("shared-3.png", ShareRules.displayName(null, "/media/42", 2, "png"))
    assertEquals("shared", ShareRules.displayName("", null, 0, null))
  }

  @Test fun aLongNameKeepsItsExtension() {
    val name = ShareRules.displayName("a".repeat(500) + ".pdf", null, 0, null)
    assertEquals(ShareRules.MAX_NAME_CHARS, name.length)
    assertTrue(name.endsWith(".pdf"))
  }

  @Test fun controlCharactersAndDotsGo() {
    assertEquals("ab.txt", ShareRules.clean("a\u0000b.txt"))
    assertEquals(null, ShareRules.clean("..."))
  }

  @Test fun mewlasOwnProvidersAreNotReadForASender() {
    assertTrue(ShareRules.isOwnAuthority("com.daoleno.mewla.files", "com.daoleno.mewla"))
    assertTrue(ShareRules.isOwnAuthority("com.daoleno.mewla.preview.files", "com.daoleno.mewla.preview"))
    assertTrue(ShareRules.isOwnAuthority("0@com.daoleno.mewla.preview.files", "com.daoleno.mewla.preview"))
    assertFalse(ShareRules.isOwnAuthority("com.tencent.mm.external.fileprovider", "com.daoleno.mewla"))
    assertFalse(ShareRules.isOwnAuthority("com.daoleno.mewlaevil.files", "com.daoleno.mewla"))
    assertFalse(ShareRules.isOwnAuthority(null, "com.daoleno.mewla"))
  }

  @Test fun theAppsOwnFoldersAreNotReadFromAFileUri() {
    val roots = listOf("/data/user/0/com.daoleno.mewla", "/data/data/com.daoleno.mewla")
    assertTrue(ShareRules.isPrivatePath("/data/user/0/com.daoleno.mewla/files/x", roots))
    assertTrue(ShareRules.isPrivatePath("/data/data/com.daoleno.mewla", roots))
    assertFalse(ShareRules.isPrivatePath("/data/user/0/com.daoleno.mewla2/files/x", roots))
    assertFalse(ShareRules.isPrivatePath("/storage/emulated/0/Download/a.pdf", roots))
  }

  @Test fun roomIsLeftToSpare() {
    assertTrue(ShareRules.fits(100, 1_000_000_000))
    assertFalse(ShareRules.fits(900_000_000, 920_000_000))
  }
}
