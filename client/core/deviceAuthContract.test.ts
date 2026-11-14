import { describe, expect, test } from "bun:test";
import nacl from "./vendor/nacl";
import { hexToBytes } from "./protocolCrypto";
import { buildSignaturePayload, signDeviceAuthorization } from "./deviceAuthContract";

const input = (seedHex: string) => ({
  purpose: "mewla-display", daemonId: "d".repeat(64), deviceId: "device-1", seedHex, timestamp: "1794550000000", nonceHex: "ab".repeat(16),
});

/** Whether the header's signature is the seed's own key signing the request's payload. */
function signedBy(header: string, seedHex: string): boolean {
  const [deviceId, daemonId, timestamp, nonceHex, signatureHex] = header.slice("Device v1:".length).split(":");
  const payload = buildSignaturePayload("mewla-display", daemonId, deviceId, timestamp, nonceHex);
  return nacl.sign.detached.verify(payload, hexToBytes(signatureHex), nacl.sign.keyPair.fromSeed(hexToBytes(seedHex)).publicKey);
}

describe("device authorization (a seed's signing key is derived once)", () => {
  const one = "11".repeat(32);
  const two = "22".repeat(32);

  test("every request of a seed is signed by that seed's key, the second as the first", () => {
    const first = signDeviceAuthorization(input(one));
    expect(signedBy(first, one)).toBe(true);
    expect(signDeviceAuthorization(input(one))).toBe(first);
  });

  test("another seed (a new device key) signs with its own key, never the one kept before", () => {
    signDeviceAuthorization(input(one));
    const other = signDeviceAuthorization(input(two));
    expect(signedBy(other, two)).toBe(true);
    expect(signedBy(other, one)).toBe(false);
    expect(signedBy(signDeviceAuthorization(input(one)), one)).toBe(true);
  });
});
