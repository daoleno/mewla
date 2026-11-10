import { getOrCreateLocalDeviceIdentity } from "../auth";
import { signDeviceAuthorization } from "../deviceAuthContract";
import { buildHTTPURL } from "../pairing";
import { platform } from "../platform";
import { bytesToHex } from "../protocolCrypto";
import type { StoredServer } from "../storedServerContract";
import type { ConnectionState, Frame, RequestOptions } from "../transport";

/**
 * What a page needs of the current computer. The session's Transport is one;
 * page tests give a fake that answers by frame type. A page never keeps one
 * across a computer switch: it gets the next computer's.
 */
export interface Daemon {
  readonly server: StoredServer;
  readonly state: ConnectionState;
  request<T = Frame>(type: string, body: Record<string, unknown>, options: RequestOptions): Promise<T>;
  on(type: string, listener: (frame: Frame) => void): () => void;
  onState(listener: (state: ConnectionState) => void): () => void;
  send(frame: Frame): void;
}

export type HttpMethod = "GET" | "POST" | "PUT" | "DELETE";

const HTTP_TIMEOUT_MS = 30_000;

/**
 * What the signature is for: a fixed purpose, or one made from the body's SHA-256
 * (`mewla-agent-use:POST:/agent-use:sha256=<hex>`), so the signature covers the body.
 */
export type Purpose = string | ((bodySha256: string) => string);

export interface SignedOptions {
  body?: unknown;
  query?: Record<string, string | undefined>;
  timeoutMs?: number;
}

/** `prefix:sha256=<hex of the body>`, the purpose of a signed POST. */
export function bodyPurpose(prefix: string): Purpose {
  return (digest) => `${prefix}:sha256=${digest}`;
}

export async function sha256Hex(text: string): Promise<string> {
  const digest = await platform().sha256(new TextEncoder().encode(text));
  return Array.from(digest, (byte) => byte.toString(16).padStart(2, "0")).join("");
}

/** The computer's HTTP address for `pathname` (and `query`), on the address the socket uses. */
export function daemonURL(server: StoredServer, pathname: string, query?: Record<string, string | undefined>): string {
  const url = buildHTTPURL(server.url, pathname);
  if (!url) throw new Error(`${server.name || "The computer"} has no address`);
  const params = Object.entries(query ?? {}).filter((entry): entry is [string, string] => Boolean(entry[1]));
  return params.length ? `${url}?${params.map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`).join("&")}` : url;
}

/** The `Authorization` header this phone's device key signs for one purpose on this computer. */
export async function deviceAuthorization(server: StoredServer, purpose: string): Promise<string> {
  const identity = await getOrCreateLocalDeviceIdentity();
  return signDeviceAuthorization({
    purpose,
    daemonId: server.daemonId || "",
    deviceId: identity.deviceId,
    seedHex: identity.seedHex,
    timestamp: Date.now().toString(),
    nonceHex: bytesToHex(platform().randomBytes(16)),
  });
}

/** A request signed by the device key; settles within 30 s. */
export async function signedFetch(
  server: StoredServer,
  method: HttpMethod,
  pathname: string,
  purpose: Purpose,
  options: SignedOptions = {},
): Promise<Response> {
  const url = daemonURL(server, pathname, options.query);
  const text = options.body === undefined ? undefined : JSON.stringify(options.body);
  const signed = typeof purpose === "function" ? purpose(text === undefined ? "" : await sha256Hex(text)) : purpose;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), options.timeoutMs ?? HTTP_TIMEOUT_MS);
  try {
    return await platform().fetch(url, {
      method,
      signal: controller.signal,
      headers: {
        ...(options.body === undefined ? {} : { "Content-Type": "application/json" }),
        Authorization: await deviceAuthorization(server, signed),
      },
      body: text,
    });
  } catch (error) {
    throw controller.signal.aborted ? new Error(`${server.name || "The computer"} did not answer`) : error;
  } finally {
    clearTimeout(timer);
  }
}

/** The error a non-2xx answer means: the computer's own words, or what its status says. */
export async function answerError(server: StoredServer, response: Response): Promise<Error> {
  const text = (await response.text().catch(() => "")).trim();
  let message = text;
  try {
    const parsed = JSON.parse(text) as { error?: unknown; message?: unknown };
    message = String(parsed.message ?? parsed.error ?? text);
  } catch {
    // Plain text.
  }
  if (response.status === 404 && !message) return new Error(`${server.name || "The computer"} runs an older Mewla; update it first`);
  return new Error(message || `${server.name || "The computer"} answered ${response.status}`);
}

/** A signed JSON request that resolves with the parsed answer or rejects with the computer's words. */
export async function signedJSON<T>(
  server: StoredServer,
  method: HttpMethod,
  pathname: string,
  purpose: Purpose,
  options: SignedOptions = {},
): Promise<T> {
  const response = await signedFetch(server, method, pathname, purpose, options);
  if (!response.ok) throw await answerError(server, response);
  const text = await response.text();
  return (text ? JSON.parse(text) : null) as T;
}

/** A request's failure in the words a page shows. */
export function failureText(error: unknown, fallback: string): string {
  const message = error instanceof Error ? error.message : typeof error === "string" ? error : "";
  return message.trim() || fallback;
}
