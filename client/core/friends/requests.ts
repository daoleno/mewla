import { answerError, bodyPurpose, signedFetch } from "../more/daemon";
import type { StoredServer } from "../storedServerContract";
import { friendsErrorMessage, type FriendsStatus } from "./api";

/**
 * The phone has no Friends credential. Everything goes to the current computer's
 * /friends door, signed as this device (a POST's signature covers its body), and the
 * computer speaks to Friends as its Mewla account.
 */

export class FriendsError extends Error {
  constructor(
    message: string,
    readonly code: string,
  ) {
    super(message);
  }
}

export interface LoginStart {
  login_id: string;
  user_code: string;
  verification_uri: string;
  verification_uri_complete?: string;
  interval_seconds: number;
  expires_at_ms: number;
}

const GET_PURPOSE = "mewla-friends:GET:/friends";
const POST_PURPOSE = bodyPurpose("mewla-friends:POST:/friends");

async function readResult<T>(server: StoredServer, response: Response): Promise<T> {
  if (response.ok) return (await response.json()) as T;
  const text = await response.text().catch(() => "");
  let problem: { error?: string; message?: string } | null = null;
  try {
    problem = JSON.parse(text) as { error?: string; message?: string };
  } catch {
    // Not the computer's JSON.
  }
  if (problem?.error) throw new FriendsError(friendsErrorMessage(problem.error, problem.message), problem.error);
  throw await answerError(server, new Response(text, { status: response.status }));
}

export async function getFriendsStatus(server: StoredServer): Promise<FriendsStatus> {
  return readResult(server, await signedFetch(server, "GET", "/friends", GET_PURPOSE, { timeoutMs: 15_000 }));
}

export async function friendsAction<T = FriendsStatus>(server: StoredServer, action: Record<string, unknown>): Promise<T> {
  return readResult(server, await signedFetch(server, "POST", "/friends", POST_PURPOSE, { body: action, timeoutMs: 15_000 }));
}

/** Starts the same sign-in `mewla login` does, on the computer. */
export function startComputerSignIn(server: StoredServer): Promise<LoginStart> {
  return friendsAction<LoginStart>(server, { op: "login_start" });
}

export async function pollComputerSignIn(server: StoredServer, loginId: string): Promise<boolean> {
  const result = await friendsAction<{ status: string }>(server, { op: "login_poll", login_id: loginId });
  return result.status === "done";
}

/** Creates (or turns back on) the Friends person, named after this phone's pet. */
export function enableFriends(server: StoredServer, agentName: string, petId: string, tz: string): Promise<FriendsStatus> {
  return friendsAction(server, { op: "enable", agent_name: agentName, pet_id: petId, tz });
}

/** Turns Friends off on the computer. The account, its posts, the sign-in and the drafts are kept. */
export function disableFriends(server: StoredServer): Promise<FriendsStatus> {
  return friendsAction(server, { op: "off" });
}

/** Deletes the Friends account and everything it made, and the computer's drafts. Friends is off after. */
export function deleteFriendsAccount(server: StoredServer): Promise<FriendsStatus> {
  return friendsAction(server, { op: "delete_account" });
}

export function setAutoPost(server: StoredServer, enabled: boolean, tz: string): Promise<FriendsStatus> {
  return friendsAction(server, { op: "auto_post", enabled, tz });
}

/** Lets auto-post attach pictures and video, or not; auto-post itself stays as it is. */
export function setAutoMedia(server: StoredServer, media: boolean, tz: string): Promise<FriendsStatus> {
  return friendsAction(server, { op: "auto_post", media, tz });
}

/** `reviewed` is the media files the owner was shown; the computer refuses a draft whose media was not. */
export function postDraft(server: StoredServer, draftId: string, text: string, reviewed: string[]): Promise<{ post: { id: string } }> {
  return friendsAction(server, { op: "post_draft", draft_id: draftId, text, reviewed });
}

/** Takes one picture or video off a draft before it is posted. */
export function removeDraftMedia(server: StoredServer, draftId: string, index: number): Promise<FriendsStatus> {
  return friendsAction(server, { op: "draft_media_remove", draft_id: draftId, index });
}

/** A draft picture (or a video's poster) as a data URI the phone can show. */
export async function draftMediaImage(server: StoredServer, draftId: string, file: string): Promise<{ uri: string; width: number; height: number }> {
  const answer = await friendsAction<{ mime: string; width: number; height: number; data: string }>(server, { op: "draft_media", draft_id: draftId, file });
  return { uri: `data:${answer.mime};base64,${answer.data}`, width: answer.width, height: answer.height };
}

export function discardDraft(server: StoredServer, draftId: string): Promise<FriendsStatus> {
  return friendsAction(server, { op: "discard_draft", draft_id: draftId });
}

/** The scrub's verdict on text the owner is editing; null when it passes. */
export async function checkDraftText(server: StoredServer, text: string): Promise<string | null> {
  try {
    await friendsAction(server, { op: "check", text });
    return null;
  } catch (reason) {
    return reason instanceof Error ? reason.message : "Friends will not post that";
  }
}

export type FriendsMethod = "GET" | "POST" | "PUT" | "DELETE" | "PATCH";

/** One request of a Friends page, passed through the computer to the service. */
export async function friendsCall<T>(server: StoredServer, method: FriendsMethod, path: string, body?: unknown): Promise<T> {
  const result = await friendsAction<{ status: number; body?: T; error?: string }>(server, {
    op: "call",
    method,
    path,
    body: body === undefined ? null : body,
  });
  if (result.status >= 300) {
    const code = result.error ?? String(result.status);
    throw new FriendsError(friendsErrorMessage(code), code);
  }
  return result.body as T;
}
