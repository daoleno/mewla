import { describe, expect, test } from "bun:test";
import type { StoredServer } from "../storedServerContract";
import { installMemoryPlatform } from "../testing/memoryPlatform";
import {
  computersApartFrom,
  forgetEverywhere,
  joinComputers,
  listComputers,
  missedComputersMessage,
  otherComputerURLs,
  peersForDaemon,
  resetComputerTeamForTests,
  type ComputerPeer,
} from "./computerTeam";

const mac: StoredServer = {
  id: "mac",
  name: "Mac",
  url: "wss://swift-hill-a9de.m.mewla.ai/ws",
  daemonId: "a".repeat(32),
  daemonPublicKey: "1".repeat(64),
  transportKind: "link",
  transportPin: "2".repeat(64),
  linkRouteId: "3".repeat(32),
};
const linux: StoredServer = { id: "linux", name: "Linux", url: "wss://zen.example.com/ws", daemonId: "b".repeat(64), daemonPublicKey: "4".repeat(64), transportKind: "manual" };
const studio: StoredServer = { ...linux, id: "studio", name: "Studio", url: "wss://studio.example.com/ws", daemonId: "c".repeat(64) };

const deps = (peers: Record<string, ComputerPeer[]>, connect: (from: StoredServer, to: StoredServer) => Promise<void>, now = 0) => ({
  list: async (server: StoredServer) => {
    if (!peers[server.id]) throw new Error("offline");
    return peers[server.id];
  },
  connect,
  now: () => now,
});

describe("introducing computers", () => {
  test("a computer reached only through Mewla Link dials the other", async () => {
    resetComputerTeamForTests();
    const dialed: string[] = [];
    await joinComputers([mac, linux], deps({ mac: [], linux: [] }, async (from, to) => void dialed.push(`${from.id}->${to.id}`)));
    expect(dialed).toEqual(["mac->linux"]);
  });

  test("a pair connected either way is left alone", async () => {
    resetComputerTeamForTests();
    const dialed: string[] = [];
    const inbound: ComputerPeer = { name: "Mac", device_id: `computer-${"a".repeat(16)}`, direction: "inbound", online: true };
    await joinComputers([mac, linux], deps({ mac: [], linux: [inbound] }, async (from, to) => void dialed.push(`${from.id}->${to.id}`)));
    expect(dialed).toEqual([]);
  });

  test("a computer that does not answer is skipped", async () => {
    resetComputerTeamForTests();
    const dialed: string[] = [];
    await joinComputers([mac, linux, studio], deps({ linux: [], studio: [] }, async (from, to) => void dialed.push(`${from.id}->${to.id}`)));
    expect(dialed).toEqual(["linux->studio"]);
  });

  test("the other direction is tried, and a pair that cannot meet waits ten minutes", async () => {
    resetComputerTeamForTests();
    const dialed: string[] = [];
    const fail = async (from: StoredServer, to: StoredServer) => {
      dialed.push(`${from.id}->${to.id}`);
      throw new Error("no address");
    };
    await joinComputers([linux, studio], deps({ linux: [], studio: [] }, fail, 1_000));
    expect(dialed).toEqual(["linux->studio", "studio->linux"]);
    expect(computersApartFrom(linux)).toEqual([studio.daemonId]);
    expect(computersApartFrom(mac)).toEqual([]);

    await joinComputers([linux, studio], deps({ linux: [], studio: [] }, fail, 60_000));
    expect(dialed).toHaveLength(2);

    await joinComputers([linux, studio], deps({ linux: [], studio: [] }, async () => undefined, 11 * 60_000));
    expect(computersApartFrom(linux)).toEqual([]);
  });

  test("one round at a time: a second call joins the first", async () => {
    resetComputerTeamForTests();
    let lists = 0;
    const slow = { list: async () => (lists++, []), connect: async () => undefined, now: () => 0 };
    await Promise.all([joinComputers([linux, studio], slow), joinComputers([linux, studio], slow)]);
    expect(lists).toBe(2);
  });
});

describe("removing a computer", () => {
  test("every other computer that answers forgets it; the others are reported", async () => {
    const told: string[] = [];
    const missed = await forgetEverywhere(mac, [mac, linux, studio], async (current, daemonId, other) => {
      if (current.id === "studio") throw new Error("offline");
      told.push(`${current.id} forgets ${daemonId === other.daemonId ? other.id : "?"}`);
    });
    expect(told).toEqual(["linux forgets mac"]);
    expect(missed.map((server) => server.id)).toEqual(["studio"]);
    expect(missedComputersMessage(mac, missed)).toEqual({
      title: "Studio didn't answer",
      detail: 'It still works with Mac. On it, run mewla computers remove "Mac".',
    });
    expect(missedComputersMessage(mac, [])).toBeNull();
  });

  test("a computer that never answers does not hold the removal past its time", async () => {
    const missed = await forgetEverywhere(mac, [linux], () => new Promise(() => {}), 10);
    expect(missed.map((server) => server.id)).toEqual(["linux"]);
  });
});

describe("peers and addresses", () => {
  test("a peer is matched by daemon id, or by the start of it in its device id", () => {
    const peers: ComputerPeer[] = [
      { name: "x", daemon_id: "B".repeat(64), direction: "outbound", online: true },
      { name: "y", device_id: `computer-${"c".repeat(16)}`, direction: "inbound", online: true },
      { name: "z", device_id: "computer-cc", direction: "inbound", online: true },
    ];
    expect(peersForDaemon(peers, linux.daemonId).map((p) => p.name)).toEqual(["x"]);
    expect(peersForDaemon(peers, studio.daemonId).map((p) => p.name)).toEqual(["y"]);
    expect(peersForDaemon(peers, "")).toEqual([]);
  });

  test("the phone's own Link tunnel is never passed to another computer", () => {
    expect(otherComputerURLs(mac)).toEqual([]);
    expect(otherComputerURLs({ ...mac, transportCandidates: [{ name: "Manual", kind: "manual", url: "https://mac.example.com/ws" }] })).toEqual(["https://mac.example.com"]);
    expect(otherComputerURLs(linux)).toEqual(["https://zen.example.com"]);
  });

  test("the list is asked for with the device's signature", async () => {
    const seen: Array<{ url: string; auth: string | null }> = [];
    installMemoryPlatform({
      fetch: async (url, init) => {
        seen.push({ url: String(url), auth: new Headers(init?.headers).get("Authorization") });
        return new Response(JSON.stringify({ computers: [{ name: "Studio", daemon_id: studio.daemonId, direction: "outbound", online: true }] }));
      },
    });
    const peers = await listComputers(linux);
    expect(peers.map((p) => p.name)).toEqual(["Studio"]);
    expect(seen[0].url).toBe("https://zen.example.com/computers");
    expect(seen[0].auth?.startsWith("Device ")).toBe(true);
  });
});
