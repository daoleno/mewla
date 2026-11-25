import { platform } from "nsh";
import { readFile, stat } from "fs/promises";
import type { UploadDocumentAsset } from "../../../core/files/uploads";
import { fileTypeOf, localFileContents, localFileName, localFilePath } from "../../../core/files/pastedFiles";
import { keepAppFiles } from "./appFiles";
import { pickedAssets, type PickedFileEntry } from "./host";
import type { AttachmentUploads } from "./upload";

/**
 * Files put into a composer without its Attach button, which become chips as picked ones do:
 * - `files`: files pasted into the field (NSH's `onPaste` `files`: a screenshot, Copy Image; with an NSH that hands
 *   them over, the contents of files copied in Finder), uploaded as the app's own (`appFiles.ts`);
 * - `dropped`: files dropped on the conversation with their contents (an NSH whose `onFileDrop` has `files`, the
 *   shape of a pick), uploaded the same way, one past what a drop reads a failed chip that says so;
 * - `uris`: the local files a drop or a Finder copy named (`file:` URLs, the desktops), matched with the contents
 *   that came (`localFileContents`). NSH 0.7 hands over a local file's URL only, so those that came without contents are read from
 *   disk here (nsh.json `fs.read`), as Mewla for Mac's Attach does; one that cannot be read is a failed chip;
 * - `assets`: files dropped in a browser, which the page host keeps as it keeps picked ones (web/host.js
 *   `files.dropped`), uploaded by it with progress, as the browser's Attach does.
 */
export type IncomingFiles = { files?: readonly File[]; dropped?: readonly PickedFileEntry[]; uris?: readonly string[]; assets?: readonly UploadDocumentAsset[] };

/** Turns `incoming` into chips of `uploads`; rejects with what the person should read (not connected, too many, nothing). */
export async function attachIncoming(uploads: AttachmentUploads, incoming: IncomingFiles): Promise<void> {
  // A browser cannot read a file manager's paths: there they are only names.
  const uris = platform.os === "web" ? [] : incoming.uris ?? [];
  const files = incoming.files ?? [];
  const dropped = incoming.dropped ?? [];
  const sorted = localFileContents([...files.map((file) => file.name), ...dropped.map((entry) => entry.name)], uris);
  const pasted = files.filter((_, index) => sorted.keep[index]);
  // A Finder file keeps its own name ("image.png" too); a clipboard picture's generic one gets its time.
  const kept = keepAppFiles(pasted).map((asset, index) => (uris.length ? { ...asset, name: pasted[index]!.name } : asset));
  const assets = [
    ...(incoming.assets ?? []),
    ...kept,
    ...pickedAssets(dropped.filter((_, index) => sorted.keep[files.length + index])),
  ];
  if (!assets.length && !sorted.unread.length) throw new Error(sorted.folders ? "Folders can't be attached." : "The dropped file didn't arrive.");
  // Not connected or no room: said before anything is read.
  uploads.checkRoom(assets.length + sorted.unread.length);
  const read = await readLocalFiles(sorted.unread.map(({ uri }) => uri));
  if (!assets.length && !read.length) throw new Error("Folders can't be attached.");
  uploads.addAssets([...assets, ...read]);
}

/** `fs/promises` reads at most this much of a file (NSH 0.7); a larger one is a failed chip that says so. */
const READ_MAX = 64 * 1024 * 1024;

/** The local files `uris` name, read for upload; folders are left out, an unreadable file is a failed chip. */
export async function readLocalFiles(uris: readonly string[]): Promise<UploadDocumentAsset[]> {
  const assets: UploadDocumentAsset[] = [];
  for (const uri of uris) {
    const path = localFilePath(uri);
    if (!path) continue;
    // A file reference the Mac could not resolve (`file:///.file/id=…`) has no name of its own: "File".
    const name = localFileName(uri) ?? "file";
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
