import { platform } from "nsh";
import { readFile, stat } from "fs/promises";
import type { UploadDocumentAsset } from "../../../core/files/uploads";
import { fileTypeOf, localFilePath } from "../../../core/files/pastedFiles";
import { keepAppFiles } from "./appFiles";
import type { AttachmentUploads } from "./upload";

/**
 * Files put into a composer without its Attach button, which become chips as picked ones do:
 * - `files`: pictures pasted into the field (a screenshot, Copy Image; NSH's `onPaste` `files`), uploaded as the
 *   app's own (`appFiles.ts`);
 * - `uris`: local files dropped on the conversation or copied in Finder (`onFileDrop`, `onPaste` `uris`: `file:` URLs,
 *   the desktops only), read from disk here (nsh.json `fs.read`) and uploaded as the app's own, as Mewla for Mac's
 *   Attach does;
 * - `assets`: files dropped in a browser, which the page host keeps as it keeps picked ones (web/host.js
 *   `files.dropped`), uploaded by it with progress, as the browser's Attach does.
 */
export type IncomingFiles = { files?: readonly File[]; uris?: readonly string[]; assets?: readonly UploadDocumentAsset[] };

/** `fs/promises` reads at most this much of a file (NSH 0.6.1); a larger one is a failed chip that says so. */
const READ_MAX = 64 * 1024 * 1024;

/** Turns `incoming` into chips of `uploads`; rejects with what the person should read (not connected, too many). */
export async function attachIncoming(uploads: AttachmentUploads, incoming: IncomingFiles): Promise<void> {
  const assets = [...(incoming.assets ?? [])];
  if (incoming.files?.length) assets.push(...keepAppFiles(incoming.files));
  // A browser cannot read a file manager's paths: there they are only names.
  const uris = platform.os === "web" ? [] : (incoming.uris ?? []).filter((uri) => localFilePath(uri) !== null);
  if (!assets.length && !uris.length) return;
  // Not connected or no room: said before anything is read.
  uploads.checkRoom(assets.length + uris.length);
  const read = await readLocalFiles(uris);
  if (!assets.length && !read.length) throw new Error("Folders can't be attached.");
  uploads.addAssets([...assets, ...read]);
}

/** The local files `uris` name, read for upload; folders are left out, an unreadable file is a failed chip. */
export async function readLocalFiles(uris: readonly string[]): Promise<UploadDocumentAsset[]> {
  const assets: UploadDocumentAsset[] = [];
  for (const uri of uris) {
    const path = localFilePath(uri);
    if (!path) continue;
    const name = path.split("/").filter(Boolean).at(-1) ?? "file";
    const type = fileTypeOf(name);
    const failed = (selectionError: string, size: number | null = null): UploadDocumentAsset =>
      ({ uri: `app-file:unread:${path}`, name, mimeType: type || null, size, selectionError, selectionRetryable: false });
    try {
      const info = await stat(path);
      if (info.isDirectory()) continue;
      if (info.size > READ_MAX) {
        assets.push(failed("This file is larger than 64 MB.", info.size));
        continue;
      }
      const bytes = await readFile(path);
      const [kept] = keepAppFiles([new File([bytes], name, { type })]);
      assets.push({ ...kept!, name });
    } catch {
      assets.push(failed("Mewla can't read this file."));
    }
  }
  return assets;
}
