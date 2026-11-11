import { effect, signal, untracked } from "nsh";
import { AttachmentUploadQueue, type AttachmentQueueItem } from "../../../core/files/attachmentUploadQueue";
import { buildAttachmentUploadPresentation } from "../../../core/files/attachmentUploadPresentation";
import { checkPickedCount, createAttachmentUploadOperation, MAX_COMPOSER_ATTACHMENTS, pickLimit, type UploadedAttachment } from "../../../core/files/uploads";
import type { StoredServer } from "../../../core/storedServerContract";
import { connection, current } from "../../session";
import { uploadFile as uploadThrough, type UploadFile, type UploadOptions, type UploadResult } from "../../../core/files/upload";
import { keepAppFiles } from "./appFiles";
import { pickFiles, uploadHost } from "./host";

export { AttachmentUploadCancelledError, type UploadFile, type UploadOptions, type UploadResult } from "../../../core/files/upload";
export { pickFiles } from "./host";

/**
 * Pasted or dropped files (an input's `onPaste` `files`) as files `uploadFile` takes, on
 * every platform: `uploadFile(server, pastedFiles([file])[0]!)`. A composer with chips
 * uses its `AttachmentUploads.add` instead.
 */
export function pastedFiles(files: readonly File[]): UploadFile[] {
  return keepAppFiles(files);
}

/**
 * Uploads one picked file (`pickFiles`, or `pastedFiles`) to `server`, streamed by the host;
 * resolves with where the computer keeps it. For a surface that attaches files its own way
 * (the terminal key bar); a composer with chips uses `createAttachmentUploads`.
 */
export function uploadFile(server: StoredServer, file: UploadFile, options: UploadOptions = {}): Promise<UploadResult> {
  return uploadThrough(uploadHost, server, file, options);
}

/**
 * A composer's attachments: files picked on the phone, streamed by the host to the
 * current computer's /upload, one at a time. Make one per composer, inside its
 * component (a computer switch drops what it held); send `ready()` with the message
 * as its `attachments` field.
 */
export interface AttachmentUploads {
  /** Each picked file, its state and progress (reactive). */
  items(): readonly AttachmentQueueItem[];
  /** The picker is open. */
  picking(): boolean;
  /** Attach can be offered now: the computer is connected, no picker is open and there is room (reactive). */
  canAttach(): boolean;
  /** Opens the picker for the room left, then uploads what was picked; throws what the person should read. */
  pickAndUpload(): Promise<void>;
  /**
   * Uploads files pasted or dropped into the composer (its input's `onPaste` `files`), as
   * picked ones are; throws what the person should read (not connected, too many).
   */
  add(files: readonly File[]): void;
  retry(id: string): void;
  /** Drops a file; cancels it while it uploads. */
  remove(id: string): void;
  /** The uploaded files, in the order they were picked. */
  ready(): UploadedAttachment[];
  /** After a send: forget the files that went with it. */
  clear(): void;
  dispose(): void;
}

let sequence = 0;

export function createAttachmentUploads(options: { server?: () => StoredServer | null } = {}): AttachmentUploads {
  const server = options.server ?? (() => current());
  const items = signal<readonly AttachmentQueueItem[]>([]);
  const picking = signal(false);
  let owner = server()?.id ?? "";
  const makeQueue = (target: StoredServer | null) =>
    new AttachmentUploadQueue(
      async (asset, progress) => {
        if (!target) throw new Error("Connect to a computer to attach files.");
        return createAttachmentUploadOperation(asset, target, uploadHost, { onProgress: progress });
      },
      (next) => items.set(next),
    );
  let queue = makeQueue(server());

  // One computer: a switch drops what was picked for the last one.
  effect(() => {
    const target = server();
    untracked(() => {
      if ((target?.id ?? "") === owner) return;
      owner = target?.id ?? "";
      queue.dispose();
      queue = makeQueue(target);
      items.set([]);
    });
  });

  return {
    items: () => items(),
    picking: () => picking(),
    canAttach: () => !!server() && connection() === "connected" && !picking() && items().length < MAX_COMPOSER_ATTACHMENTS,
    async pickAndUpload() {
      if (picking()) return;
      const room = pickLimit(MAX_COMPOSER_ATTACHMENTS - items().length);
      if (!server() || connection() !== "connected") throw new Error("Connect to a computer to attach files.");
      picking.set(true);
      try {
        const assets = checkPickedCount(await pickFiles(room), room);
        queue.enqueue(assets.map((asset) => ({ id: `upload-${(sequence += 1)}`, asset })));
      } finally {
        picking.set(false);
      }
    },
    add(files) {
      if (!files.length) return;
      if (!server() || connection() !== "connected") throw new Error("Connect to a computer to attach files.");
      const room = pickLimit(MAX_COMPOSER_ATTACHMENTS - items().length);
      const assets = checkPickedCount(keepAppFiles(files), room);
      queue.enqueue(assets.map((asset) => ({ id: `upload-${(sequence += 1)}`, asset })));
    },
    retry: (id) => queue.retry(id),
    remove: (id) => queue.remove(id),
    ready: () => items().flatMap((item) => (item.status === "ready" && item.result ? [item.result] : [])),
    clear() {
      const keep = new Set(items().filter((item) => item.status !== "ready").map((item) => item.id));
      queue.forgetCompleted(keep);
      items.set(items().filter((item) => keep.has(item.id)));
    },
    dispose() {
      queue.dispose();
      items.set([]);
    },
  };
}

/** What a chip says about a file. */
export function attachmentLabel(item: AttachmentQueueItem): string {
  switch (item.status) {
    case "queued": return "Waiting";
    case "uploading": return buildAttachmentUploadPresentation(item.asset.name, item.progress).progressLabel;
    case "ready": return "Ready";
    case "failed": return item.error || "Could not upload this file.";
  }
}
