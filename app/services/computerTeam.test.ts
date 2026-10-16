import { expect, mock, test } from "bun:test";
import type { StoredServer } from "./storage";
import type { ComputerPeer } from "./computers";

// Other files replace ./auth and ./computers with their own fixtures, and Bun
// shares module mocks across one run, so this suite runs in its own process.
const isolated = process.env.MEWLA_COMPUTER_TEAM_ISOLATED === "1";

if (!isolated) {
  test("computer team passes in its own process", () => {
    const run = Bun.spawnSync([process.execPath, "test", import.meta.path], {
      cwd: `${import.meta.dir}/..`,
      env: { ...process.env, MEWLA_COMPUTER_TEAM_ISOLATED: "1" },
      stdout: "pipe",
      stderr: "pipe",
    });
    const output = `${run.stdout.toString()}${run.stderr.toString()}`;
    if (run.exitCode !== 0) throw new Error(output);
    expect(output).toContain(" 0 fail");
  }, 30_000);
} else {
  let answer: () => Promise<Response> = async () => new Response("{}");
  let genuine = true;
  mock.module("./auth", () => ({
    getOrCreateLocalDeviceIdentity: async () => ({ deviceId: "phone", seedHex: "00" }),
  }));
  mock.module("./signedRequest", () => ({ signedFetch: () => answer() }));
  mock.module("./deviceAuthContract", () => ({ verifyDaemonAssertion: () => genuine }));
  mock.module("./computers", () => ({
    listComputers: async () => [],
    connectComputers: async () => undefined,
    forgetComputer: async () => null,
  }));

  const { checkComputer, computersApartFrom, forgetEverywhere, joinComputers, resetComputerTeamForTests } =
    await import("./computerTeam");

  const mac: StoredServer = {
    id: "mac",
    name: "swift-hill-a9de.m.mewla.ai",
    url: "wss://swift-hill-a9de.m.mewla.ai/ws",
    daemonId: "a".repeat(32),
    daemonPublicKey: "1".repeat(64),
    transportKind: "link",
    transportPin: "2".repeat(64),
    linkRouteId: "3".repeat(32),
  };
  const linux: StoredServer = {
    id: "linux",
    name: "zen.daoleno.com",
    url: "wss://zen.daoleno.com/ws",
    daemonId: "b".repeat(32),
    daemonPublicKey: "4".repeat(64),
    transportKind: "manual",
  };
  const studio: StoredServer = { ...linux, id: "studio", url: "wss://studio.example.com/ws", daemonId: "c".repeat(32) };

  const deps = (peers: Record<string, ComputerPeer[]>, connect: (from: StoredServer, to: StoredServer) => Promise<void>, now = 0) => ({
    list: async (server: StoredServer) => {
      if (!peers[server.id]) throw new Error("offline");
      return peers[server.id];
    },
    connect,
    now: () => now,
  });

  test("a computer reached only through Mewla Link dials the other", async () => {
    resetComputerTeamForTests();
    const dialed: string[] = [];
    await joinComputers([mac, linux], deps({ mac: [], linux: [] }, async (from, to) => {
      dialed.push(`${from.id}->${to.id}`);
    }));
    expect(dialed).toEqual(["mac->linux"]);
  });

  test("a pair connected either way is left alone", async () => {
    resetComputerTeamForTests();
    const dialed: string[] = [];
    const inbound: ComputerPeer = { name: "Mac", device_id: `computer-${"a".repeat(16)}`, direction: "inbound", online: true };
    await joinComputers([mac, linux], deps({ mac: [], linux: [inbound] }, async (from, to) => {
      dialed.push(`${from.id}->${to.id}`);
    }));
    expect(dialed).toEqual([]);
  });

  test("a computer that does not answer is skipped", async () => {
    resetComputerTeamForTests();
    const dialed: string[] = [];
    await joinComputers([mac, linux, studio], deps({ linux: [], studio: [] }, async (from, to) => {
      dialed.push(`${from.id}->${to.id}`);
    }));
    expect(dialed).toEqual(["linux->studio"]);
  });

  test("the other direction is tried, and a pair that cannot meet waits", async () => {
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

  test("checkComputer reports the name of a computer that proves its key", async () => {
    genuine = true;
    answer = async () => new Response(JSON.stringify({ name: " daoleno's MacBook Pro " }));
    expect(await checkComputer(mac)).toEqual({ state: "online", name: "daoleno's MacBook Pro" });
    answer = async () => new Response("{}");
    expect(await checkComputer(mac)).toEqual({ state: "online" });
    genuine = false;
    expect(await checkComputer(mac)).toEqual({ state: "offline" });
    genuine = true;
    answer = async () => new Response("", { status: 401 });
    expect(await checkComputer(mac)).toEqual({ state: "unpaired" });
    answer = async () => {
      throw new Error("Mewla Link is offline.");
    };
    expect(await checkComputer(mac)).toEqual({ state: "offline" });
  });

  test("a removed computer is forgotten by every other one that answers", async () => {
    const told: string[] = [];
    const missed = await forgetEverywhere(mac, [mac, linux, studio], async (current, daemonId, other) => {
      if (current.id === "studio") throw new Error("offline");
      told.push(`${current.id} forgets ${daemonId === other.daemonId ? other.id : "?"}`);
    });
    expect(told).toEqual(["linux forgets mac"]);
    expect(missed.map((server) => server.id)).toEqual(["studio"]);
  });
}
