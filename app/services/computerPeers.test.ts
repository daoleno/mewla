import { describe, expect, test } from "bun:test";
import type { ComputerPeer } from "./computers";
import { peersForDaemon } from "./computerPeers";

const MAC = "fdce8164daf8e28d2e8a9ca286925ccb69d9eca64b80795e3dacf771c46d3f35";

describe("peersForDaemon", () => {
  test("finds a computer in both directions, by its id or its device's", () => {
    const peers: ComputerPeer[] = [
      { name: "mac", daemon_id: MAC, direction: "outbound", online: true },
      { name: "studio", device_id: `computer-${MAC.slice(0, 16)}`, direction: "inbound", online: false },
      { name: "other", daemon_id: "ab".repeat(32), direction: "outbound", online: true },
    ];
    expect(peersForDaemon(peers, MAC).map((peer) => peer.name)).toEqual(["mac", "studio"]);
    expect(peersForDaemon(peers, "")).toEqual([]);
    expect(peersForDaemon([{ name: "x", device_id: "computer-fd", direction: "inbound", online: true }], MAC)).toEqual([]);
  });
});
