import { afterEach, beforeEach, describe, expect, setSystemTime, test } from "bun:test";
import nacl from "../vendor/nacl";
import { bytesToHex } from "../protocolCrypto";
import { buildServerAssertionPayload } from "../deviceAuthContract";
import type { LinkTunnels } from "../platform";
import type { StoredServer } from "../storedServerContract";
import { FakeSocket, installMemoryPlatform } from "../testing/memoryPlatform";
import { Transport, transportTiming } from "../transport";
import { learnDirectAddresses } from "./directAddresses";
import { resetLinkTransportForTests, resolveStoredServerURL, transportFailed, transportPathInfo } from "./pinnedTransport";
import { linkRouteBinding, linkRouteChange } from "./routeUpgrade";

// Owner 11-23: a computer paired by its hosted https address (remote access on) gains the LAN path after the update,
// with no new pairing. The computer tells its route over the connection; the app trusts it on the daemon key alone.
const keys = nacl.sign.keyPair.fromSeed(new Uint8Array(32).fill(9));
const other = nacl.sign.keyPair.fromSeed(new Uint8Array(32).fill(8));
const PUBLIC_KEY = bytesToHex(keys.publicKey);
const DAEMON_ID = "a".repeat(64);
const ROUTE = "3".repeat(32);
const PIN = "4".repeat(64);
const HOSTED = "wss://quiet-otter.a.link.test/ws";

/** A pairing as the current release stores one made by the hosted address. */
function hostedPairing(extra: Partial<StoredServer> = {}): StoredServer {
  return {
    id: "srv",
    name: "Desk",
    url: HOSTED,
    daemonId: DAEMON_ID,
    daemonPublicKey: PUBLIC_KEY,
    transportKind: "manual",
    transportCandidates: [{ name: "Manual", kind: "manual", url: HOSTED }],
    ...extra,
  };
}

/** The `link_route` the daemon sends (daemon/server/link_route.go), signed with `signer`. */
function routeFrame(input: { signer?: nacl.SignKeyPair; pin?: string; signedPin?: string; issuedAtMs?: number; candidates?: { name: string; url: string }[] } = {}) {
  const candidates = input.candidates ?? [{ name: "region-a", url: HOSTED }, { name: "region-b", url: "wss://quiet-otter.b.link.test/ws" }];
  const issuedAtMs = input.issuedAtMs ?? Date.now();
  const pin = input.signedPin ?? input.pin ?? PIN;
  // A field with a control character has no binding: such a frame is signed over the plain join, as a daemon that did not refuse would.
  const binding = linkRouteBinding({ daemonId: DAEMON_ID, daemonPublicKey: PUBLIC_KEY, routeId: ROUTE, transportPin: pin, issuedAtMs, candidates })
    ?? new TextEncoder().encode(["1", DAEMON_ID, PUBLIC_KEY, ROUTE, pin, String(issuedAtMs), ...candidates.flatMap((candidate) => [candidate.name.trim(), candidate.url.trim()])].join("\n"));
  const domain = new TextEncoder().encode("mewla-link-route-v1\u0000");
  const signed = new Uint8Array(domain.length + binding.length);
  signed.set(domain);
  signed.set(binding, domain.length);
  return {
    type: "link_route",
    version: 1,
    route_id: ROUTE,
    transport_pin: input.pin ?? PIN,
    candidates,
    issued_at_ms: issuedAtMs,
    signature: bytesToHex(nacl.sign.detached(signed, (input.signer ?? keys).secretKey)),
  };
}

function fakeTunnels() {
  let next = 4200;
  const started: string[] = [];
  const stopped: string[] = [];
  const tunnels: LinkTunnels = {
    async start(key, host, port, pin, mode) {
      expect(pin).toBe(PIN);
      started.push(`${key} ${host}:${port} ${mode}`);
      return { port: next++, rttMs: 30 };
    },
    async stop(key) {
      stopped.push(key);
    },
    async events() {
      return [];
    },
  };
  return { tunnels, started, stopped };
}

async function signedHealth(challenge: string) {
  const timestamp = new Date().toISOString();
  const signature = nacl.sign.detached(buildServerAssertionPayload("mewla-health-challenge", DAEMON_ID, timestamp, challenge), keys.secretKey);
  return {
    daemon_id: DAEMON_ID,
    daemon_public_key: PUBLIC_KEY,
    assertion_purpose: "mewla-health-challenge",
    assertion_timestamp: timestamp,
    assertion_nonce: challenge,
    assertion_signature: bytesToHex(signature),
  };
}

beforeEach(() => resetLinkTransportForTests());
afterEach(() => setSystemTime());

describe("the computer's route, told over the connection a pairing by address already has", () => {
  test("is signed on the same bytes the daemon signs (daemon/link TestRouteBindingPayloadIsLineForLine)", () => {
    const binding = linkRouteBinding({
      daemonId: " AA ",
      daemonPublicKey: "BB",
      routeId: "CC",
      transportPin: "DD",
      issuedAtMs: 1700000000000,
      candidates: [
        { name: "region-a", url: "wss://quiet-otter.a.link.test/ws" },
        { name: " region-b ", url: "wss://quiet-otter.b.link.test:8443/ws" },
      ],
    });
    expect(new TextDecoder().decode(binding!)).toBe(
      "1\naa\nbb\ncc\ndd\n1700000000000\nregion-a\nwss://quiet-otter.a.link.test/ws\nregion-b\nwss://quiet-otter.b.link.test:8443/ws",
    );
    // A control character in a name or URL has no binding (review 11-23, finding 4; the daemon refuses the same).
    // U+0085 is one Go's TrimSpace would strip and JavaScript's trim keeps; U+FEFF the other way round (round 2 P1-3).
    for (const bad of ["\n", "\r", "\u0000", "\t", "\u007f", "\u0085", "\ufeff"]) {
      for (const candidate of [
        { name: `region-a${bad}wss://evil.test/ws`, url: "wss://quiet-otter.a.link.test/ws" },
        { name: "region-a", url: `wss://quiet-otter.a.link.test/ws${bad}` },
        { name: `${bad}region-a`, url: "wss://quiet-otter.a.link.test/ws" },
      ]) {
        expect(linkRouteBinding({ daemonId: "AA", daemonPublicKey: "BB", routeId: "CC", transportPin: "DD", issuedAtMs: 1700000000000, candidates: [candidate] })).toBeNull();
      }
    }
  });

  test("a route whose names hold a newline is refused even under the computer's own signature", () => {
    installMemoryPlatform({ os: "android", linkTunnels: fakeTunnels().tunnels });
    const split = [{ name: "region-a\nwss://other.link.test/ws", url: HOSTED }];
    expect(linkRouteChange(hostedPairing(), routeFrame({ candidates: split }))).toBeNull();
    expect(linkRouteChange(hostedPairing(), routeFrame({ candidates: [{ name: "region-a", url: HOSTED }] }))).not.toBeNull();
  });

  test("makes the pairing a Mewla Relay route in place: same address and computer, pinned to the route's transport key", () => {
    installMemoryPlatform({ os: "desktop", linkTunnels: fakeTunnels().tunnels });
    const lan = { name: "LAN 192.168.1.20", kind: "direct" as const, url: "wss://192.168.1.20:9443/ws" };
    const change = linkRouteChange(hostedPairing({ transportCandidates: [{ name: "Manual", kind: "manual", url: HOSTED }, lan] }), routeFrame());
    expect(change).toEqual({
      transportKind: "link",
      transportPin: PIN,
      linkRouteId: ROUTE,
      transportCandidates: [
        { name: "region-a", kind: "link", url: HOSTED },
        { name: "region-b", kind: "link", url: "wss://quiet-otter.b.link.test/ws" },
        lan,
      ],
    });
  });

  test("is refused unless the computer's own key signed it, for the address the app is paired by, lately", () => {
    installMemoryPlatform({ os: "android", linkTunnels: fakeTunnels().tunnels });
    expect(linkRouteChange(hostedPairing(), routeFrame())).not.toBeNull();
    // Another transport key than the one signed: whoever answers at the hosted name cannot hand the app its own.
    expect(linkRouteChange(hostedPairing(), routeFrame({ pin: "5".repeat(64), signedPin: PIN }))).toBeNull();
    // Signed by another key than the computer's.
    expect(linkRouteChange(hostedPairing(), routeFrame({ signer: other }))).toBeNull();
    // A pairing by another address (a LAN one, another computer's name) stays as it is.
    expect(linkRouteChange(hostedPairing({ url: "wss://192.168.1.20:9443/ws" }), routeFrame())).toBeNull();
    expect(linkRouteChange(hostedPairing(), routeFrame({ candidates: [{ name: "region-a", url: "wss://someone-else.a.link.test/ws" }] }))).toBeNull();
    // A route signed long ago, or not over TLS.
    expect(linkRouteChange(hostedPairing(), routeFrame({ issuedAtMs: Date.now() - 2 * 24 * 60 * 60 * 1000 }))).toBeNull();
    expect(linkRouteChange(hostedPairing({ url: "ws://quiet-otter.a.link.test/ws" }), routeFrame({ candidates: [{ name: "a", url: "ws://quiet-otter.a.link.test/ws" }] }))).toBeNull();
    // Another version of the message.
    expect(linkRouteChange(hostedPairing(), { ...routeFrame(), version: 2 })).toBeNull();
    // A pairing that already is this route.
    const route = [{ name: "region-a", kind: "link" as const, url: HOSTED }, { name: "region-b", kind: "link" as const, url: "wss://quiet-otter.b.link.test/ws" }];
    expect(linkRouteChange(hostedPairing({ transportKind: "link", transportPin: PIN, linkRouteId: ROUTE, transportCandidates: route }), routeFrame())).toBeNull();
  });

  // Review 11-23, finding 5: a Relay pairing took no newer route, so a rename could only be fixed by pairing again.
  test("a Relay pairing takes the computer's newer route under the same pin: a rename moves its address", () => {
    installMemoryPlatform({ os: "android", linkTunnels: fakeTunnels().tunnels });
    const lan = { name: "LAN 192.168.1.20", kind: "direct" as const, url: "wss://192.168.1.20:9443/ws" };
    const old = hostedPairing({
      transportKind: "link", transportPin: PIN, linkRouteId: ROUTE, url: "wss://old-name.a.link.test/ws",
      transportCandidates: [{ name: "region-a", kind: "link", url: "wss://old-name.a.link.test/ws" }, lan],
    });
    expect(linkRouteChange(old, routeFrame())).toEqual({
      transportKind: "link",
      transportPin: PIN,
      linkRouteId: ROUTE,
      transportCandidates: [
        { name: "region-a", kind: "link", url: HOSTED },
        { name: "region-b", kind: "link", url: "wss://quiet-otter.b.link.test/ws" },
        lan,
      ],
      url: HOSTED,
    });
    // Under another pin: not taken (only pairing again moves a pin).
    expect(linkRouteChange(old, routeFrame({ pin: "6".repeat(64) }))).toBeNull();
    // Not the computer's own signature: not taken.
    expect(linkRouteChange(old, routeFrame({ signer: nacl.sign.keyPair.fromSeed(new Uint8Array(32).fill(99)) }))).toBeNull();
  });

  test("changes nothing in a browser, which carries no pinned tunnel", () => {
    installMemoryPlatform({ os: "web" });
    expect(linkRouteChange(hostedPairing(), routeFrame())).toBeNull();
  });
});

describe("a pairing by the hosted address, once its route is told", () => {
  const saved = { ...transportTiming };
  afterEach(() => Object.assign(transportTiming, saved));

  test("moves onto the LAN beside the open connection, and falls back to the relay through a pinned tunnel", async () => {
    Object.assign(transportTiming, { silentLimitMs: 60_000, moveCheckMs: 2, moveCheckMaxMs: 8, moveHealthyChecks: 2, moveOpenLimitMs: 2_000, moveBackoffMs: 60_000, retireLimitMs: 2_000 });
    const host = fakeTunnels();
    const sockets: FakeSocket[] = [];
    const lan = { up: true };
    installMemoryPlatform({
      os: "desktop",
      linkTunnels: host.tunnels,
      openSocket: (url, protocols) => {
        const socket = new FakeSocket(url, protocols);
        sockets.push(socket);
        return socket;
      },
      fetch: async (url) => {
        if (!lan.up) throw new Error("connection reset");
        return Response.json(await signedHealth(new URL(url).searchParams.get("challenge")!));
      },
    });
    const until = async (condition: () => boolean, what: string) => {
      const deadline = Date.now() + 2_000;
      while (!condition() && Date.now() < deadline) await new Promise((resolve) => setTimeout(resolve, 1));
      if (!condition()) throw new Error(`never: ${what}`);
    };

    // The current release's pairing connects to the hosted address itself, as it always did: no path label.
    const transport = new Transport(hostedPairing());
    const heard: number[] = [];
    transport.on("note", (frame) => heard.push(frame.n as number));
    transport.connect();
    await until(() => sockets.length === 1, "the connection to the hosted address");
    expect(sockets[0]!.url).toBe(HOSTED);
    sockets[0]!.open();
    await until(() => transport.state === "connected", "connected");
    expect(transportPathInfo("srv")).toBeNull();

    // The computer tells its route, then its addresses (daemon/server/server.go, in that order).
    const change = linkRouteChange(transport.server, routeFrame());
    expect(change).not.toBeNull();
    transport.adopt({ ...transport.server, ...change! });
    expect(transportPathInfo("srv")?.path).toBe("relay");
    learnDirectAddresses(transport.server, [{ host: "192.168.1.20", port: 9443 }]);

    // A make-before-break move onto the LAN, through a tunnel pinned to the route's key; the session stays.
    await until(() => sockets.length === 2, "a socket to the LAN path");
    expect(host.started).toContain("server:srv:direct:192.168.1.20:9443 192.168.1.20:9443 measure");
    expect(sockets[1]!.url).toStartWith("ws://127.0.0.1:");
    sockets[1]!.open();
    await until(() => transportPathInfo("srv")?.path === "lan", "the LAN path label");
    expect(transport.state).toBe("connected");
    sockets[1]!.receive({ type: "note", n: 1 });
    expect(heard).toEqual([1]);

    // The LAN is lost: the next connection reaches the relay through a pinned tunnel, never the plain hosted socket.
    lan.up = false;
    await transportFailed(transport.server);
    const relay = await resolveStoredServerURL(transport.server);
    expect(relay).toStartWith("ws://127.0.0.1:");
    expect(host.started.some((line) => line.startsWith("server:srv:0 quiet-otter.a.link.test:443 measure"))).toBe(true);
    expect(transportPathInfo("srv")?.path).toBe("relay");
    transport.close();
  });
});
