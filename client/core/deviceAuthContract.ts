import nacl from "./vendor/nacl";
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
 * The signing key of a seed, made once: deriving it is a scalar multiplication, as long as the signature
 * itself, and it ran on every signed request (a phone signs one per Files, Git or Screen read; ~40 ms each
 * in the app's interpreter on an emulator). One device has one seed, so one is kept.
 */
let derived: { seed: string; secretKey: Uint8Array } | null = null;
function secretKeyOf(seed: string): Uint8Array {
  if (derived?.seed !== seed) derived = { seed, secretKey: nacl.sign.keyPair.fromSeed(hexToBytes(seed)).secretKey };
  return derived.secretKey;
}

export function signDeviceAuthorization(input: {
  purpose: string;
  daemonId: string;
  deviceId: string;
  seedHex: string;
  timestamp: string;
  nonceHex: string;
}): string {
  const seed = normalizeFixedHex(input.seedHex, 64);
  const nonceHex = normalizeFixedHex(input.nonceHex, 32);
  const daemonId = normalizeDaemonId(input.daemonId);
  const deviceId = input.deviceId.trim();
  const timestamp = input.timestamp.trim();
  if (!seed || !nonceHex || !daemonId || !deviceId || !timestamp) {
    throw new Error("Invalid device authorization input.");
  }
  const signature = nacl.sign.detached(
    buildSignaturePayload(input.purpose, daemonId, deviceId, timestamp, nonceHex),
    secretKeyOf(seed),
  );
  return `${DEVICE_AUTH_HEADER_PREFIX}v1:${deviceId}:${daemonId}:${timestamp}:${nonceHex}:${bytesToHex(signature)}`;
}

export function verifyDaemonAssertion(input: DaemonAssertionInput): boolean {
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
    return nacl.sign.detached.verify(
      buildServerAssertionPayload(input.purpose, daemonId, timestamp, nonceHex),
      hexToBytes(signatureHex),
      hexToBytes(daemonPublicKey),
    );
  } catch {
    return false;
  }
}
