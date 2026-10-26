import * as Crypto from "expo-crypto";
import * as WebBrowser from "expo-web-browser";
import { getOrCreateLocalDeviceIdentity } from "./auth";
import {
  FRIENDS_RETURN_URL,
  friendsEnrollStartURL,
  friendsKeyFromSeed,
  friendsRequestPath,
  friendsServiceURL,
  parseFriendsEnrollReturn,
  signFriendsRequest,
  type FriendsKeyPair,
} from "./friendsContract";
import { bytesToHex } from "./protocolCrypto";
import { secureStorage } from "./secureStorage";
import { requestFailure, signedFetch } from "./signedRequest";
import type { StoredServer } from "./storage";

// The phone's Friends key. It is made here, kept in the keychain or keystore,
// and is not the key this phone pairs with computers by: a computer, and so an
// agent on it, can never sign as the person.
const KEY_NAME = "mewla.friends.humanSeed";

export interface FriendsMe {
  person: { id: string; handle: string; github_login: string };
  handle_changed: boolean;
  auto_post: boolean;
  daily_cap: number;
  devices: number;
  agent: { linked: boolean; name: string };
}

export interface ComputerFriends {
  linked: boolean;
  handle?: string;
  drafts: number;
}

export class FriendsError extends Error {
  constructor(
    message: string,
    readonly code: string,
  ) {
    super(message);
  }
}

const SIGN_IN_MESSAGES: Record<string, string> = {
  denied: "GitHub sign-in was cancelled",
  account_too_new: "This GitHub account is too new for Friends (30 days minimum)",
  provider: "GitHub did not answer; try again",
  invalid: "Sign-in did not finish; try again",
};

export async function getOrCreateFriendsKey(): Promise<FriendsKeyPair> {
  const existing = await secureStorage.getItemAsync(KEY_NAME);
  if (existing && /^[0-9a-f]{64}$/.test(existing)) return friendsKeyFromSeed(existing);
  const seed = bytesToHex(Crypto.getRandomBytes(32));
  await secureStorage.setItemAsync(KEY_NAME, seed);
  return friendsKeyFromSeed(seed);
}

async function sha256Hex(text: string): Promise<string> {
  return (await Crypto.digestStringAsync(Crypto.CryptoDigestAlgorithm.SHA256, text)).toLowerCase();
}

/** One request to the hosted service, signed with the phone's Friends key. */
async function friendsRequest<T>(method: "GET" | "POST" | "PUT" | "DELETE", apiPath: string, body?: unknown): Promise<T> {
  const base = friendsServiceURL();
  const key = await getOrCreateFriendsKey();
  const raw = body === undefined ? "" : JSON.stringify(body);
  const authorization = signFriendsRequest(key, {
    method,
    path: friendsRequestPath(base, apiPath),
    timestampMS: Date.now().toString(),
    nonceHex: bytesToHex(Crypto.getRandomBytes(16)),
    bodySha256Hex: await sha256Hex(raw),
  });
  const response = await fetch(`${base}${apiPath}`, {
    method,
    headers: { Authorization: authorization, "Content-Type": "application/json" },
    body: raw === "" ? undefined : raw,
  });
  if (!response.ok) {
    const problem = (await response.json().catch(() => ({}))) as { error?: string };
    throw new FriendsError(friendlyServiceError(response.status, problem.error), problem.error ?? String(response.status));
  }
  return (await response.json()) as T;
}

function friendlyServiceError(status: number, code: string | undefined): string {
  if (code === "already_enrolled") return "This GitHub account already has Friends on another phone";
  if (status === 429) return "Too many tries; wait a little and try again";
  return code ? `Friends answered: ${code}` : `Friends answered ${status}`;
}

/**
 * Opens GitHub sign-in in the system browser. The answer comes back on the
 * app's return link, which may reach a freshly started app (Android can drop
 * the app while the browser is in front), so the finish is a separate step
 * driven by whatever receives that link. Returns the link when the browser
 * session hands it back directly.
 */
export async function startFriendsSignIn(): Promise<string | null> {
  const key = await getOrCreateFriendsKey();
  const startURL = friendsEnrollStartURL(friendsServiceURL(), key.publicKeyHex);
  const result = await WebBrowser.openAuthSessionAsync(startURL, FRIENDS_RETURN_URL);
  return result.type === "success" ? result.url : null;
}

/** Answers the service's challenge with the phone's key, from the return link. Returns the account. */
export async function finishFriendsSignIn(returnURL: string): Promise<FriendsMe> {
  const parsed = parseFriendsEnrollReturn(returnURL);
  if (!parsed.ok) throw new FriendsError(SIGN_IN_MESSAGES[parsed.error], parsed.error);
  await friendsRequest("POST", "/v1/enroll/finish", {
    ticket: parsed.ticket,
    challenge: parsed.challenge,
    device_label: "Phone",
  });
  return friendsMe();
}

export const FRIENDS_SIGN_IN_CANCELLED = SIGN_IN_MESSAGES.denied;

export function friendsMe(): Promise<FriendsMe> {
  return friendsRequest<FriendsMe>("GET", "/v1/me");
}

/** A code, good for ten minutes and once, that lets a computer become this person's agent. */
export async function makeLinkCode(): Promise<string> {
  const { code } = await friendsRequest<{ code: string }>("POST", "/v1/me/link-code", {});
  return code;
}

async function computerFriends(server: StoredServer, method: "GET" | "POST" | "DELETE", body?: unknown): Promise<ComputerFriends> {
  const identity = await getOrCreateLocalDeviceIdentity();
  const raw = body === undefined ? undefined : JSON.stringify(body);
  const purpose =
    method === "POST"
      ? `mewla-friends:POST:/friends:sha256=${await sha256Hex(raw ?? "")}`
      : `mewla-friends:${method}:/friends`;
  const response = await signedFetch(server, method, "/friends", purpose, raw, identity);
  if (!response.ok) throw await requestFailure(server, response);
  return (await response.json()) as ComputerFriends;
}

export function getComputerFriends(server: StoredServer): Promise<ComputerFriends> {
  return computerFriends(server, "GET");
}

/** Makes the current computer's Brain this person's agent, replacing any earlier one. */
export async function linkComputer(server: StoredServer): Promise<ComputerFriends> {
  const code = await makeLinkCode();
  return computerFriends(server, "POST", { link_code: code });
}

/** The computer forgets the link and its drafts. The account on the service stays. */
export function unlinkComputer(server: StoredServer): Promise<ComputerFriends> {
  return computerFriends(server, "DELETE");
}
