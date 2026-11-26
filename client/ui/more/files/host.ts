import { host, nativeComponent, openUrl, Picker, platform } from "nsh";
import { sessionFileAttachmentUrl, type SessionFileDownloadHost } from "../../../core/files/sessionFilePreviewDownload";
import type { NativeUploadProgress, UploadDocumentAsset, UploadHost } from "../../../core/files/uploads";
import { appFileImageUrl, appFileUploadHost, isAppFileUri, keepAppFiles } from "./appFiles";
import { pickedPhotoName } from "../../../core/files/attachSources";

/**
 * `files.*`, what only the host can do with files: Android (client/android/…/files/),
 * iOS (client/ios/Sources/Files/) and the browser (client/ui/web/host.js) answer the same
 * calls. The browser has no folder picker (`files.pickDirectory` answers at once and the
 * browser saves the download where it saves downloads) and no `mewla.pdf`.
 *
 * - `files.pick {maxCount}` → `UploadDocumentAsset[]`; `[]` when the picker was closed.
 *   The system picker (Android SAF ACTION_GET_CONTENT, iOS document picker); each `uri`
 *   is the host's own handle of the picked file (a content:// URI, an iOS token).
 * - `files.canCapture` → boolean: this host takes photos (`files.capture`) and the device has a camera. A host
 *   from before it answers nothing (the call rejects): no Camera.
 * - `files.capture` → `UploadDocumentAsset` or null when the person closed the camera: a photo taken now
 *   (Android ACTION_IMAGE_CAPTURE after the system's camera prompt, iOS UIImagePickerController), kept by the
 *   host as a JPEG in its cache, its `uri` streamed by `files.upload` like a picked file. Rejects with what to
 *   read when the camera was refused or cannot open.
 * - `files.upload {uploadId, url, fileUri, expectedSize, headers}` → `{status, body}`:
 *   POSTs the file's bytes as they are read; `files.uploadProgress {uploadId, bytesSent,
 *   totalBytes}` at most every 200 ms (`totalBytes` -1 when unknown), the last one whole.
 * - `files.cancel {uploadId}` (a post): the upload rejects with "Attachment upload cancelled.".
 * - `files.pickDirectory` → `{token, name?}` or null when closed: the folder downloads go to.
 * - `files.download {downloadId, url, directory, names, mimeType, expectedSize, maxBytes, headers}`
 *   → `{bytesWritten, name}`: GETs `url` into the first of `names` the folder does not
 *   have, a chunk at a time (a file of any size, none held whole), refuses more than `maxBytes`
 *   (the size the file was inspected at) and any size but `expectedSize`, and deletes the file
 *   it made when it fails. The browser's answers `{…, handedOff: true}`: its own download took it.
 * - `files.cancelDownload {downloadId}` (a post): rejects with "Session file download was cancelled.".
 * - `files.thumbnail {uri, maxPixels}` → `{dataUrl}` or null: a picked picture, scaled (below).
 * - `mewla.pdf` (below): a PDF's pages.
 * - `share.*` (Android only; `shareIn.ts`): what other apps share or open with Mewla, copied by the host, handed to
 *   Brain's composer as chips whose `uri` is a file:// URI of the copy that `files.upload` streams like a picked one.
 *   `share.take`, `share.ack {ids}`, `share.release {uris}` and the `share.arrived` event; see `shareIn.ts`.
 *
 * The calls that wait on a person or a transfer have no timeout.
 *
 * In a browser the page host (client/ui/web/host.js) answers them: a file input, XHR, a
 * download the browser itself saves (a link to the signed read as an attachment). The web player delivers no posts to the page,
 * so the two cancels are calls there.
 */

const WAIT = { timeout: 0 };

/**
 * Mewla for Mac's host answers no `files.*` (an unanswered `host.call` rejects): it picks with NSH's own picker
 * (the open panel, the files' bytes read), and the files upload as the app's own (`appFiles.ts`), as pasted ones do.
 */
const APP_PICKER = platform.os === "macos";

/** NSH's picker reads at most this much of a file; a larger one comes back unread. */
const APP_PICK_MAX = 512 * 1024 * 1024;

/** A file NSH hands over with its contents: a pick's (`Picker.pickFiles`), and on the Mac a drop's (`onFileDrop` `files`). */
export type PickedFileEntry = { file: File | null; name: string; type: string; size: number };

/**
 * Picked or dropped files as chips, uploaded as the app's own. One unread (larger than a pick or a drop reads, 512 MB)
 * is a failed chip that says so, never a retry. Each keeps its own name (a paste's generic "image.png" gets its time;
 * a picked or dropped one is named).
 */
export function pickedAssets(picked: readonly PickedFileEntry[]): UploadDocumentAsset[] {
  const kept = keepAppFiles(picked.flatMap((pick) => (pick.file ? [pick.file] : [])));
  return picked.map((pick) => (pick.file ? { ...kept.shift()!, name: pick.name } : {
    uri: `app-file:unread:${pick.name}`, name: pick.name, mimeType: pick.type, size: pick.size,
    selectionError: "This file is larger than 512 MB.", selectionRetryable: false,
  }));
}

async function pickAppFiles(maxCount: number): Promise<UploadDocumentAsset[]> {
  return pickedAssets(await Picker.pickFiles({ multiple: maxCount > 1, limit: Math.max(1, maxCount), maxSize: APP_PICK_MAX }));
}

function cancel(name: string, data: Record<string, string>): void {
  if (platform.os === "web") void host.call(name, data).catch(() => {});
  else host.post(name, data);
}

/**
 * Photos and screenshots from the system's photo picker (NSH's `Picker.pickImages`: Android's Photo Picker, iOS's
 * PHPicker; no permission), up to `maxCount`, as the app's own files. HEIC and other formats an agent cannot read
 * come as JPEG; sizes are kept.
 */
export async function pickPhotos(maxCount: number): Promise<UploadDocumentAsset[]> {
  const picked = await Picker.pickImages({ multiple: maxCount > 1, limit: Math.max(1, maxCount), format: "compatible" });
  const now = new Date();
  return keepAppFiles(picked.map((pick) => pick.file), now).map((asset, index) => ({ ...asset, name: pickedPhotoName(asset.name, asset.mimeType ?? "", now, index) }));
}

let capture: Promise<boolean> | null = null;

/** Whether "+" offers Camera: asked of the host once per run (a host from before `files.capture` does not answer). */
export function canCapture(): Promise<boolean> {
  if (platform.os !== "ios" && platform.os !== "android") return Promise.resolve(false);
  capture ??= host.call<boolean>("files.canCapture", null).then((answer) => answer === true, () => false);
  return capture;
}

/** A photo taken now (`files.capture`), as a picked file; `[]` when the camera was closed. */
export async function capturePhoto(): Promise<UploadDocumentAsset[]> {
  const asset = await host.call<UploadDocumentAsset | null>("files.capture", null, WAIT);
  return asset && typeof asset.uri === "string" ? [asset] : [];
}

export function pickFiles(maxCount: number): Promise<UploadDocumentAsset[]> {
  if (APP_PICKER) return pickAppFiles(maxCount);
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

/** The phones stream a download into the folder the person chose (Android's, iOS's host: no whole file is held). */
const nativeDownloads: SessionFileDownloadHost = {
  pickDirectory: () => host.call<{ token: string } | null>("files.pickDirectory", null, WAIT).then((folder) => (folder?.token ? folder : null)),
  download: (request) => host.call<{ bytesWritten: number; name: string; handedOff?: boolean }>("files.download", request, WAIT),
};

/**
 * A desktop's host (Mac, Linux, Windows) answers no `files.*` and the app may only read files: the signed read, as
 * an attachment, goes to the system's browser, which saves it to its Downloads folder as it streams, whatever its size.
 */
const browserDownloads: SessionFileDownloadHost = {
  pickDirectory: async () => ({ token: "browser-downloads" }),
  download: async (request) => {
    const result = await openUrl(sessionFileAttachmentUrl(request.url));
    if (!result.opened) throw new Error("The browser blocked the download. Allow pop-ups for Mewla and try again.");
    return { bytesWritten: request.expectedSize ?? 0, name: request.names[0] ?? "download", handedOff: true };
  },
};

export const downloadHost: SessionFileDownloadHost = platform.os === "android" || platform.os === "ios" || platform.os === "web" ? nativeDownloads : browserDownloads;

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
