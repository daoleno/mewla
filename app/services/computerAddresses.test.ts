import { describe, expect, test } from "bun:test";
import { otherComputerURLs } from "./computerAddresses";
import type { StoredServer } from "./storedServerContract";

const base: StoredServer = {
  id: "mac",
  name: "Mac mini",
  url: "ws://100.73.112.8:9877/ws",
  daemonId: "a".repeat(64),
  daemonPublicKey: "b".repeat(64),
};

describe("otherComputerURLs", () => {
  test("passes the address the phone uses and its manual candidates", () => {
    expect(
      otherComputerURLs({
        ...base,
        transportCandidates: [
          { name: "tailnet", kind: "manual", url: "https://mac.tail1234.ts.net" },
          { name: "same", kind: "manual", url: "http://100.73.112.8:9877" },
          { name: "relay", kind: "link", url: "wss://route.link.example" },
        ],
      }),
    ).toEqual(["http://100.73.112.8:9877", "https://mac.tail1234.ts.net"]);
  });

  test("never passes the phone's own Link tunnel", () => {
    expect(otherComputerURLs({ ...base, transportKind: "link", url: "http://127.0.0.1:41234" })).toEqual([]);
  });
});
