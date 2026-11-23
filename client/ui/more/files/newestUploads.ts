import { cleanup, signal, untracked } from "nsh";
import type { AttachmentUploads } from "./upload";

/** The newest composer's attachments (Brain's, a Session's, a Work's), for Mewla Dev's proof script (mac.ts `imageInput`). */
const newest = signal<{ uploads: AttachmentUploads } | null>(null);

export function newestUploads(): AttachmentUploads | null {
  return newest()?.uploads ?? null;
}

/** Makes `uploads` the newest until the caller's scope ends. */
export function markNewestUploads(uploads: AttachmentUploads): void {
  newest.set({ uploads });
  cleanup(() => {
    if (untracked(newest)?.uploads === uploads) newest.set(null);
  });
}
