import { ed25519Sign, ed25519Verify } from "./ed25519";
import { bytesToHex, hexToBytes, normalizeFixedHex } from "./protocolCrypto";

// Cross-language device authorization contract. The canonical purpose literals
// live with the daemon (daemon/auth); this module mirrors the exact signed
// payloads so the app and integration fixtures can build and verify them
// without loading native or secure-store modules.
export const DEVICE_AUTH_HEADER_PREFIX = "Device ";

export interface DaemonAssertionInput {
  purpose: string;
  daemonId: string;
  daemonPublicKey: string;
  timestamp: string | null | undefined;
  nonceHex: string | null | undefined;
  signatureHex: string | null | undefined;
}

export function normalizeDaemonId(rawValue: string | null | undefined): string {
  return normalizeFixedHex(rawValue, 64);
}

export function normalizePublicKeyHex(
  rawValue: string | null | undefined,
): string {
  return normalizeFixedHex(rawValue, 64);
}

export function buildSignaturePayload(
  purpose: string,
  daemonId: string,
  deviceId: string,
  timestamp: string,
  nonceHex: string,
): Uint8Array {
  const encoder = new TextEncoder();
  return encoder.encode(
    [
      purpose.trim(),
      normalizeDaemonId(daemonId),
      deviceId.trim(),
      timestamp.trim(),
      normalizeFixedHex(nonceHex, 32),
    ].join("\n"),
  );
}

export function buildServerAssertionPayload(
  purpose: string,
  daemonId: string,
  timestamp: string,
  nonceHex: string,
): Uint8Array {
  const encoder = new TextEncoder();
  return encoder.encode(
    [
      purpose.trim(),
      normalizeDaemonId(daemonId),
      timestamp.trim(),
      normalizeFixedHex(nonceHex, 32),
    ].join("\n"),
  );
}

/**
 * What a bound (v2) device header signs: daemon/auth BuildBoundSignaturePayload, line for line. Its first line is no
 * purpose, so a v1 signature never reads as a v2 one or back.
 */
export function buildBoundSignaturePayload(
  purpose: string,
  daemonId: string,
  deviceId: string,
  timestamp: string,
  nonceHex: string,
  binding: string,
): Uint8Array {
  return new TextEncoder().encode(
    ["mewla-device-v2", purpose.trim(), normalizeDaemonId(daemonId), deviceId.trim(), timestamp.trim(), normalizeFixedHex(nonceHex, 32), binding].join("\n"),
  );
}

/** A Host or URL hostname as a binding names it: daemon/server bindingHostname, line for line. */
export function bindingHost(hostname: string): string {
  let host = hostname.trim().toLowerCase();
  const bracketed = /^\[([^\]]*)\](?::\d+)?$/.exec(host);
  if (bracketed) host = bracketed[1];
  else if ((host.match(/:/g) ?? []).length === 1) host = host.replace(/:\d*$/, "");
  return host.replace(/\.$/, "");
}

/**
 * The binding a device header names (security review 11-23, PR B item 6), or null for a URL it cannot bind: the
 * transport key a pinned tunnel reaches (`pin:`), else the host the request goes to (`host:`). The daemon takes it only
 * from a request that reached it that way, so a host in the middle cannot hand the signature on. Grammar as
 * daemon/auth ValidBinding: nothing that could reshape the signed lines.
 */
export function deviceBinding(target: { transportPin?: string; url?: undefined } | { url: string; transportPin?: undefined }): string | null {
  if (target.url === undefined) {
    const pin = normalizeFixedHex(target.transportPin, 64);
    return pin ? `pin:${pin}` : null;
  }
  let host: string;
  try {
    host = bindingHost(new URL(target.url).hostname);
  } catch {
    return null;
  }
  if (!host || host.length > 253 || !/^[a-z0-9.:-]+$/.test(host)) return null;
  return `host:${host}`;
}

const base64url = (text: string) => globalThis.btoa(text).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");

export async function signDeviceAuthorization(input: {
  purpose: string;
  daemonId: string;
  deviceId: string;
  seedHex: string;
  timestamp: string;
  nonceHex: string;
  /** A `deviceBinding`: the header is then v2, taken only at what it names (by a daemon that signs challenges). */
  binding?: string | null;
}): Promise<string> {
  const seed = normalizeFixedHex(input.seedHex, 64);
  const nonceHex = normalizeFixedHex(input.nonceHex, 32);
  const daemonId = normalizeDaemonId(input.daemonId);
  const deviceId = input.deviceId.trim();
  const timestamp = input.timestamp.trim();
  if (!seed || !nonceHex || !daemonId || !deviceId || !timestamp) {
    throw new Error("Invalid device authorization input.");
  }
  if (input.binding) {
    const signature = await ed25519Sign(hexToBytes(seed), buildBoundSignaturePayload(input.purpose, daemonId, deviceId, timestamp, nonceHex, input.binding));
    return `${DEVICE_AUTH_HEADER_PREFIX}v2:${deviceId}:${daemonId}:${timestamp}:${nonceHex}:${base64url(input.binding)}:${bytesToHex(signature)}`;
  }
  const signature = await ed25519Sign(hexToBytes(seed), buildSignaturePayload(input.purpose, daemonId, deviceId, timestamp, nonceHex));
  return `${DEVICE_AUTH_HEADER_PREFIX}v1:${deviceId}:${daemonId}:${timestamp}:${nonceHex}:${bytesToHex(signature)}`;
}

export async function verifyDaemonAssertion(input: DaemonAssertionInput): Promise<boolean> {
  const daemonId = normalizeDaemonId(input.daemonId);
  const daemonPublicKey = normalizePublicKeyHex(input.daemonPublicKey);
  const nonceHex = normalizeFixedHex(input.nonceHex, 32);
  const signatureHex = normalizeFixedHex(input.signatureHex, 128);
  const timestamp = input.timestamp?.trim() || "";

  if (
    !input.purpose.trim() ||
    !daemonId ||
    !daemonPublicKey ||
    !timestamp ||
    !nonceHex ||
    !signatureHex
  ) {
    return false;
  }

  try {
    return await ed25519Verify(
      buildServerAssertionPayload(input.purpose, daemonId, timestamp, nonceHex),
      hexToBytes(signatureHex),
      hexToBytes(daemonPublicKey),
    );
  } catch {
    return false;
  }
}
