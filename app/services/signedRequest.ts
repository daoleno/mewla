import * as Crypto from "expo-crypto";
import type { LocalDeviceIdentity } from "./auth";
import { signDeviceAuthorization } from "./deviceAuthContract";
import { buildHTTPURL } from "./pairing";
import { bytesToHex } from "./protocolCrypto";
import { resolveStoredServerURL } from "./pinnedTransport";
import type { StoredServer } from "./storage";

const REQUEST_TIMEOUT_MS = 30_000;

/** A request signed by this phone's device key for one purpose. */
export async function signedFetch(
  server: StoredServer,
  method: "GET" | "POST",
  pathname: string,
  purpose: string,
  body: string | undefined,
  identity: LocalDeviceIdentity,
): Promise<Response> {
  const url = buildHTTPURL(await resolveStoredServerURL(server), pathname);
  if (!url) throw new Error(`${server.name} has no address`);
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);
  try {
    return await fetch(url, {
      method,
      signal: controller.signal,
      headers: {
        "Content-Type": "application/json",
        Authorization: signDeviceAuthorization({
          purpose,
          daemonId: server.daemonId || "",
          deviceId: identity.deviceId,
          seedHex: identity.seedHex,
          timestamp: Date.now().toString(),
          nonceHex: bytesToHex(Crypto.getRandomBytes(16)),
        }),
      },
      body,
    });
  } finally {
    clearTimeout(timer);
  }
}

export async function requestFailure(server: StoredServer, response: Response): Promise<Error> {
  const text = (await response.text().catch(() => "")).trim();
  if (response.status === 404) {
    return new Error(`${server.name} runs an older Mewla; update it first`);
  }
  return new Error(text || `${server.name} answered ${response.status}`);
}
