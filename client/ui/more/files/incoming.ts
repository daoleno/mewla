import { host, platform } from "nsh";
import { readFile, stat } from "fs/promises";
import type { UploadDocumentAsset } from "../../../core/files/uploads";
import { fileTypeOf, localFileContents, localFileName, localFilePath } from "../../../core/files/pastedFiles";
import { isMacApp } from "../../mac/app";
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
 *   that came (`localFileContents`). NSH 0.7 hands over a local file's URL only, so those that came without contents are
 *   read here (`readLocalFiles`); one that cannot be read is a failed chip that says why;
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

/** What reading a local file gave: its bytes, a folder (left out), or why it can't be read. */
type LocalRead = { bytes: Uint8Array } | { folder: true } | { failed: string; size?: number };

const CANT_READ = "Mewla can't read this file.";
const TOO_LARGE = "This file is larger than 64 MB.";

/** The local files `uris` name, read for upload; folders are left out, an unreadable file is a failed chip. */
export async function readLocalFiles(uris: readonly string[]): Promise<UploadDocumentAsset[]> {
  const assets: UploadDocumentAsset[] = [];
  for (const uri of uris) {
    const path = localFilePath(uri);
    if (!path) continue;
    // A file reference the Mac could not resolve (`file:///.file/id=…`) has no name of its own: "File".
    const name = localFileName(uri) ?? "file";
    const type = fileTypeOf(name);
    let read: LocalRead;
    try {
      read = isMacApp() ? await readOnMac(path) : await readHere(path);
    } catch {
      read = { failed: CANT_READ };
    }
    if ("folder" in read) continue;
    if ("failed" in read) {
      assets.push({ uri: `app-file:unread:${path}`, name, mimeType: type || null, size: read.size ?? null, selectionError: read.failed, selectionRetryable: false });
      continue;
    }
    const [kept] = keepAppFiles([new File([read.bytes], name, { type })]);
    assets.push({ ...kept!, name });
  }
  return assets;
}

/** A local file read with `fs/promises` (nsh.json `fs.read`). */
async function readHere(path: string): Promise<LocalRead> {
  const info = await stat(path);
  if (info.isDirectory()) return { folder: true };
  if (info.size > READ_MAX) return { failed: TOO_LARGE, size: info.size };
  return { bytes: await readFile(path) };
}

/** Mewla for Mac's host's answer to `files.localCopy` (client/macos/src/dropped.rs). */
type MacCopy = { copy?: string; size?: number; folder?: boolean; failed?: "missing" | "denied" | "large" | "unreadable" };

const MAC_FAILED: Record<string, string> = {
  missing: "Mewla can't find this file.",
  denied: "macOS didn't let Mewla read this file.",
  large: TOO_LARGE,
};

/**
 * A dropped or pasted file on Mewla for Mac, read through its host: NSH 0.7.1's `fs/promises` opens every folder on
 * the way for reading, and macOS lets no app list some of them, though the drag gave Mewla the file (the screenshot
 * thumbnail dragged out after ⌘⇧4 lives in screencaptureui's own folder; owner 11-27). The host opens the file by
 * its path, as a Mac app reads a drop, and hands over a copy in Mewla's own folder, read here and let go. Phones and
 * the browser never get a local file's URL (a browser's drop comes with its contents, web/host.js), so this is the
 * Mac's alone.
 */
async function readOnMac(path: string): Promise<LocalRead> {
  const answer = await host.call<MacCopy>("files.localCopy", { path, maxBytes: READ_MAX }, { timeout: 60_000 });
  if (answer?.folder) return { folder: true };
  if (!answer?.copy) return { failed: MAC_FAILED[answer?.failed ?? ""] ?? CANT_READ, size: answer?.size };
  const copy = answer.copy;
  try {
    return { bytes: await readFile(copy) };
  } finally {
    // What is not let go here, the host clears at its next launch.
    void host.call("files.localCopyDone", { path: copy }).catch(() => undefined);
  }
}
