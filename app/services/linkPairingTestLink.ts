import nacl from "tweetnacl";
import { bytesToHex, hexToBytes } from "./protocolCrypto";

const BASE32_ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";

// Test-only builder for the Link pairing link daemon/link BuildPairingLink
// issues (layout in daemon/link/pairing_compact.go), signed with the given
// daemon key.
export function buildLinkPairingTestLink(input: {
  secretKey: Uint8Array;
  publicKey: Uint8Array;
  enrollmentToken: string;
  routeId: string;
  transportPin: string;
  expiresAtMs?: number;
  label: string;
  domain: string;
  name?: string;
  admissionAlias?: string;
}): string {
  const text = new TextEncoder();
  const name = input.name ?? "main";
  const expiresAtMs = input.expiresAtMs ?? Date.now() + 60_000;
  const stable = `wss://${input.label}.${input.domain}/ws`;
  const admission = input.admissionAlias
    ? `wss://${input.admissionAlias}.${input.domain}/ws`
    : "";
  const publicKeyHex = bytesToHex(input.publicKey);
  const binding = text.encode(
    [
      "3",
      "",
      publicKeyHex,
      input.enrollmentToken,
      input.routeId,
      input.transportPin,
      expiresAtMs.toString(),
      name,
      admission,
      stable,
    ].join("\n"),
  );
  const domainSeparator = text.encode("mewla-link-pairing-v2\u0000");
  const signed = new Uint8Array(domainSeparator.length + binding.length);
  signed.set(domainSeparator);
  signed.set(binding, domainSeparator.length);
  const signature = nacl.sign.detached(signed, input.secretKey);

  const expiry = new Uint8Array(6);
  new DataView(expiry.buffer).setUint16(0, Math.floor(expiresAtMs / 2 ** 32));
  new DataView(expiry.buffer).setUint32(2, expiresAtMs % 2 ** 32);
  const short = (value: string) => {
    const bytes = text.encode(value);
    return [Uint8Array.of(bytes.length), bytes];
  };
  const parts: Uint8Array[] = [
    Uint8Array.of(3),
    input.publicKey,
    hexToBytes(input.enrollmentToken),
    hexToBytes(input.routeId),
    hexToBytes(input.transportPin),
    expiry,
    signature,
    ...short(input.label),
    Uint8Array.of(1),
    ...short(name),
    ...short(input.domain),
    Uint8Array.of(443 >> 8, 443 & 0xff),
    Uint8Array.of(input.admissionAlias ? 1 : 0),
    ...(input.admissionAlias ? [hexToBytes(input.admissionAlias)] : []),
  ];
  const bytes = new Uint8Array(parts.reduce((total, part) => total + part.length, 0));
  let offset = 0;
  for (const part of parts) {
    bytes.set(part, offset);
    offset += part.length;
  }
  let encoded = "";
  let buffer = 0;
  let bits = 0;
  for (const byte of bytes) {
    buffer = (buffer << 8) | byte;
    bits += 8;
    while (bits >= 5) {
      bits -= 5;
      encoded += BASE32_ALPHABET[(buffer >> bits) & 31];
    }
    buffer &= (1 << bits) - 1;
  }
  if (bits > 0) encoded += BASE32_ALPHABET[(buffer << (5 - bits)) & 31];
  return `mewla://settings?v=3&p=${encoded}`;
}
