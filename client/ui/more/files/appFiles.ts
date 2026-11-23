import { platform } from "nsh";
import { pastedFileName } from "../../../core/files/pastedFiles";
import type { NativeUploadProgress, UploadDocumentAsset, UploadHost } from "../../../core/files/uploads";

/**
 * Files the app itself holds: what a person pastes or drops into a field (`onPaste`'s
 * `File`s) on any platform, and the photos the phone's photo picker gives (`pickPhotos`).
 * The host never saw them, so they are kept here under an `app-file:<n>` uri and uploaded
 * with `fetch`. On a phone the body is the file read as a stream, counted as it is sent, so
 * its chip shows progress; elsewhere it is the File itself (a browser streams a body only
 * over HTTP/2), which goes from 0 to whole when the computer answers.
 */

const KEPT = 64;
const files = new Map<string, File>();
let sequence = 0;

const PREFIX = "app-file:";

export function isAppFileUri(uri: string): boolean {
  return uri.startsWith(PREFIX);
}

/** Keeps pasted or dropped files for upload; a generic clipboard name ("image.png") gets the paste's time. */
export function keepAppFiles(list: readonly File[], now: Date = new Date()): UploadDocumentAsset[] {
  return list.map((file) => {
    sequence += 1;
    const uri = `${PREFIX}${sequence}`;
    files.set(uri, file);
    while (files.size > KEPT) releaseAppFiles([files.keys().next().value!]);
    return { uri, name: pastedFileName(file.name, file.type, now), mimeType: file.type || null, size: file.size };
  });
}

const urls = new Map<string, string>();

/** A kept picture's own URL, for a chip or the viewer; null for a file that is not one or was dropped. */
export function appFileImageUrl(uri: string): string | null {
  const file = files.get(uri);
  if (!file || !file.type.startsWith("image/")) return null;
  let url = urls.get(uri);
  if (!url) urls.set(uri, (url = URL.createObjectURL(file)));
  return url;
}

/**
 * The chips of `uris` went (sent, removed, the composer gone): their kept files go too, with their pictures' URLs,
 * so a large dropped file is not held after it went (a drop or a paste can bring 512 MB).
 */
export function releaseAppFiles(uris: readonly string[]): void {
  for (const uri of uris) {
    files.delete(uri);
    const url = urls.get(uri);
    if (url) URL.revokeObjectURL(url);
    urls.delete(uri);
  }
}

/** NSH's fetch on a phone sends a stream as it reads it. */
const STREAMS = platform.os === "ios" || platform.os === "android";

/** `file`'s bytes as a stream that reports how many have been read for sending. */
function counted(file: File, report: (bytesSent: number) => void): ReadableStream<Uint8Array> {
  const reader = file.stream().getReader();
  let sent = 0;
  return new ReadableStream<Uint8Array>({
    async pull(controller) {
      const { done, value } = await reader.read();
      if (done) return controller.close();
      sent += value.byteLength;
      report(sent);
      controller.enqueue(value);
    },
    cancel: (reason) => reader.cancel(reason),
  });
}

const running = new Map<string, AbortController>();
const listeners = new Map<string, Set<(progress: NativeUploadProgress) => void>>();

export const appFileUploadHost: UploadHost = {
  async upload(request) {
    const file = files.get(request.fileUri);
    if (!file) throw new Error("Choose the file again to upload it.");
    const controller = new AbortController();
    running.set(request.uploadId, controller);
    const report = (bytesSent: number) => listeners.get(request.uploadId)?.forEach((listener) => listener({ bytesSent, totalBytes: file.size }));
    try {
      report(0);
      const sending = STREAMS ? counted(file, report) : file;
      const response = await fetch(request.url, { method: "POST", headers: request.headers, body: sending, signal: controller.signal, ...(STREAMS ? { duplex: "half" as const } : {}) });
      const body = await response.text();
      report(file.size);
      return { status: response.status, body };
    } catch (error) {
      if (controller.signal.aborted) throw new Error("Attachment upload cancelled.");
      throw error instanceof Error && error.message ? error : new Error("Upload could not reach Mewla. Check the connection and retry.");
    } finally {
      running.delete(request.uploadId);
    }
  },
  cancel: (uploadId) => running.get(uploadId)?.abort(),
  onProgress(uploadId, listener) {
    const set = listeners.get(uploadId) ?? new Set();
    set.add(listener);
    listeners.set(uploadId, set);
    return () => {
      set.delete(listener);
      if (!set.size) listeners.delete(uploadId);
    };
  },
};
