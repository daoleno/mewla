import type { SessionFileMetadata } from "./sessionFilePreview";

// The host (files.download) picks the folder, takes the first free name of
// sessionFileDownloadNameCandidates, streams into it and deletes only the file
// it created when the download fails. What stays here decides what to ask for.

export type SessionFileDownloadResult = "saved" | "started" | "cancelled";
export type SessionFileDownloadFeedback = "idle" | "busy" | "saved" | "started" | "failed";

export function sessionFileDownloadErrorMessage(error: unknown): string {
  const message =
    error instanceof Error
      ? error.message
      : typeof error === "string"
        ? error
        : "Unknown download error.";
  const normalized = message.trim();
  return normalized || "Unknown download error.";
}

export interface SessionFileDownloadLifecycleOwner {
  start(task: () => Promise<SessionFileDownloadResult>): Promise<SessionFileDownloadResult | undefined>;
  reset(): void;
  dispose(): void;
}

export function createSessionFileDownloadLifecycleOwner(input: {
  onFeedbackChange(
    feedback: SessionFileDownloadFeedback,
    error?: unknown,
  ): void;
}): SessionFileDownloadLifecycleOwner {
  let disposed = false;
  let active = false;
  let epoch = 0;

  return {
    async start(task) {
      if (disposed || active) return undefined;
      active = true;
      const taskEpoch = ++epoch;
      input.onFeedbackChange("busy");
      try {
        const result = await task();
        if (!disposed && epoch === taskEpoch) {
          input.onFeedbackChange(result === "cancelled" ? "idle" : result);
        }
        return result;
      } catch (error) {
        if (!disposed && epoch === taskEpoch) {
          input.onFeedbackChange("failed", error);
        }
        throw error;
      } finally {
        active = false;
      }
    },
    reset() {
      epoch += 1;
      if (!disposed) input.onFeedbackChange("idle");
    },
    dispose() {
      disposed = true;
      epoch += 1;
      active = false;
    },
  };
}

export function sessionFileDownloadFileName(
  metadata: Pick<SessionFileMetadata, "name" | "path">,
): string {
  const pathName = metadata.path.replace(/\\/g, "/").split("/").at(-1)?.trim() || "";
  const raw = metadata.name.trim() || pathName || "file";
  let sanitized = raw.replace(/[/\\]/g, "_").replace(/[\u0000-\u001f\u007f]/g, "").trim();
  const pathExtension = splitSessionFileDownloadName(sanitizeSessionFileName(pathName)).extension;
  const { stem, extension } = splitSessionFileDownloadName(sanitized);
  const cleanedStem = stripSessionFileImportNoise(stem);
  sanitized = `${cleanedStem || stem}${extension || pathExtension}`;
  if (
    !sanitized ||
    sanitized === "." ||
    sanitized === ".." ||
    /^\.+$/.test(sanitized)
  ) {
    sanitized = "file";
  }
  return sanitized.slice(0, 255) || "file";
}

const SESSION_FILE_SOURCE_SUFFIXES = [
  "airtable",
  "box",
  "confluence",
  "databricks",
  "dropbox",
  "github",
  "gitlab",
  "google drive",
  "google docs",
  "jira",
  "linear",
  "microsoft 365",
  "notion",
  "onedrive",
  "sharepoint",
  "slack",
].map((value) => value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"));

const SESSION_FILE_SOURCE_SUFFIX_RE = new RegExp(
  `(?:${SESSION_FILE_SOURCE_SUFFIXES.join("|")})(?:\\.com)?`,
  "i",
);
const SESSION_FILE_DOMAIN_SUFFIX_RE = /(?:https?:\/\/|www\.)?[a-z0-9-]+(?:\.[a-z0-9-]+)+/i;

function sanitizeSessionFileName(value: string): string {
  return value.replace(/[/\\]/g, "_").replace(/[\u0000-\u001f\u007f]/g, "").trim();
}

function stripSessionFileImportNoise(stem: string): string {
  let cleaned = stem.trim();
  let previous = "";
  while (cleaned && cleaned !== previous) {
    previous = cleaned;
    cleaned = cleaned
      .replace(/\s*(?:[-–—|·•]|\b(?:from|on)\b)\s*\([^)]*\)\s*$/i, "")
      .replace(
        new RegExp(
          `\\s*(?:[-–—|·•]|\\b(?:from|on)\\b)\\s*(?:${SESSION_FILE_SOURCE_SUFFIX_RE.source}|${SESSION_FILE_DOMAIN_SUFFIX_RE.source})\\s*$`,
          "i",
        ),
        "",
      )
      .replace(/\s*[([]\s*(?:imported|exported|downloaded|uploaded)\s*[)\]]\s*$/i, "")
      .replace(/\s*[-–—|·•]\s*(?:imported|exported|downloaded|uploaded|attachment|file)\s*$/i, "")
      .trim();
  }
  return cleaned;
}

/** Prefer metadata content-type without parameters; else octet-stream. */
export function sessionFileDownloadMimeType(
  contentType: string | null | undefined,
): string {
  const base = contentType?.split(";")[0]?.trim() || "";
  return base || "application/octet-stream";
}

/**
 * Any file a reply names can be downloaded, whatever its size: `tooLarge` and the preview limit
 * are about what the app renders, never about what it may save. The bytes stream to disk.
 */
export function sessionFileCanDownload(
  metadata: SessionFileMetadata | null | undefined,
): boolean {
  if (!metadata) return false;
  if (!metadata.path.trim() || !metadata.generation.trim()) return false;
  return Number.isFinite(metadata.size) && metadata.size >= 0;
}

/** Copy reads a file whole into the clipboard through the stream, so it keeps the old 50 MiB bound (a download does not). */
export const SESSION_FILE_COPY_LIMIT_BYTES = 50 * 1024 * 1024;

export function sessionFileCanCopyWhole(
  metadata: SessionFileMetadata | null | undefined,
): boolean {
  return sessionFileCanDownload(metadata) && metadata!.size <= SESSION_FILE_COPY_LIMIT_BYTES;
}

/**
 * The signed read as a download: the daemon answers it `attachment`, so a browser saves it as it
 * streams (progress, Cancel and the folder are the browser's) instead of showing it.
 */
export function sessionFileAttachmentUrl(url: string): string {
  return `${url}${url.includes("?") ? "&" : "?"}disposition=attachment`;
}

/** User/picker/abort cancellation only — never HTTP failure text. */
export function isSessionFileDownloadCancelError(error: unknown): boolean {
  const message =
    error instanceof Error
      ? error.message
      : typeof error === "string"
        ? error
        : "";
  return (
    /(?:picker|picking)\s+was\s+cancelled/i.test(message) ||
    /download was cancelled/i.test(message) ||
    /operation was aborted/i.test(message)
  );
}

export function splitSessionFileDownloadName(fileName: string): {
  stem: string;
  extension: string;
} {
  const trimmed = fileName.trim() || "file";
  const lastDot = trimmed.lastIndexOf(".");
  if (lastDot <= 0 || lastDot === trimmed.length - 1) {
    return { stem: trimmed, extension: "" };
  }
  return {
    stem: trimmed.slice(0, lastDot),
    extension: trimmed.slice(lastDot),
  };
}

export function sessionFileDownloadNameCandidates(
  preferredName: string,
  maxSuffix = 32,
): string[] {
  const names = [preferredName];
  const { stem, extension } = splitSessionFileDownloadName(preferredName);
  for (let index = 1; index <= maxSuffix; index += 1) {
    names.push(`${stem} (${index})${extension}`);
  }
  return names;
}

/**
 * The host side of a download (`files.pickDirectory`, `files.download`). A host that streams
 * to the chosen folder answers with the bytes written; one that hands the download to the
 * system's browser (the web, a desktop) answers `handedOff`, and the browser saves it.
 */
export interface SessionFileDownloadHost {
  /** The folder the person chose, as a token only the host can read; null when they closed the picker. */
  pickDirectory(): Promise<{ token: string } | null>;
  /** Streams `url` into the first free name of `names` in `directory`; deletes only its own file when it fails. */
  download(request: {
    downloadId: string;
    url: string;
    directory: string;
    names: string[];
    mimeType: string;
    expectedSize: number | null;
    maxBytes: number;
    headers: Record<string, string>;
  }): Promise<{ bytesWritten: number; name: string; handedOff?: boolean }>;
}

let downloadSequence = 0;

/**
 * Download: the folder first, then the read capability (it lives two minutes,
 * so it is signed after the picker closes), then the host streams the file.
 */
export async function downloadSessionFile(input: {
  host: SessionFileDownloadHost;
  metadata: SessionFileMetadata;
  sign(): Promise<{ uri: string; headers: Record<string, string> }>;
  onStart?(downloadId: string): void;
}): Promise<SessionFileDownloadResult> {
  if (!sessionFileCanDownload(input.metadata)) throw new Error("This file can't be downloaded.");
  const folder = await input.host.pickDirectory();
  if (!folder) return "cancelled";
  const source = await input.sign();
  downloadSequence += 1;
  const downloadId = `download-${Date.now().toString(36)}-${downloadSequence.toString(36)}`;
  input.onStart?.(downloadId);
  const saved = await input.host.download({
    downloadId,
    url: source.uri,
    directory: folder.token,
    names: sessionFileDownloadNameCandidates(sessionFileDownloadFileName(input.metadata)),
    mimeType: sessionFileDownloadMimeType(input.metadata.contentType),
    expectedSize: input.metadata.size,
    // No limit but the file itself: the host stops a stream that runs past the size it was inspected at.
    maxBytes: Math.max(1, input.metadata.size),
    headers: source.headers,
  });
  return saved?.handedOff ? "started" : "saved";
}
