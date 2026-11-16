import { signal, untracked } from "nsh";
import { createChatFileExistence } from "../../core/files/chatFileExistence";
import { chatFileCandidates, linkFilePaths } from "../../core/files/chatPaths";
import { getExistingSessionFiles } from "../../core/files/requests";
import type { SessionFileIdentity } from "../../core/files/sessionFilePreview";
import { DaemonError } from "../../core/more/daemon";
import { daemon, daemonState } from "../more/daemon";

/**
 * The paths a reply names that are files on the speaking Session's computer (`core/files/chatFileExistence.ts`),
 * for `linkFilePaths`: relative ones are the Session's cwd's (Brain's: its workspace). Read in a row's markdown, it
 * draws the row again once the computer answered. Answers are kept per server and run of the Session, so another
 * server's never link a path here.
 */

const answered = signal(0);
const existence = createChatFileExistence({
  onChange: () => answered.set(untracked(answered) + 1),
  isFinalRefusal: (error) => error instanceof DaemonError && error.code === "unknown_message_type",
});
const NONE: ReadonlySet<string> = new Set();

/** Of `candidates` (`chatFileCandidates` of a reply), the files there; asks the computer about the rest. */
export function knownChatFiles(candidates: string[], identity: SessionFileIdentity | null): ReadonlySet<string> {
  answered();
  // Tracked: a row drawn before the computer connected asks once it has (and asks again what failed while away).
  const current = daemon();
  if (!candidates.length || !identity || !current || daemonState() !== "connected") return NONE;
  const scope = `${current.server.id}|${identity.workerId}|${identity.processId}|${identity.startedAt}`;
  return existence.existing(scope, candidates, (paths) => getExistingSessionFiles(current, identity, paths));
}

/** A reply's Markdown with the files it names as links, made again only when its text or what is known changes. */
export function createReplyLinks(identity: () => SessionFileIdentity | null): (body: string, streaming: boolean) => string {
  let lastBody: string | null = null;
  let candidates: string[] | null = null;
  let lastKey = "";
  let lastLinked = "";
  return (body, streaming) => {
    const changed = body !== lastBody;
    if (changed) {
      lastBody = body;
      candidates = null;
    }
    // A streaming reply is asked about once its words stop.
    if (!streaming && !candidates) candidates = chatFileCandidates(body);
    const known = streaming || !candidates ? NONE : knownChatFiles(candidates, identity());
    const key = [...known].join("\n");
    if (changed || key !== lastKey) {
      lastKey = key;
      lastLinked = linkFilePaths(body, known);
    }
    return lastLinked;
  };
}
