package com.daoleno.mewla.share

/**
 * What another app handed to Mewla, as plain data (the Intent is read into this by ShareIntent):
 * the rules below are plain Kotlin so the unit tests can run them.
 */
internal data class ShareSource(
  val action: String?,
  /** The intent's MIME type; may be a wildcard (`*` / `*`). */
  val type: String?,
  /** An ACTION_VIEW's data, as the string it came in. */
  val data: String?,
  /** EXTRA_STREAM and ClipData URIs, in order, as strings. */
  val streams: List<String>,
  /** EXTRA_TEXT (several of them for SEND_MULTIPLE) or the ClipData's text. */
  val texts: List<String>,
  val subject: String?,
)

internal object ShareRules {
  const val SEND = "android.intent.action.SEND"
  const val SEND_MULTIPLE = "android.intent.action.SEND_MULTIPLE"
  const val VIEW = "android.intent.action.VIEW"

  /** The daemon's and the app's own file limit (core/files/uploads.ts V1_MAX_UPLOAD_FILE_BYTES). */
  const val MAX_BYTES = 2L * 1024 * 1024 * 1024
  const val MAX_NAME_CHARS = 200
  const val NOTHING = "Nothing usable was shared with Mewla."

  /** A share or an Open with, not a link: `mewla://` and web links stay `links.open`'s. */
  fun isShare(action: String?, dataScheme: String?): Boolean = when (action) {
    SEND, SEND_MULTIPLE -> true
    VIEW -> dataScheme == "content" || dataScheme == "file"
    else -> false
  }

  /** The files of a share: a VIEW's data, then the streams, each once. */
  fun files(source: ShareSource): List<String> =
    ((if (source.action == VIEW) listOfNotNull(source.data) else emptyList()) + source.streams)
      .filter { it.isNotBlank() }
      .distinct()

  /** The text of a share, after its subject when that adds something: a page's title above its link. */
  fun text(source: ShareSource): String {
    val body = source.texts.map { it.trim() }.filter { it.isNotEmpty() }.joinToString("\n")
    val subject = source.subject?.trim().orEmpty()
    if (body.isEmpty()) return ""
    return if (subject.isEmpty() || body.contains(subject)) body else "$subject\n$body"
  }

  /** `application/pdf` is a type; a wildcard or none is not. */
  fun isSpecificType(type: String?): Boolean =
    type != null && type.contains('/') && !type.endsWith("/*") && !type.startsWith("*/") && type != "application/octet-stream"

  /** The provider's type, else the intent's, else the name's, else bytes. */
  fun mimeType(provider: String?, intent: String?, byName: String?): String =
    listOf(provider, intent, byName).firstOrNull { isSpecificType(it) } ?: "application/octet-stream"

  /**
   * The name the person sees and the computer keeps: the provider's display name; else the last piece of the
   * URI's path when it has an extension; else `shared` (numbered from the second) with the type's extension.
   * Never a path, never control characters, at most 200 characters with its extension kept.
   */
  fun displayName(provided: String?, uriPath: String?, index: Int, extensionOfType: String?): String {
    clean(provided)?.let { return it }
    clean(uriPath?.substringAfterLast('/')?.let(::decode))?.takeIf { it.lastIndexOf('.') in 1 until it.length - 1 }?.let { return it }
    val base = if (index == 0) "shared" else "shared-${index + 1}"
    return if (extensionOfType.isNullOrBlank()) base else "$base.$extensionOfType"
  }

  private fun decode(value: String): String = runCatching { java.net.URLDecoder.decode(value.replace("+", "%2B"), "UTF-8") }.getOrDefault(value)

  fun clean(name: String?): String? {
    val last = name?.substringAfterLast('/')?.substringAfterLast('\\') ?: return null
    val text = last.filter { !it.isISOControl() }.trim().trim('.')
    if (text.isEmpty()) return null
    if (text.length <= MAX_NAME_CHARS) return text
    val dot = text.lastIndexOf('.')
    val extension = if (dot > 0 && text.length - dot <= 16) text.substring(dot) else ""
    return text.substring(0, MAX_NAME_CHARS - extension.length) + extension
  }

  /**
   * A URI of Mewla's own providers (any build identity: `<package>.files`, a `0@` user prefix) is never taken from a
   * sender: the host would read it with Mewla's identity, not the sender's.
   */
  fun isOwnAuthority(authority: String?, packageName: String): Boolean {
    val bare = authority?.substringAfter('@')?.lowercase() ?: return false
    return bare == packageName.lowercase() || bare.startsWith("${packageName.lowercase()}.")
  }

  /** A path inside one of the app's own folders (canonical paths; `roots` too). */
  fun isPrivatePath(path: String, roots: List<String>): Boolean =
    roots.any { root -> root.isNotEmpty() && (path == root || path.startsWith(root.trimEnd('/') + "/")) }

  /** Whether `bytes` more still leave room to spare on a volume with `available` bytes. */
  fun fits(bytes: Long, available: Long, spare: Long = 64L * 1024 * 1024): Boolean = bytes + spare <= available
}
