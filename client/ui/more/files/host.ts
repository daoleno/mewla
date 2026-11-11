import { host, nativeComponent, platform } from "nsh";
import type { SessionFileDownloadHost } from "../../../core/files/sessionFilePreviewDownload";
import type { NativeUploadProgress, UploadDocumentAsset, UploadHost } from "../../../core/files/uploads";

/**
 * `files.*`, what only the phone can do with files: Android (client/android/…/files/)
 * and iOS (client/ios/Sources/Files/) answer the same calls.
 *
 * - `files.pick {maxCount}` → `UploadDocumentAsset[]`; `[]` when the picker was closed.
 *   The system picker (Android SAF ACTION_GET_CONTENT, iOS document picker); each `uri`
 *   is the host's own handle of the picked file (a content:// URI, an iOS token).
 * - `files.upload {uploadId, url, fileUri, expectedSize, headers}` → `{status, body}`:
 *   POSTs the file's bytes as they are read; `files.uploadProgress {uploadId, bytesSent,
 *   totalBytes}` at most every 200 ms (`totalBytes` -1 when unknown), the last one whole.
 * - `files.cancel {uploadId}` (a post): the upload rejects with "Attachment upload cancelled.".
 * - `files.pickDirectory` → `{token, name?}` or null when closed: the folder downloads go to.
 * - `files.download {downloadId, url, directory, names, mimeType, expectedSize, maxBytes, headers}`
 *   → `{bytesWritten, name}`: GETs `url` into the first of `names` the folder does not
 *   have, refuses more than `maxBytes` and any size but `expectedSize`, and deletes the file
 *   it made when it fails.
 * - `files.cancelDownload {downloadId}` (a post): rejects with "Session file download was cancelled.".
 * - `mewla.pdf` (below): a PDF's pages.
 *
 * The calls that wait on a person or a transfer have no timeout.
 *
 * In a browser the page host (client/ui/web/host.js) answers them: a file input, XHR, a
 * download into memory handed to the browser. The web player delivers no posts to the page,
 * so the two cancels are calls there.
 */

const WAIT = { timeout: 0 };

function cancel(name: string, data: Record<string, string>): void {
  if (platform.os === "web") void host.call(name, data).catch(() => {});
  else host.post(name, data);
}

export function pickFiles(maxCount: number): Promise<UploadDocumentAsset[]> {
  return host.call<UploadDocumentAsset[]>("files.pick", { maxCount }, WAIT).then((assets) => (Array.isArray(assets) ? assets : []));
}

export const uploadHost: UploadHost = {
  upload: (request) => host.call<{ status: number; body: string }>("files.upload", request, WAIT),
  // A post: a failure to stop is the host's to log; the upload's own answer still settles it.
  cancel: (uploadId) => cancel("files.cancel", { uploadId }),
  onProgress: (uploadId, listener) =>
    host.on<NativeUploadProgress & { uploadId: string }>("files.uploadProgress", (event) => {
      if (event?.uploadId === uploadId) listener({ bytesSent: event.bytesSent, totalBytes: event.totalBytes });
    }),
};

export const downloadHost: SessionFileDownloadHost = {
  pickDirectory: () => host.call<{ token: string } | null>("files.pickDirectory", null, WAIT).then((folder) => (folder?.token ? folder : null)),
  download: (request) => host.call<{ bytesWritten: number; name: string }>("files.download", request, WAIT),
};

export function cancelDownload(downloadId: string): void {
  cancel("files.cancelDownload", { downloadId });
}

/**
 * `mewla.pdf`: the pages of a PDF, scrolled (Android PdfRenderer, iOS PDFKit). `source` is
 * a local file URL or an http(s) stream URL the host fetches with `headers`, staged in its
 * cache and deleted with the view. `load {pageCount}` once it shows, `error {message}`.
 */
export const MewlaPdf = nativeComponent<
  { source: string; headers?: Record<string, string> },
  { load: { pageCount: number }; error: { message: string } }
>("mewla.pdf", { props: ["source", "headers"], events: ["load", "error"] });
