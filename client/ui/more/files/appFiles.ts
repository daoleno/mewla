import { pastedFileName } from "../../../core/files/pastedFiles";
import type { NativeUploadProgress, UploadDocumentAsset, UploadHost } from "../../../core/files/uploads";

/**
 * Files the app itself holds: what a person pastes or drops into a field (`onPaste`'s
 * `File`s) on any platform. The host never saw them, so they are kept here under an
 * `app-file:<n>` uri and uploaded with `fetch`, the File as the body. `fetch` does not
 * report upload progress, so such a file goes from 0 to whole when the computer answers.
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
    while (files.size > KEPT) files.delete(files.keys().next().value!);
    return { uri, name: pastedFileName(file.name, file.type, now), mimeType: file.type || null, size: file.size };
  });
}

/** A kept picture's own URL, for a chip or the viewer; null for a file that is not one or was dropped. */
export function appFileImageUrl(uri: string): string | null {
  const file = files.get(uri);
  if (!file || !file.type.startsWith("image/")) return null;
  return URL.createObjectURL(file);
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
      const response = await fetch(request.url, { method: "POST", headers: request.headers, body: file, signal: controller.signal });
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
