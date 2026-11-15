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

describe("device authorization (a seed's key is the platform's, imported once)", () => {
  const one = "11".repeat(32);
  const two = "22".repeat(32);

  test("every request of a seed is signed by that seed's key, the second as the first, as tweetnacl signs it", async () => {
    const first = await signDeviceAuthorization(input(one));
    expect(signedBy(first, one)).toBe(true);
    expect(await signDeviceAuthorization(input(one))).toBe(first);
    const { purpose, daemonId, deviceId, timestamp, nonceHex } = input(one);
    const expected = nacl.sign.detached(buildSignaturePayload(purpose, daemonId, deviceId, timestamp, nonceHex), nacl.sign.keyPair.fromSeed(hexToBytes(one)).secretKey);
    expect(first.split(":").at(-1)).toBe(Buffer.from(expected).toString("hex"));
  });

  test("another seed (a new device key) signs with its own key, never the one kept before", async () => {
    await signDeviceAuthorization(input(one));
    const other = await signDeviceAuthorization(input(two));
    expect(signedBy(other, two)).toBe(true);
    expect(signedBy(other, one)).toBe(false);
    expect(signedBy(await signDeviceAuthorization(input(one)), one)).toBe(true);
  });
});
