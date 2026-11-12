import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { supersededAtAddress } from "./pairFlow";

// R5#1: a browser that had paired with an earlier install at its own address paired again from a
// new `mewla web` link, but kept the old entry: a second "manjaro" at the same address, refused
// for ever (wrong-daemon).
const server = (id: string, url: string, key: string) => ({ id, name: "manjaro", url, daemonId: `id-${key}`, daemonPublicKey: key });

describe("a browser's new pairing supersedes the old one at its address (R5#1)", () => {
  test("an entry at the same address with another daemon key goes; the rest stay", () => {
    const paired = server("new", "ws://127.0.0.1:9871/ws", "KEYB");
    const list = [
      server("old", "WS://127.0.0.1:9871/ws ", "keya"),
      paired,
      server("same-key", "ws://127.0.0.1:9871/ws", "keyb"),
      server("elsewhere", "ws://10.0.0.5:9877/ws", "keyc"),
    ];
    expect(supersededAtAddress(list, paired)).toEqual(["old"]);
  });

  test("nothing goes when the address is only the new one's", () => {
    const paired = server("new", "ws://127.0.0.1:9871/ws", "keyb");
    expect(supersededAtAddress([paired, server("other", "ws://127.0.0.1:9986/ws", "keya")], paired)).toEqual([]);
  });

  test("only a browser drops them: a phone keeps an address another computer may take back", () => {
    const pairing = readFileSync(join(import.meta.dir, "../ui/pairing.ts"), "utf8");
    expect(pairing).toContain('if (paired && device() === "browser") {');
    expect(pairing).toContain("supersededAtAddress(servers(), paired)");
  });
});
