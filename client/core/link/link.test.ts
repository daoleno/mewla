import { beforeEach, describe, expect, test } from "bun:test";
import nacl from "../vendor/nacl";
import { bytesToHex } from "../protocolCrypto";
import { buildServerAssertionPayload } from "../deviceAuthContract";
import { pairingProblem, pairWith, readPairingLink } from "../pairFlow";
import type { LinkTunnels } from "../platform";
import type { StoredServer } from "../storedServerContract";
import { buildLinkPairingTestLink } from "../testing/linkPairingTestLink";
import { installMemoryPlatform } from "../testing/memoryPlatform";
import { learnDirectAddresses, parseDirectAddresses, withDirectAddresses } from "./directAddresses";
import {
  LINK_NEEDS_BUILD_MESSAGE,
  LINK_OFFLINE_MESSAGE,
  resetLinkTransportForTests,
  resolveStoredServerURL,
  transportFailed,
  transportPathInfo,
} from "./pinnedTransport";

const keys = nacl.sign.keyPair.fromSeed(new Uint8Array(32).fill(7));
const PUBLIC_KEY = bytesToHex(keys.publicKey);
const ROUTE = "3".repeat(32);
const PIN = "4".repeat(64);
const ADMISSION = "5".repeat(32);

function linkCode(): string {
  return buildLinkPairingTestLink({
    secretKey: keys.secretKey,
    publicKey: keys.publicKey,
    enrollmentToken: "2".repeat(64),
    routeId: ROUTE,
    transportPin: PIN,
    label: "quiet-otter-3f9a",
    domain: "a.link.test",
    name: "region-a",
    admissionAlias: ADMISSION,
  });
}

async function daemonId(): Promise<string> {
  return bytesToHex(new Uint8Array(await crypto.subtle.digest("SHA-256", keys.publicKey)));
}

async function signed(purpose: string, nonce = "6".repeat(32)) {
  const id = await daemonId();
  const timestamp = new Date().toISOString();
  const signature = nacl.sign.detached(buildServerAssertionPayload(purpose, id, timestamp, nonce), keys.secretKey);
  return { daemon_id: id, daemon_public_key: PUBLIC_KEY, assertion_purpose: purpose, assertion_timestamp: timestamp, assertion_nonce: nonce, assertion_signature: bytesToHex(signature) };
}

/** Host tunnels that hand out loopback ports and remember what was opened and closed. */
function fakeTunnels(rtt: Record<string, number | "down"> = {}) {
  let next = 4100;
  const open = new Map<string, { host: string; port: number; local: number; mode: string }>();
  const started: string[] = [];
  const stopped: string[] = [];
  const tunnels: LinkTunnels = {
    async start(key, host, port, pin, mode) {
      expect(pin).toBe(PIN);
      started.push(`${key} ${host}:${port} ${mode}`);
      if (rtt[host] === "down") throw new Error("Mewla Link could not reach a pinned relay candidate.");
      const local = next++;
      open.set(key, { host, port, local, mode });
      return { port: local, rttMs: typeof rtt[host] === "number" ? (rtt[host] as number) : 30 };
    },
    async stop(key) {
      stopped.push(key);
      open.delete(key);
    },
    async events() {
      return [];
    },
  };
  return { tunnels, open, started, stopped };
}

function linkServer(extra: Partial<StoredServer> = {}): StoredServer {
  return {
    id: "srv",
    name: "region-a",
    url: "wss://quiet-otter-3f9a.a.link.test/ws",
    daemonId: "",
    daemonPublicKey: PUBLIC_KEY,
    transportKind: "link",
    transportPin: PIN,
    linkRouteId: ROUTE,
    transportCandidates: [
      { name: "region-a", kind: "link", url: "wss://quiet-otter-3f9a.a.link.test/ws" },
      { name: "region-b", kind: "link", url: "wss://quiet-otter-3f9a.b.link.test/ws" },
    ],
    ...extra,
  };
}

beforeEach(() => resetLinkTransportForTests());

describe("pairing with a Mewla Link code", () => {
  test("pairs through a pinned tunnel to the code's admission and keeps the route, pin and stable address", async () => {
    const host = fakeTunnels();
    const calls: string[] = [];
    const answer = await signed("mewla-pair");
    installMemoryPlatform({
      linkTunnels: host.tunnels,
      fetch: async (url, init) => {
        calls.push(`${init?.method} ${url}`);
        return Response.json(answer);
      },
    });
    const progress: string[] = [];
    const server = await pairWith(readPairingLink(linkCode())!, (message) => progress.push(message));
    expect(host.started).toEqual([`pair:${ROUTE} ${ADMISSION}.a.link.test:443 on-demand`]);
    expect(calls).toEqual(["POST http://127.0.0.1:4100/pair"]);
    expect(host.stopped).toEqual([`pair:${ROUTE}`]);
    expect(progress).toEqual(["Connecting to quiet-otter-3f9a through Mewla…"]);
    expect(server).toEqual({
      name: "",
      url: "wss://quiet-otter-3f9a.a.link.test/ws",
      daemonId: await daemonId(),
      daemonPublicKey: PUBLIC_KEY,
      transportKind: "link",
      transportPin: PIN,
      linkRouteId: ROUTE,
      transportCandidates: [{ name: "region-a", kind: "link", url: "wss://quiet-otter-3f9a.a.link.test/ws" }],
    });
  });

  test("a lost answer is settled by asking the computer over the stable route whether it already trusts this phone", async () => {
    const host = fakeTunnels();
    const probe = await signed("mewla-probe");
    const calls: string[] = [];
    const platform = installMemoryPlatform({
      linkTunnels: host.tunnels,
      fetch: async (url, init) => {
        calls.push(`${init?.method} ${url}`);
        if (url.endsWith("/pair")) throw new Error("stream reset");
        const { getOrCreateLocalDeviceIdentity } = await import("../auth");
        return Response.json({ ...probe, device_id: (await getOrCreateLocalDeviceIdentity()).deviceId });
      },
    });
    expect(platform.linkTunnels).toBeDefined();
    const server = await pairWith(readPairingLink(linkCode())!);
    expect(server.transportKind).toBe("link");
    expect(host.started[1]).toBe(`pair-confirm:${ROUTE} quiet-otter-3f9a.a.link.test:443 on-demand`);
    expect(calls).toEqual(["POST http://127.0.0.1:4100/pair", "GET http://127.0.0.1:4101/auth-check"]);
    expect(host.stopped).toEqual([`pair:${ROUTE}`, `pair-confirm:${ROUTE}`]);
  });

  test("a computer that can't be reached at all says so", async () => {
    installMemoryPlatform({ linkTunnels: fakeTunnels().tunnels, fetch: () => Promise.reject(new Error("offline")) });
    const problem = pairingProblem(await pairWith(readPairingLink(linkCode())!).catch((e) => e));
    expect(problem.title).toBe("Can't reach your computer");
    expect(problem.detail).toContain("mewla is running");
  });

  test("a spent code that the computer does not know this phone by asks for a fresh one", async () => {
    installMemoryPlatform({
      linkTunnels: fakeTunnels().tunnels,
      fetch: async (url) => new Response("used", { status: 401 }),
    });
    const problem = pairingProblem(await pairWith(readPairingLink(linkCode())!).catch((e) => e));
    expect(problem.title).toBe("Code used or expired");
    expect(problem.detail).toContain("Run mewla pair");
  });

  test("where no host carries the tunnel, it says the app needs updating", async () => {
    installMemoryPlatform({ fetch: () => Promise.reject(new Error("unused")) });
    const error = await pairWith(readPairingLink(linkCode())!).catch((e) => e);
    expect(error.message).toBe(LINK_NEEDS_BUILD_MESSAGE);
  });
});

describe("reaching a computer on Mewla Link", () => {
  test("a computer paired by address is reached at its address", async () => {
    installMemoryPlatform();
    expect(await resolveStoredServerURL({ ...linkServer(), transportKind: "manual", url: "ws://10.0.0.5:9876/ws" })).toBe("ws://10.0.0.5:9876/ws");
  });

  test("measures every relay, keeps the nearest open, closes the rest, and reuses it while fresh", async () => {
    const host = fakeTunnels({ "quiet-otter-3f9a.a.link.test": 80, "quiet-otter-3f9a.b.link.test": 20 });
    installMemoryPlatform({ linkTunnels: host.tunnels });
    const url = await resolveStoredServerURL(linkServer());
    expect(url).toBe("ws://127.0.0.1:4101/ws");
    expect(host.stopped).toEqual(["server:srv:0"]);
    expect(transportPathInfo("srv")).toEqual({ path: "relay", host: null, rttMs: 20 });
    expect(await resolveStoredServerURL(linkServer())).toBe("ws://127.0.0.1:4101/ws");
    expect(host.started.length).toBe(2);
  });

  test("a failed relay is closed and measured afresh; with every relay down it says Link is offline", async () => {
    const rtt: Record<string, number | "down"> = {};
    const host = fakeTunnels(rtt);
    installMemoryPlatform({ linkTunnels: host.tunnels });
    await resolveStoredServerURL(linkServer());
    await transportFailed(linkServer());
    expect(transportPathInfo("srv")).toBeNull();
    rtt["quiet-otter-3f9a.a.link.test"] = "down";
    rtt["quiet-otter-3f9a.b.link.test"] = "down";
    expect((await resolveStoredServerURL(linkServer()).catch((e) => e)).message).toBe(LINK_OFFLINE_MESSAGE);
  });

  test("a LAN address the computer announced is used once it signs a fresh challenge", async () => {
    const host = fakeTunnels();
    installMemoryPlatform({
      linkTunnels: host.tunnels,
      fetch: async (url) => {
        const challenge = new URL(url).searchParams.get("challenge")!;
        return Response.json(await signed("mewla-health-challenge", challenge));
      },
    });
    const server = linkServer({ daemonId: await daemonId() });
    const learned = learnDirectAddresses(server, [{ host: "192.168.1.20", port: 9443 }, { host: "example.com", port: 1 }]);
    expect(learned.changed).toBe(true);
    const url = await resolveStoredServerURL(server);
    expect(transportPathInfo("srv")).toEqual({ path: "lan", host: "192.168.1.20", rttMs: 30 });
    const direct = [...host.open.values()].find((tunnel) => tunnel.host === "192.168.1.20")!;
    expect(url).toBe(`ws://127.0.0.1:${direct.local}/ws`);
  });

  test("an answer that is not the paired computer's keeps the phone on the relay", async () => {
    const host = fakeTunnels();
    installMemoryPlatform({ linkTunnels: host.tunnels, fetch: async () => Response.json(await signed("mewla-health-challenge")) });
    const server = linkServer({ daemonId: await daemonId() });
    learnDirectAddresses(server, [{ host: "192.168.1.20", port: 9443 }]);
    await resolveStoredServerURL(server);
    expect(transportPathInfo("srv")?.path).toBe("relay");
    expect(host.stopped).toContain("server:srv:direct:192.168.1.20:9443");
  });
});

describe("direct addresses", () => {
  test("only private IPv4 literals are kept, and they are saved on the computer's request path", () => {
    expect(parseDirectAddresses([{ host: "100.100.1.2", port: 9 }, { host: "8.8.8.8", port: 9 }, { host: "router.local", port: 9 }, { host: "10.0.0.1", port: 0 }])).toEqual([
      { host: "100.100.1.2", port: 9, kind: "tailscale" },
    ]);
    const server = linkServer({ transportCandidates: [{ name: "region-a", kind: "link", url: "wss://quiet-otter-3f9a.a.link.test/ws" }, { name: "LAN", kind: "direct", url: "wss://10.0.0.9:1/ws" }] });
    expect(withDirectAddresses(server, [{ host: "192.168.1.20", port: 9443, kind: "lan" }])).toEqual([
      { name: "region-a", kind: "link", url: "wss://quiet-otter-3f9a.a.link.test/ws" },
      { name: "LAN", kind: "direct", url: "wss://192.168.1.20:9443/ws" },
    ]);
  });
});
