import nacl from "tweetnacl";
import { bytesToHex, hexToBytes } from "./protocolCrypto";

// Cross-language contract with daemon/reqsign and daemon/friendsvc: the exact
// bytes a phone signs for the hosted Friends service. Kept free of native
// modules so tests can build and check them.

/** Payload domain of phone-signed Friends requests (reqsign.FriendsHumanScheme). */
export const FRIENDS_HUMAN_DOMAIN = "mewla-friends-human-v1";
export const FRIENDS_HUMAN_SCHEME = "MewlaFriendsHuman";
export const FRIENDS_DEFAULT_URL = "https://connect.mewla.ai/friends";
/** Where the service sends the browser back to the app. */
export const FRIENDS_RETURN_URL = "mewla://friends/enroll";

/** The service's base URL; a development build may point at a local one. */
export function friendsServiceURL(override: string | undefined = process.env.EXPO_PUBLIC_MEWLA_FRIENDS_URL): string {
  const value = (override ?? "").trim().replace(/\/+$/, "");
  return value || FRIENDS_DEFAULT_URL;
}

/** The path the service sees, which is what a signature covers: mount prefix included. */
export function friendsRequestPath(baseURL: string, apiPath: string): string {
  const base = new URL(baseURL);
  return `${base.pathname.replace(/\/+$/, "")}${apiPath}`;
}

/** The byte string a phone signs for one request. bodySha256Hex is the hex SHA-256 of the body ("" body included). */
export function friendsSignaturePayload(
  method: string,
  path: string,
  timestampMS: string,
  nonceHex: string,
  bodySha256Hex: string,
): Uint8Array {
  return new TextEncoder().encode(
    [FRIENDS_HUMAN_DOMAIN, method, path, timestampMS, nonceHex, bodySha256Hex.toLowerCase()].join("\n"),
  );
}

export function friendsAuthorization(publicKeyHex: string, timestampMS: string, nonceHex: string, signatureHex: string): string {
  return `${FRIENDS_HUMAN_SCHEME} v1:${publicKeyHex}:${timestampMS}:${nonceHex}:${signatureHex}`;
}

export interface FriendsKeyPair {
  publicKeyHex: string;
  seedHex: string;
}

export function friendsKeyFromSeed(seedHex: string): FriendsKeyPair {
  const pair = nacl.sign.keyPair.fromSeed(hexToBytes(seedHex));
  return { publicKeyHex: bytesToHex(pair.publicKey), seedHex };
}

/** Signs a request with the phone's Friends key and returns the Authorization header value. */
export function signFriendsRequest(
  key: FriendsKeyPair,
  input: { method: string; path: string; timestampMS: string; nonceHex: string; bodySha256Hex: string },
): string {
  const payload = friendsSignaturePayload(input.method, input.path, input.timestampMS, input.nonceHex, input.bodySha256Hex);
  const pair = nacl.sign.keyPair.fromSeed(hexToBytes(key.seedHex));
  const signature = nacl.sign.detached(payload, pair.secretKey);
  return friendsAuthorization(key.publicKeyHex, input.timestampMS, input.nonceHex, bytesToHex(signature));
}

/** The page that starts GitHub sign-in for a key. */
export function friendsEnrollStartURL(baseURL: string, publicKeyHex: string, extra: Record<string, string> = {}): string {
  const query = new URLSearchParams({ key: publicKeyHex, return: FRIENDS_RETURN_URL, ...extra });
  return `${baseURL.replace(/\/+$/, "")}/v1/enroll/start?${query.toString()}`;
}

export type FriendsEnrollReturn =
  | { ok: true; ticket: string; challenge: string }
  | { ok: false; error: "denied" | "account_too_new" | "provider" | "invalid" };

/** Reads what the service put on the app's return link. */
export function parseFriendsEnrollReturn(url: string): FriendsEnrollReturn {
  if (!isFriendsReturnUrl(url)) return { ok: false, error: "invalid" };
  const query = new URLSearchParams(url.slice(url.indexOf("?") + 1));
  const error = query.get("error");
  if (error === "denied" || error === "account_too_new" || error === "provider") return { ok: false, error };
  const ticket = query.get("ticket") ?? "";
  const challenge = query.get("challenge") ?? "";
  if (!/^[0-9a-f]{32}$/.test(ticket) || !/^[0-9a-f]{32}$/.test(challenge)) return { ok: false, error: "invalid" };
  return { ok: true, ticket, challenge };
}

export function isFriendsReturnUrl(url: string): boolean {
  return url === FRIENDS_RETURN_URL || url.startsWith(`${FRIENDS_RETURN_URL}?`);
}
