import { serverURLNow } from "../link/pinnedTransport";
import { deviceAuthorization } from "../more/daemon";
import type { StoredServer } from "../storedServerContract";

/**
 * Attachments: files picked on the phone and streamed by the host to the current
 * computer's `/upload` door (POST, raw bytes, the name in `X-Mewla-Upload-Name`).
 * JS never holds a file's bytes; it signs the request, watches the progress and
 * reads the computer's answer.
 */

export type UploadedAttachment = {
  name: string;
  /** Where the computer keeps it (7 days). */
  path: string;
  /** The phone's own URI, for thumbnails. */
  localUri?: string;
  mimeType?: string;
};

/** One picked file, as the host's picker reports it. */
export type UploadDocumentAsset = {
  uri: string;
  name: string;
  mimeType?: string | null;
  size?: number | null;
  /** The provider would not give this file; shown instead of uploading. */
  selectionError?: string;
  /** False when picking again cannot help (a folder, say). */
  selectionRetryable?: boolean;
};

export type UploadProgressSnapshot = {
  transferredBytes: number | null;
  totalBytes: number | null;
  fraction: number | null;
  bytesPerSecond?: number;
  etaSeconds?: number;
};

export type ActiveAttachmentUpload = {
  name: string;
  progress: UploadProgressSnapshot | null;
};

/** What the host reports while it streams: `totalBytes` is -1 when unknown. */
export type NativeUploadProgress = { bytesSent: number; totalBytes: number };

/** The host side of an upload (`files.upload`, `files.cancel`, `files.uploadProgress`). */
export interface UploadHost {
  upload(request: { uploadId: string; url: string; fileUri: string; expectedSize: number | null; headers: Record<string, string> }): Promise<{ status: number; body: string }>;
  /** Stops the upload; throws only when stopping itself failed. */
  cancel(uploadId: string): void;
  onProgress(uploadId: string, listener: (progress: NativeUploadProgress) => void): () => void;
}

export interface AttachmentUploadOperation {
  readonly result: Promise<UploadedAttachment>;
  /** Returns a genuine cancellation failure, if cancelling itself fails. */
  cancel(): Error | null;
}

export interface AttachmentUploadOperationOptions {
  onProgress?(progress: UploadProgressSnapshot): void;
  now?(): number;
}

export class AttachmentUploadCancelledError extends Error {
  constructor() {
    super("Attachment upload cancelled.");
    this.name = "AttachmentUploadCancelledError";
  }
}

// Mirrors the daemon's explicit V1 file limit to avoid starting a transfer the
// server cannot accept. The daemon still enforces its policy authoritatively.
export const V1_MAX_UPLOAD_FILE_BYTES = 2 * 1024 * 1024 * 1024;
export const UPLOAD_NAME_HEADER = "X-Mewla-Upload-Name";
const MAX_UPLOAD_NAME_BYTES = 1024;
const MAX_UPLOAD_NAME_HEADER_BYTES = 3 * 1024;
export const MAX_COMPOSER_ATTACHMENTS = 8;

export function buildUploadUrl(serverUrl: string): string | null {
  if (!serverUrl) return null;
  try {
    const url = new URL(serverUrl);
    if (url.protocol === "ws:") url.protocol = "http:";
    else if (url.protocol === "wss:") url.protocol = "https:";
    if (url.protocol !== "http:" && url.protocol !== "https:") return null;
    url.pathname = "/upload";
    url.search = "";
    url.hash = "";
    return url.toString();
  } catch {
    return null;
  }
}

export async function buildUploadHeaders(server: StoredServer, asset: UploadDocumentAsset): Promise<Record<string, string>> {
  return {
    Authorization: await deviceAuthorization(server, "mewla-upload"),
    "Content-Type": asset.mimeType || "application/octet-stream",
    [UPLOAD_NAME_HEADER]: encodeUploadName(asset.name || "upload"),
  };
}

let uploadSequence = 0;

export function createAttachmentUploadOperation(
  asset: UploadDocumentAsset,
  server: StoredServer,
  host: UploadHost,
  options: AttachmentUploadOperationOptions = {},
): AttachmentUploadOperation {
  const uploadUrl = buildUploadUrl(serverURLNow(server));
  if (!uploadUrl) throw new Error("Server URL is not configured.");
  if (typeof asset.size === "number" && asset.size > V1_MAX_UPLOAD_FILE_BYTES) throw new Error("File exceeds the 2 GiB upload limit.");
  const originalName = asset.name || "upload";
  // Checked before anything is signed or sent.
  encodeUploadName(originalName);
  uploadSequence += 1;
  const uploadId = `attachment-${Date.now().toString(36)}-${uploadSequence.toString(36)}`;
  let started = false;
  let cancelRequested = false;
  let settled = false;
  let cancelFailure: Error | null = null;
  let projected: UploadProgressSnapshot | null = null;
  let startedAt = 0;
  let stopProgress: (() => void) | null = null;

  const result = (async (): Promise<UploadedAttachment> => {
    try {
      const headers = await buildUploadHeaders(server, asset);
      if (cancelRequested) throw cancelFailure ?? new AttachmentUploadCancelledError();
      stopProgress = host.onProgress(uploadId, (progress) => {
        if (cancelRequested || settled) return;
        projected = projectUploadProgress(projected, progress);
        projected = projectUploadTiming(projected, (options.now?.() ?? Date.now()) - startedAt);
        options.onProgress?.(projected);
      });
      startedAt = options.now?.() ?? Date.now();
      started = true;
      const answer = await host.upload({
        uploadId,
        url: uploadUrl,
        fileUri: asset.uri,
        expectedSize: typeof asset.size === "number" && Number.isFinite(asset.size) ? asset.size : null,
        headers,
      });
      if (cancelRequested) throw cancelFailure ?? new AttachmentUploadCancelledError();
      if (answer.status < 200 || answer.status >= 300) throw new Error(answer.body.trim() || `Upload failed (${answer.status})`);
      let payload: { path?: string; name?: string };
      try {
        payload = JSON.parse(answer.body) as { path?: string; name?: string };
      } catch {
        throw new Error("Upload returned an invalid response.");
      }
      if (!payload.path) throw new Error("Upload response missing file path.");
      return { name: payload.name || originalName, path: payload.path, localUri: asset.uri, mimeType: asset.mimeType || undefined };
    } catch (error) {
      if (cancelRequested && !cancelFailure) throw new AttachmentUploadCancelledError();
      throw error;
    } finally {
      settled = true;
      stopProgress?.();
    }
  })();

  return {
    result,
    cancel() {
      if (settled || cancelRequested) return null;
      cancelRequested = true;
      if (!started) return null;
      try {
        host.cancel(uploadId);
        return null;
      } catch (error) {
        cancelFailure = error instanceof Error ? error : new Error("Could not cancel this upload.");
        return cancelFailure;
      }
    },
  };
}

export function projectUploadProgress(previous: UploadProgressSnapshot | null, nativeProgress: NativeUploadProgress): UploadProgressSnapshot {
  const transferredBytes = maxKnownByteCount(previous?.transferredBytes ?? null, normalizeByteCount(nativeProgress.bytesSent));
  const totalBytes = normalizePositiveByteCount(nativeProgress.totalBytes) ?? previous?.totalBytes ?? null;
  const nativeFraction = transferredBytes !== null && totalBytes !== null ? clampFraction(transferredBytes / totalBytes) : null;
  const fraction = nativeFraction === null ? null : Math.max(previous?.fraction ?? 0, nativeFraction);
  return { transferredBytes, totalBytes, fraction };
}

function projectUploadTiming(progress: UploadProgressSnapshot, elapsedMilliseconds: number): UploadProgressSnapshot {
  if (elapsedMilliseconds < 250 || progress.transferredBytes === null || progress.transferredBytes <= 0) return progress;
  const bytesPerSecond = Math.round(progress.transferredBytes / (elapsedMilliseconds / 1000));
  if (!Number.isFinite(bytesPerSecond) || bytesPerSecond <= 0) return progress;
  if (progress.totalBytes === null) return { ...progress, bytesPerSecond };
  const remainingBytes = Math.max(0, progress.totalBytes - progress.transferredBytes);
  return { ...progress, bytesPerSecond, etaSeconds: Math.ceil(remainingBytes / bytesPerSecond) };
}

/** The picker's answer, refused when it holds more than room is left for. */
export function checkPickedCount(assets: UploadDocumentAsset[], maxCount: number): UploadDocumentAsset[] {
  if (assets.length > maxCount) throw new Error(`Choose up to ${maxCount} files. Remove an attachment to make room.`);
  return assets;
}

export function pickLimit(maxCount: number): number {
  if (maxCount < 1 || maxCount > MAX_COMPOSER_ATTACHMENTS) throw new Error("Remove an attachment before selecting more files.");
  return maxCount;
}

function encodeUploadName(name: string): string {
  let encoded: string;
  try {
    encoded = encodeURIComponent(name);
  } catch {
    throw new Error("File name cannot be uploaded.");
  }
  if (!encoded || encoded.length > MAX_UPLOAD_NAME_HEADER_BYTES || encodedByteLength(encoded) > MAX_UPLOAD_NAME_BYTES) throw new Error("File name is too long.");
  return encoded;
}

function encodedByteLength(encoded: string): number {
  let length = 0;
  for (let index = 0; index < encoded.length; index += 1) {
    length += 1;
    if (encoded[index] === "%") index += 2;
  }
  return length;
}

function normalizeByteCount(value: number): number | null {
  return Number.isFinite(value) && value >= 0 ? Math.floor(value) : null;
}

function normalizePositiveByteCount(value: number): number | null {
  return Number.isFinite(value) && value > 0 ? Math.floor(value) : null;
}

function maxKnownByteCount(previous: number | null, next: number | null): number | null {
  if (previous === null) return next;
  if (next === null) return previous;
  return Math.max(previous, next);
}

function clampFraction(value: number) {
  return Math.max(0, Math.min(1, value));
}
