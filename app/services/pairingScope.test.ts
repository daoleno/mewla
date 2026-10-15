import { describe, expect, test } from "bun:test";
import { alreadyPairedWith, pairingScopeCopy } from "./pairingScope";

describe("pairing scope copy", () => {
  test("title and message name the same device", () => {
    for (const device of ["browser", "phone"] as const) {
      const { title, message } = pairingScopeCopy(device);
      expect(title).toBe(`Pair this ${device}?`);
      expect(message).toContain(`gives this ${device} access`);
      expect(message).toContain(`Only pair a ${device} you trust.`);
      expect(`${title} ${message}`).not.toContain(device === "browser" ? "phone" : "browser");
    }
  });
});

describe("already paired with", () => {
  const key = "a".repeat(64);
  const id = "b".repeat(64);
  const servers = [{ daemonId: id, daemonPublicKey: key }];

  test("a code for a saved server skips the prompt", () => {
    expect(alreadyPairedWith(servers, { daemonPublicKey: key })).toBe(true);
    expect(alreadyPairedWith(servers, { daemonId: id, daemonPublicKey: key.toUpperCase() })).toBe(true);
  });

  test("a new server, a mismatched id or no link still asks", () => {
    expect(alreadyPairedWith(servers, { daemonPublicKey: "c".repeat(64) })).toBe(false);
    expect(alreadyPairedWith(servers, { daemonId: "d".repeat(64), daemonPublicKey: key })).toBe(false);
    expect(alreadyPairedWith([], { daemonPublicKey: key })).toBe(false);
    expect(alreadyPairedWith(servers, null)).toBe(false);
  });
});
