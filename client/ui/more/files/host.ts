import { host, nativeComponent, platform } from "nsh";
import type { SessionFileDownloadHost } from "../../../core/files/sessionFilePreviewDownload";
import type { NativeUploadProgress, UploadDocumentAsset, UploadHost } from "../../../core/files/uploads";
import { appFileImageUrl, appFileUploadHost, isAppFileUri } from "./appFiles";

/**
 * `files.*`, what only the host can do with files: Android (client/android/…/files/),
 * iOS (client/ios/Sources/Files/) and the browser (client/ui/web/host.js) answer the same
 * calls. The browser has no folder picker (`files.pickDirectory` answers at once and the
 * browser saves the download where it saves downloads) and no `mewla.pdf`.
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
 * - `files.thumbnail {uri, maxPixels}` → `{dataUrl}` or null: a picked picture, scaled (below).
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

const hostUploads: UploadHost = {
  upload: (request) => host.call<{ status: number; body: string }>("files.upload", request, WAIT),
  // A post: a failure to stop is the host's to log; the upload's own answer still settles it.
  cancel: (uploadId) => cancel("files.cancel", { uploadId }),
  onProgress: (uploadId, listener) =>
    host.on<NativeUploadProgress & { uploadId: string }>("files.uploadProgress", (event) => {
      if (event?.uploadId === uploadId) listener({ bytesSent: event.bytesSent, totalBytes: event.totalBytes });
    }),
};

const appUploads = new Set<string>();

/** Picked files go through the host; pasted or dropped ones (`keepAppFiles`) through the app's own fetch. */
export const uploadHost: UploadHost = {
  upload: (request) => {
    if (!isAppFileUri(request.fileUri)) return hostUploads.upload(request);
    appUploads.add(request.uploadId);
    return appFileUploadHost.upload(request).finally(() => appUploads.delete(request.uploadId));
  },
  cancel: (uploadId) => (appUploads.has(uploadId) ? appFileUploadHost : hostUploads).cancel(uploadId),
  onProgress: (uploadId, listener) => {
    const stops = [appFileUploadHost.onProgress(uploadId, listener), hostUploads.onProgress(uploadId, listener)];
    return () => stops.forEach((stop) => stop());
  },
};

export const downloadHost: SessionFileDownloadHost = {
  pickDirectory: () => host.call<{ token: string } | null>("files.pickDirectory", null, WAIT).then((folder) => (folder?.token ? folder : null)),
  download: (request) => host.call<{ bytesWritten: number; name: string }>("files.download", request, WAIT),
};

const thumbnails = new Map<string, Promise<string | null>>();

/**
 * `files.thumbnail {uri, maxPixels}` → `{dataUrl}` or null: a picked picture (`files.pick`'s
 * `uri`, the host's handle of it), scaled so its longer side is at most `maxPixels`, as a
 * JPEG data URL; null when it is not a picture the host can decode. Asked once per picture
 * and size while the app runs.
 */
export function thumbnail(uri: string, maxPixels: number): Promise<string | null> {
  // A pasted picture is the app's own: shown from its object URL, at any size.
  if (isAppFileUri(uri)) return Promise.resolve(appFileImageUrl(uri));
  const key = `${maxPixels}\u0000${uri}`;
  let pending = thumbnails.get(key);
  if (!pending) {
    pending = host
      .call<{ dataUrl?: string } | null>("files.thumbnail", { uri, maxPixels })
      .then((answer) => (typeof answer?.dataUrl === "string" && answer.dataUrl.startsWith("data:image/") ? answer.dataUrl : null));
    pending.catch(() => thumbnails.delete(key));
    thumbnails.set(key, pending);
  }
  return pending;
}

export function cancelDownload(downloadId: string): void {
  cancel("files.cancelDownload", { downloadId });
}

/**
 * `mewla.pdf`: the pages of a PDF, scrolled (Android PdfRenderer, iOS PDFKit). `source` is
 * a local file URL or an http(s) stream URL the host fetches with `headers`, staged in its
 * cache and deleted with the view. `expectedBytes` (when > 0) is the size the file was
 * inspected at: a stream that runs past it errors with "The PDF stream exceeded its inspected
 * file size.", one that ends short or long of it with "The PDF changed while it was being
 * downloaded." (the preview reads that as the file having changed). `load {pageCount}` once
 * it shows, `error {message}`.
 */
export const MewlaPdf = nativeComponent<
  { source: string; headers?: Record<string, string>; expectedBytes?: number },
  { load: { pageCount: number }; error: { message: string } }
>("mewla.pdf", { props: ["source", "headers", "expectedBytes"], events: ["load", "error"] });
