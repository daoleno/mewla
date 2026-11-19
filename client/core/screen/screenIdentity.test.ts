import { describe, expect, test } from "bun:test";
import { createHash, generateKeyPairSync, sign } from "node:crypto";
import { installMemoryPlatform } from "../testing/memoryPlatform";
import { offerIsTheComputers, screenDtlsPurpose, sdpFingerprints } from "./screenIdentity";

installMemoryPlatform();

const SDP = "v=0\r\na=fingerprint:SHA-256 ab:cd:01\r\nm=video\r\na=fingerprint:sha-256 AB:CD:01\r\na=fingerprint:sha-1 ff:00\r\n";

describe("screen identity", () => {
  test("fingerprints and their purpose are the daemon's (screencast.Fingerprints, auth.ScreenDTLSPurpose)", async () => {
    expect(sdpFingerprints(SDP)).toEqual(["sha-1 FF:00", "sha-256 AB:CD:01"]);
    expect(await screenDtlsPurpose(sdpFingerprints(SDP))).toBe("mewla-screen-dtls:sha256=52c2bfeb09d883ca02cca46379637625422a008cf4c5d1136a9ef5a5b1982e70");
  });

  test("an offer counts as the computer's only with its signature over these certificates", async () => {
    const { publicKey, privateKey } = generateKeyPairSync("ed25519");
    const raw = publicKey.export({ format: "der", type: "spki" }).subarray(-32);
    const daemonPublicKey = Buffer.from(raw).toString("hex");
    const daemonId = createHash("sha256").update(raw).digest("hex");
    const timestamp = new Date().toISOString();
    const nonce = "0123456789abcdef0123456789abcdef";
    const purpose = await screenDtlsPurpose(sdpFingerprints(SDP));
    const signature = sign(null, Buffer.from([purpose, daemonId, timestamp, nonce].join("\n")), privateKey).toString("hex");
    const server = { daemonId, daemonPublicKey };
    const assertion = { timestamp, nonce_hex: nonce, signature_hex: signature };

    expect(await offerIsTheComputers(server, SDP, assertion)).toBe(true);
    expect(await offerIsTheComputers(server, SDP.replace("ff:00", "ff:01"), assertion)).toBe(false);
    expect(await offerIsTheComputers(server, SDP, undefined)).toBe(false);
    expect(await offerIsTheComputers(server, "v=0\r\n", assertion)).toBe(false);
    const other = generateKeyPairSync("ed25519").publicKey.export({ format: "der", type: "spki" }).subarray(-32);
    expect(await offerIsTheComputers({ daemonId, daemonPublicKey: Buffer.from(other).toString("hex") }, SDP, assertion)).toBe(false);
  });
});
