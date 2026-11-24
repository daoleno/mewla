import { formatSessionFileSize, type SessionFileMetadata, type SessionFileTextPreview } from "./sessionFilePreview";
import type { SessionFileDownloadFeedback } from "./sessionFilePreviewDownload";

/** The words of a file preview's header and states. */

export function sessionFileKindLabel(metadata: Pick<SessionFileMetadata, "kind" | "contentType" | "name">): string {
  switch (metadata.kind) {
    case "markdown": return "Markdown";
    case "text":
      if (isHtmlSessionFile(metadata)) return "HTML";
      if (metadata.contentType.includes("json")) return "JSON";
      if (metadata.contentType.includes("yaml")) return "YAML";
      return metadata.name.toLowerCase().endsWith(".log") ? "Log" : "Text";
    case "image": return "Image";
    case "pdf": return "PDF";
    case "video": return "Video";
    case "audio": return "Audio";
    default: return "Binary";
  }
}

/** An HTML file, which the preview draws as a page (`FilePreview`'s HtmlBody). */
export function isHtmlSessionFile(metadata: Pick<SessionFileMetadata, "kind" | "contentType" | "name">): boolean {
  return metadata.kind === "text" && (/^text\/html\b/i.test(metadata.contentType) || /\.html?$/i.test(metadata.name));
}

export function sessionFileTitle(metadata: SessionFileMetadata | null, reference: string): string {
  return metadata?.name || reference.replace(/\\/g, "/").split("/").filter(Boolean).at(-1) || "File";
}

/** `relativePath · Kind · size` (the whole path for a file outside the Session's folder), or the reference while it opens. */
export function sessionFileMetaLine(metadata: SessionFileMetadata | null, reference: string): string {
  if (!metadata) return reference || "Current Session";
  const where = /^\.\.(?:[\\/]|$)/.test(metadata.relativePath) ? metadata.path : metadata.relativePath;
  return [where, sessionFileKindLabel(metadata), formatSessionFileSize(metadata.size)].filter(Boolean).join(" · ");
}

export function sessionFileProblemTitle(input: { stale: boolean; tooLarge: boolean; gone?: boolean }): string {
  // Deleted or moved since the Session wrote it (a worktree or scratch folder collected): said as such.
  if (input.gone) return "File is gone";
  if (input.stale) return "File changed";
  return input.tooLarge ? "File too large" : "Could not open file";
}

export function sessionFileTruncationNote(text: SessionFileTextPreview | null): string | null {
  return text?.truncated ? `Preview limited to the first ${formatSessionFileSize(text.bytesRead)}.` : null;
}

export function sessionFileDownloadStatus(feedback: SessionFileDownloadFeedback, error: string): string | null {
  if (feedback === "saved") return "Saved";
  return feedback === "failed" ? `Download failed: ${error || "Unknown error"}` : null;
}

export function sessionFileDownloadLabel(input: { busy: boolean; can: boolean }): string {
  if (input.busy) return "Downloading file";
  return input.can ? "Download file" : "Download unavailable";
}

export const SESSION_FILE_NO_IDENTITY = "This Session has no live generation identity. Refresh the Session list and try again.";
