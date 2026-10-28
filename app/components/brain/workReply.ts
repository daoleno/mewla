import type { SendAttachment } from "../../services/structuredWebSocketTransport";
import type { ComposerAttachment } from "../terminal/InterfaceChatSession";

/** The uploaded files a reply carries; one still uploading or failed is not sent. */
export function workReplyFiles(attachments: readonly ComposerAttachment[]): SendAttachment[] {
  return attachments
    .filter((item) => (!item.uploadStatus || item.uploadStatus === "ready") && item.path)
    .map((item) => ({
      name: item.name,
      path: item.path,
      ...(item.mimeType ? { content_type: item.mimeType } : {}),
    }));
}

/**
 * A reply can go when it has words or files, nothing is mid-upload (a failed
 * upload blocks until it is retried or removed, so a file is never dropped
 * silently) and no reply is already in flight.
 */
export function workReplyReady(draft: string, attachments: readonly ComposerAttachment[], busy: boolean): boolean {
  if (busy) return false;
  if (attachments.some((item) => item.uploadStatus && item.uploadStatus !== "ready")) return false;
  return Boolean(draft.trim()) || workReplyFiles(attachments).length > 0;
}
