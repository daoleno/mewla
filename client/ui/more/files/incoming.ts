import { platform } from "nsh";
import type { UploadDocumentAsset } from "../../../core/files/uploads";
import { fileTypeOf, localFileContents } from "../../../core/files/pastedFiles";
import { keepAppFiles } from "./appFiles";
import { pickedAssets, type PickedFileEntry } from "./host";
import type { AttachmentUploads } from "./upload";

/**
 * Files put into a composer without its Attach button, which become chips as picked ones do:
 * - `files`: files pasted into the field (NSH's `onPaste` `files`: a screenshot, Copy Image; on the Mac the contents
 *   of files copied in Finder), uploaded as the app's own (`appFiles.ts`);
 * - `dropped`: files dropped on the conversation with their contents (NSH's `onFileDrop` `files` on the Mac, the
 *   shape of a pick: Finder's files, an image dragged out of a browser, the screenshot thumbnail), uploaded the same
 *   way, one past what a drop reads a failed chip that says so;
 * - `uris`: the local files a drop or a Finder copy named (`file:` URLs, the desktops): never read here, only
 *   matched with the contents that came (`localFileContents`); one that came without contents is a failed chip;
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
    ...sorted.unread.map(({ uri, name }): UploadDocumentAsset => ({
      uri: `app-file:unread:${uri}`, name, mimeType: fileTypeOf(name) || null, size: null,
      selectionError: "Mewla can't read this file.", selectionRetryable: false,
    })),
  ];
  if (!assets.length) throw new Error(sorted.folders ? "Folders can't be attached." : "The dropped file didn't arrive.");
  // Not connected or no room: said before any chip shows.
  uploads.checkRoom(assets.length);
  uploads.addAssets(assets);
}
