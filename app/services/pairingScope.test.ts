import { describe, expect, test } from "bun:test";
import { pairingScopeCopy } from "./pairingScope";

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
