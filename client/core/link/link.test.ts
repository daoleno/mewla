import { afterEach, beforeEach, describe, expect, setSystemTime, test } from "bun:test";
import nacl from "../vendor/nacl";
import { bytesToHex } from "../protocolCrypto";
import { buildServerAssertionPayload } from "../deviceAuthContract";
import { CODE_EXPIRED, NOT_A_CODE, pairingLinkProblem, pairingProblem, pairWith, readPairingLink } from "../pairFlow";
import type { LinkTunnels } from "../platform";
import type { StoredServer } from "../storedServerContract";
import { buildLinkPairingTestLink } from "../testing/linkPairingTestLink";
import { installMemoryPlatform } from "../testing/memoryPlatform";
import { learnDirectAddresses, parseDirectAddresses, withDirectAddresses } from "./directAddresses";
import {
  checkDirectPath,
  LINK_OFFLINE_MESSAGE,
  moveAbandoned,
  resetLinkTransportForTests,
  resolveStoredServerURL,
  transportFailed,
  transportMoved,
  transportOpened,
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
      if (rtt[host] === "down") throw new Error("Mewla Relay could not be reached.");
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
afterEach(() => setSystemTime());

describe("pairing with a Mewla Relay code", () => {
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

  test("a desktop host with the tunnels (Mewla for Mac) pairs through them, as a phone does", async () => {
    const host = fakeTunnels();
    const calls: string[] = [];
    const answer = await signed("mewla-pair");
    installMemoryPlatform({ os: "desktop", linkTunnels: host.tunnels, fetch: async (url, init) => (calls.push(`${init?.method} ${url}`), Response.json(answer)) });
    const server = await pairWith(readPairingLink(linkCode())!);
    expect(host.started).toEqual([`pair:${ROUTE} ${ADMISSION}.a.link.test:443 on-demand`]);
    expect(calls).toEqual(["POST http://127.0.0.1:4100/pair"]);
    expect(server.transportKind).toBe("link");
  });

  test("an expired code says it expired, not that it is no code", () => {
    const code = linkCode();
    setSystemTime(new Date(Date.now() + 120_000));
    expect(readPairingLink(code)).toBeNull();
    expect(pairingLinkProblem(code)).toEqual(CODE_EXPIRED);
    expect(pairingLinkProblem("mewla://settings?p=AAAA&v=3")).toEqual(NOT_A_CODE);
  });
});

// Owner issue 11-23 ("mewla: 协议的 computer 无法连接"): a Mac and a browser have no pinned tunnel, so every Mewla
// Relay code (what `mewla pair` shows once remote access is on) stopped at "needs a current Android or iOS build".
// With remote access the stable address carries a WebPKI certificate for the same transport key, so they pair there.
describe("pairing with a Mewla Relay code where no host carries the tunnel (a browser)", () => {
  const STABLE = "quiet-otter-3f9a.a.link.test";

  test("an app without the tunnel pairs at the code's stable address and keeps it as the computer's address", async () => {
    const calls: string[] = [];
    const answer = await signed("mewla-pair");
    installMemoryPlatform({
      os: "desktop",
      fetch: async (url, init) => {
        calls.push(`${init?.method} ${url}`);
        return Response.json(answer);
      },
    });
    const progress: string[] = [];
    const server = await pairWith(readPairingLink(linkCode())!, (message) => progress.push(message));
    expect(calls).toEqual([`POST https://${STABLE}/pair`]);
    expect(progress).toEqual(["Connecting to quiet-otter-3f9a through Mewla…"]);
    expect(server).toEqual({
      name: "",
      url: `wss://${STABLE}/ws`,
      daemonId: await daemonId(),
      daemonPublicKey: PUBLIC_KEY,
      transportKind: "manual",
    });
  });

  test("a browser that can't reach the stable address says why in one line", async () => {
    installMemoryPlatform({ os: "web", pageOrigin: `https://${STABLE}`, fetch: () => Promise.reject(new Error("certificate not trusted")) });
    const problem = pairingProblem(await pairWith(readPairingLink(linkCode())!).catch((e) => e));
    expect(problem.title).toBe("Can't reach your computer");
    expect(problem.detail).toBe(`This browser couldn't reach ${STABLE}. A browser reaches a computer on Mewla Relay only when remote access is on there (mewla login) and mewla is running.`);
    expect(problem.detail).not.toContain("Android");
  });

  test("a browser on that computer's own page pairs there", async () => {
    const calls: string[] = [];
    const answer = await signed("mewla-pair");
    installMemoryPlatform({
      os: "web",
      pageOrigin: `https://${STABLE}`,
      fetch: async (url, init) => {
        calls.push(`${init?.method} ${url}`);
        return Response.json(answer);
      },
    });
    const server = await pairWith(readPairingLink(linkCode())!);
    expect(calls).toEqual([`POST https://${STABLE}/pair`]);
    expect(server.url).toBe(`wss://${STABLE}/ws`);
    expect(server.transportKind).toBe("manual");
  });

  test("a browser on the page of a computer on two relays pairs at the page's own address", async () => {
    const calls: string[] = [];
    const answer = await signed("mewla-pair");
    const twoRelays = buildLinkPairingTestLink({
      secretKey: keys.secretKey, publicKey: keys.publicKey, enrollmentToken: "2".repeat(64), routeId: ROUTE, transportPin: PIN,
      label: "quiet-otter-3f9a", domain: "a.link.test", name: "region-a", admissionAlias: ADMISSION, moreRelays: [{ name: "region-b", domain: "b.link.test" }],
    });
    installMemoryPlatform({ os: "web", pageOrigin: "https://quiet-otter-3f9a.b.link.test", fetch: async (url, init) => (calls.push(`${init?.method} ${url}`), Response.json(answer)) });
    const server = await pairWith(readPairingLink(twoRelays)!);
    expect(calls).toEqual(["POST https://quiet-otter-3f9a.b.link.test/pair"]);
    expect(server.url).toBe("wss://quiet-otter-3f9a.b.link.test/ws");
  });

  test("a browser on another computer's page says it can pair only with the computer it is on", async () => {
    const calls: string[] = [];
    installMemoryPlatform({ os: "web", pageOrigin: "https://other-cat-1a2b.a.link.test", fetch: async (url) => (calls.push(url), Response.json({})) });
    const problem = pairingProblem(await pairWith(readPairingLink(linkCode())!).catch((e) => e));
    expect(calls).toEqual([]);
    expect(problem.title).toBe("Can't reach your computer");
    expect(problem.detail).toBe(`This page reaches only other-cat-1a2b.a.link.test, the computer it came from. Open https://${STABLE} in the browser to use that computer.`);
  });
});

describe("a browser pairs only with the computer whose page it is on", () => {
  const code = (url: string) => `mewla://settings?u=${encodeURIComponent(url)}&k=${PUBLIC_KEY}&t=${"2".repeat(64)}`;

  test("a code for another computer's address says so before anything is sent", async () => {
    const calls: string[] = [];
    installMemoryPlatform({ os: "web", pageOrigin: "http://127.0.0.1:9876", fetch: async (url) => (calls.push(url), Response.json({})) });
    const problem = pairingProblem(await pairWith(readPairingLink(code("ws://192.168.1.20:9876/ws"))!).catch((e) => e));
    expect(calls).toEqual([]);
    expect(problem.detail).toBe("This page reaches only 127.0.0.1:9876, the computer it came from.");
  });

  test("a code for the page's own computer pairs as before", async () => {
    const calls: string[] = [];
    const answer = await signed("mewla-pair");
    installMemoryPlatform({ os: "web", pageOrigin: "http://localhost:9876", fetch: async (url, init) => (calls.push(`${init?.method} ${url}`), Response.json(answer)) });
    const server = await pairWith(readPairingLink(code("ws://127.0.0.1:9876/ws"))!);
    expect(calls).toEqual(["POST http://127.0.0.1:9876/pair"]);
    expect(server.url).toBe("ws://127.0.0.1:9876/ws");
  });
});

describe("reaching a computer on Mewla Relay", () => {
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

// The phones' hosts and Mewla for Mac (a desktop host, client/macos/src/link.rs) carry the same tunnels, and the
// connection takes the same paths on each: the owner asked 11-23 whether the desktop app found the LAN on Mewla Relay.
for (const os of ["android", "desktop"] as const)
describe(`a connection open on a path (${os} host)`, () => {
  /** A computer whose LAN address answers its health challenge while `answering` says so. */
  async function lanComputer(answering: { now: boolean }) {
    const host = fakeTunnels();
    installMemoryPlatform({
      os,
      linkTunnels: host.tunnels,
      fetch: async (url) => {
        if (!answering.now) throw new Error("connection reset");
        const challenge = new URL(url).searchParams.get("challenge")!;
        return Response.json(await signed("mewla-health-challenge", challenge));
      },
    });
    return { host, server: linkServer({ id: `srv-${os}`, daemonId: await daemonId() }) };
  }

  test("keeps it for every caller: a failed re-check never closes the tunnel the connection runs on", async () => {
    const answering = { now: true };
    const { host, server } = await lanComputer(answering);
    learnDirectAddresses(server, [{ host: "192.168.1.20", port: 9443 }]);
    const url = await resolveStoredServerURL(server);
    expect(transportPathInfo(server.id)?.path).toBe("lan");
    transportOpened(server);
    // Long past the selection's freshness, a screen's request comes while the health check would fail.
    setSystemTime(new Date(Date.now() + 5 * 60_000));
    answering.now = false;
    expect(await resolveStoredServerURL(server)).toBe(url);
    expect(host.stopped).not.toContain(`server:${server.id}:direct:192.168.1.20:9443`);
    // Once the connection on it fails, the next one measures afresh.
    await transportFailed(server);
    expect(host.stopped).toContain(`server:${server.id}:direct:192.168.1.20:9443`);
    await resolveStoredServerURL(server);
    expect(transportPathInfo(server.id)?.path).toBe("relay");
  });

  test("on the relay, a direct path that became reachable is checked beside it and becomes current only when the connection moves", async () => {
    const answering = { now: true };
    const { host, server: base } = await lanComputer(answering);
    // A computer of its own: what was learned for another stays learned for this run.
    const server = { ...base, id: `srv-relay-${os}` };
    const relay = await resolveStoredServerURL(server);
    expect(transportPathInfo(server.id)?.path).toBe("relay");
    // Nothing to move onto before the connection is open, nor while the computer names no direct address.
    expect(await checkDirectPath(server)).toBeNull();
    transportOpened(server);
    expect(await checkDirectPath(server)).toBeNull();
    // The phone joins the computer's Wi-Fi: the computer names its LAN address.
    learnDirectAddresses(server, [{ host: "192.168.1.20", port: 9443 }]);
    setSystemTime(new Date(Date.now() + 5 * 60_000));
    const found = await checkDirectPath(server);
    expect(found?.path).toBe("lan");
    expect(found?.url).toStartWith("ws://127.0.0.1:");
    // The check left the connection's path as it was: every other caller still gets the relay.
    expect(await resolveStoredServerURL(server)).toBe(relay);
    expect(transportPathInfo(server.id)?.path).toBe("relay");
    // The connection moved: the direct path is current, the relay's tunnel stays open for the old socket and as the fallback.
    transportMoved(server);
    expect(transportPathInfo(server.id)?.path).toBe("lan");
    expect(await resolveStoredServerURL(server)).toBe(found!.url);
    expect(host.stopped.some((key) => key.startsWith(`server:${server.id}:0`))).toBe(false);
  });

  test("a direct path that stops answering, or a move given up, closes only the tunnel found for it", async () => {
    const answering = { now: true };
    const { host, server: base } = await lanComputer(answering);
    const server = { ...base, id: `srv-abandon-${os}` };
    const relay = await resolveStoredServerURL(server);
    transportOpened(server);
    learnDirectAddresses(server, [{ host: "192.168.1.30", port: 9443 }]);
    expect(await checkDirectPath(server)).not.toBeNull();
    answering.now = false;
    expect(await checkDirectPath(server)).toBeNull();
    expect(host.stopped).toContain(`server:${server.id}:direct:192.168.1.30:9443`);
    answering.now = true;
    expect(await checkDirectPath(server)).not.toBeNull();
    expect(host.open.has(`server:${server.id}:direct:192.168.1.30:9443`)).toBe(true);
    moveAbandoned(server);
    await Promise.resolve();
    expect(host.open.has(`server:${server.id}:direct:192.168.1.30:9443`)).toBe(false);
    expect(host.stopped.some((key) => key.startsWith(`server:${server.id}:0`))).toBe(false);
    expect(await resolveStoredServerURL(server)).toBe(relay);
    expect(transportPathInfo(server.id)?.path).toBe("relay");
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
