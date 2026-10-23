import { describe, expect, test } from "bun:test";
import nacl from "tweetnacl";
import {
  directCandidate,
  directKind,
  parseDirectAddresses,
  storedDirectAddresses,
  verifyHealthChallenge,
} from "./directAddresses";
import { buildServerAssertionPayload } from "./deviceAuthContract";
import { bytesToHex } from "./protocolCrypto";
import { normalizeStoredServers } from "./storedServerContract";

describe("which addresses may be tried directly", () => {
  test("private-network IPv4 literals only", () => {
    expect(directKind("192.168.1.20")).toBe("lan");
    expect(directKind("10.0.2.2")).toBe("lan");
    expect(directKind("172.16.0.1")).toBe("lan");
    expect(directKind("172.31.255.1")).toBe("lan");
    expect(directKind("100.64.0.1")).toBe("tailscale");
    expect(directKind("100.127.255.254")).toBe("tailscale");
    for (const host of [
      "example.com",
      "my-mac.local",
      "localhost",
      "127.0.0.1",
      "8.8.8.8",
      "172.32.0.1",
      "100.128.0.1",
      "192.168.1.256",
      "192.168.01.1",
      "192.168.1",
      "::1",
      "fd00::1",
      "",
    ]) {
      expect(directKind(host)).toBeNull();
    }
  });

  test("a published list keeps only usable, distinct addresses", () => {
    expect(
      parseDirectAddresses([
        { host: "192.168.1.20", port: 9876, kind: "lan" },
        { host: "192.168.1.20", port: 9876, kind: "lan" },
        { host: "100.64.0.9", port: 9876, kind: "lan" },
        { host: "evil.example", port: 9876 },
        { host: "192.168.1.21", port: 0 },
        { host: "192.168.1.22", port: 70000 },
        { host: "192.168.1.23", port: "9876" },
        null,
        "192.168.1.24",
      ]),
    ).toEqual([
      { host: "192.168.1.20", port: 9876, kind: "lan" },
      // The phone decides the kind from the address, not from the message.
      { host: "100.64.0.9", port: 9876, kind: "tailscale" },
    ]);
    expect(parseDirectAddresses(undefined)).toEqual([]);
    expect(parseDirectAddresses({ host: "192.168.1.20", port: 1 })).toEqual([]);
  });

  test("saved direct addresses survive storage and skip damaged entries", () => {
    const [server] = normalizeStoredServers([
      {
        id: "s",
        name: "Mac",
        url: "wss://relay.link.test/ws",
        daemonId: "d".repeat(64),
        daemonPublicKey: "e".repeat(64),
        transportKind: "link",
        transportPin: "3".repeat(64),
        linkRouteId: "4".repeat(32),
        transportCandidates: [
          { name: "Mewla Link", kind: "link", url: "wss://relay.link.test/ws" },
          directCandidate({ host: "192.168.1.20", port: 9876, kind: "lan" }),
          { name: "bad", kind: "direct", url: "wss://evil.example:9876/ws" },
        ],
      },
    ]);
    expect(server.transportCandidates?.filter((c) => c.kind === "direct")).toHaveLength(2);
    expect(storedDirectAddresses(server)).toEqual([{ host: "192.168.1.20", port: 9876, kind: "lan" }]);
  });
});

describe("the health challenge", () => {
  const keys = nacl.sign.keyPair.fromSeed(new Uint8Array(32).fill(3));
  const other = nacl.sign.keyPair.fromSeed(new Uint8Array(32).fill(4));
  const daemonId = "a".repeat(64);
  const publicKey = bytesToHex(keys.publicKey);
  const challenge = "0123456789abcdef0123456789abcdef";

  function answer(over: Record<string, unknown> = {}, signer = keys, nonce = challenge, purpose = "mewla-health-challenge") {
    const timestamp = new Date().toISOString();
    return {
      daemon_id: daemonId,
      daemon_public_key: bytesToHex(signer.publicKey),
      assertion_purpose: purpose,
      assertion_timestamp: timestamp,
      assertion_nonce: nonce,
      assertion_signature: bytesToHex(
        nacl.sign.detached(buildServerAssertionPayload(purpose, daemonId, timestamp, nonce), signer.secretKey),
      ),
      ...over,
    };
  }
  const verify = (payload: unknown, over: Record<string, string> = {}) =>
    verifyHealthChallenge({ challengeHex: challenge, daemonId, daemonPublicKey: publicKey, payload, ...over });

  test("accepts a fresh answer from the paired daemon", () => {
    expect(verify(answer())).toBe(true);
  });

  test("rejects a replayed answer to an earlier challenge", () => {
    expect(verify(answer({}, keys, "ffffffffffffffffffffffffffffffff"))).toBe(false);
    expect(verify(answer(), { challengeHex: "ffffffffffffffffffffffffffffffff" })).toBe(false);
  });

  test("rejects an answer signed by a different key", () => {
    expect(verify(answer({}, other))).toBe(false);
    // Claiming the paired key while signing with another fails the signature.
    expect(verify(answer({ daemon_public_key: publicKey }, other))).toBe(false);
  });

  test("rejects another daemon, another purpose, a legacy daemon and a forged signature", () => {
    expect(verify(answer(), { daemonId: "b".repeat(64) })).toBe(false);
    expect(verify(answer({ daemon_id: "b".repeat(64) }))).toBe(false);
    expect(verify(answer({}, keys, challenge, "mewla-health"))).toBe(false);
    expect(verify(answer({ assertion_purpose: "mewla-health" }))).toBe(false);
    expect(verify(answer({ assertion_nonce: "11".repeat(16) }))).toBe(false);
    expect(verify(answer({ assertion_signature: "00".repeat(64) }))).toBe(false);
  });

  test("rejects anything that is not an answer", () => {
    for (const payload of [null, undefined, "ok", 7, [], {}, { status: "ok" }]) {
      expect(verify(payload)).toBe(false);
    }
    expect(verify(answer(), { challengeHex: "short" })).toBe(false);
  });
});
