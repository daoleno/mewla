import * as Crypto from "expo-crypto";
import { getOrCreateLocalDeviceIdentity } from "./auth";
import { friendsErrorMessage, type FriendsStatus } from "./friendsApi";
import { requestFailure, signedFetch } from "./signedRequest";
import type { StoredServer } from "./storage";

// The phone has no Friends credential. Everything goes to the paired
// computer's /friends door, signed as the device like every other feature,
// and the computer speaks to Friends as its Mewla account.

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

async function sha256Hex(text: string): Promise<string> {
  return (await Crypto.digestStringAsync(Crypto.CryptoDigestAlgorithm.SHA256, text)).toLowerCase();
}

async function computerRequest(server: StoredServer, method: "GET" | "POST", body?: unknown): Promise<Response> {
  const identity = await getOrCreateLocalDeviceIdentity();
  const raw = body === undefined ? undefined : JSON.stringify(body);
  const purpose =
    method === "POST" ? `mewla-friends:POST:/friends:sha256=${await sha256Hex(raw ?? "")}` : "mewla-friends:GET:/friends";
  return signedFetch(server, method, "/friends", purpose, raw, identity);
}

async function readResult<T>(server: StoredServer, response: Response): Promise<T> {
  if (response.ok) return (await response.json()) as T;
  const problem = (await response
    .clone()
    .json()
    .catch(() => null)) as { error?: string; message?: string } | null;
  if (problem?.error) throw new FriendsError(friendsErrorMessage(problem.error, problem.message), problem.error);
  throw await requestFailure(server, response);
}

export async function getFriendsStatus(server: StoredServer): Promise<FriendsStatus> {
  return readResult(server, await computerRequest(server, "GET"));
}

export async function friendsAction<T = FriendsStatus>(server: StoredServer, action: Record<string, unknown>): Promise<T> {
  return readResult(server, await computerRequest(server, "POST", action));
}

/** Starts the same sign-in `mewla login` does, on the paired computer. */
export function startComputerSignIn(server: StoredServer): Promise<LoginStart> {
  return friendsAction<LoginStart>(server, { op: "login_start" });
}

export async function pollComputerSignIn(server: StoredServer, loginId: string): Promise<boolean> {
  const result = await friendsAction<{ status: string }>(server, { op: "login_poll", login_id: loginId });
  return result.status === "done";
}

export function enableFriends(server: StoredServer, agentName: string, petId: string): Promise<FriendsStatus> {
  return friendsAction(server, { op: "enable", agent_name: agentName, pet_id: petId, tz: deviceTimeZone() });
}

/** Turns Friends off on the computer. The account, its posts, the sign-in and the drafts are kept. */
export function disableFriends(server: StoredServer): Promise<FriendsStatus> {
  return friendsAction(server, { op: "off" });
}

/** Deletes the Friends account and everything it made, and the computer's drafts. Friends is off after. */
export function deleteFriendsAccount(server: StoredServer): Promise<FriendsStatus> {
  return friendsAction(server, { op: "delete_account" });
}

export function setAutoPost(server: StoredServer, enabled: boolean): Promise<FriendsStatus> {
  return friendsAction(server, { op: "auto_post", enabled, tz: deviceTimeZone() });
}

/** Lets auto-post attach pictures and video, or not. Leaves auto-post itself as it is. */
export function setAutoMedia(server: StoredServer, media: boolean): Promise<FriendsStatus> {
  return friendsAction(server, { op: "auto_post", enabled: undefined, media, tz: deviceTimeZone() });
}

/** `reviewed` is the media files the owner was shown; the computer refuses a draft whose media was not. */
export function postDraft(server: StoredServer, draftId: string, text: string, reviewed: string[]) {
  return friendsAction<{ post: { id: string } }>(server, { op: "post_draft", draft_id: draftId, text, reviewed });
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

/** The scrub's verdict on text the owner is editing; resolves null when it passes. */
export async function checkDraftText(server: StoredServer, text: string): Promise<string | null> {
  try {
    await friendsAction(server, { op: "check", text });
    return null;
  } catch (reason) {
    return reason instanceof Error ? reason.message : "Friends will not post that";
  }
}

/** One Friends screen request, passed through the computer to the service. */
export async function friendsCall<T>(
  server: StoredServer,
  method: "GET" | "POST" | "PUT" | "DELETE" | "PATCH",
  path: string,
  body?: unknown,
): Promise<T> {
  const result = await friendsAction<{ status: number; body?: T; error?: string; kinds?: string[] }>(server, {
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

function deviceTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "";
  } catch {
    return "";
  }
}
