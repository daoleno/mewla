import { openUrl, untracked } from "nsh";
import { recognizeSessionFileReference, type SessionFileIdentity } from "../../core/files/sessionFilePreview";
import { filePreviewRoute } from "../more/files/routes";
import { nav } from "../shell/nav";
import { brain } from "../session";

/**
 * Where a chat's links and file paths open (SPEC §1.7 Links): a file the agent wrote previews
 * against the Session that wrote it (its live run, `identity`); a web page opens in the browser.
 * Brain's thread, a Calendar result in it and a Work file are Brain's: their files are on Brain's
 * host Session (`brainFiles`). A Session passes its own.
 */
export type ChatFiles = {
  /** The run of the Session the files belong to; null while it has none (not started, offline). */
  identity: () => SessionFileIdentity | null;
  /** Says why a link did not open. */
  notice: (text: string) => void;
};

export const NO_FILE_IDENTITY_NOTICE = "This Session has no live generation identity. Refresh the Session list and try again.";
const BRAIN_NOT_RUNNING = "Brain's files open once Brain is running.";
const LINK_FAILED = "Could not open the link.";

/** Brain's own Session's run, which its workspace files are read through. */
export function brainHostIdentity(): SessionFileIdentity | null {
  const host = untracked(brain)?.host_worker;
  return host?.id && host.process_id && host.started_at ? { workerId: host.id, processId: host.process_id, startedAt: host.started_at } : null;
}

/** Brain's files: read through its host Session. */
export function brainFiles(notice: (text: string) => void): ChatFiles {
  return { identity: brainHostIdentity, notice: (text) => notice(text === NO_FILE_IDENTITY_NOTICE ? BRAIN_NOT_RUNNING : text) };
}

/** A path the agent named: previewed, pushed on whatever is shown, so Back returns to the chat. */
export function openChatPath(path: string, files: ChatFiles): void {
  const reference = recognizeSessionFileReference(path);
  if (!reference) return;
  const identity = files.identity();
  if (!identity) return files.notice(NO_FILE_IDENTITY_NOTICE);
  nav.push(filePreviewRoute(reference, identity));
}

/** A link pressed in a reply: a file path previews; a web page opens in the browser; anything else does nothing. */
export function openChatLink(url: string, files: ChatFiles): void {
  if (recognizeSessionFileReference(url)) return openChatPath(url, files);
  if (/^https?:\/\//i.test(url)) void openUrl(url).catch(() => files.notice(LINK_FAILED));
}
