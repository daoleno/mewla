import { describe, expect, open, test } from "nsh/test";
import nacl from "../core/vendor/nacl";
import { bytesToHex } from "../core/protocolCrypto";
import { linkRouteBinding } from "../core/link/routeUpgrade";
import { phoneHost } from "./testing/fixtures";

/**
 * Owner 11-23: Mewla for Mac and the web, on a computer reached through Mewla Relay, never moved onto the LAN; nobody
 * pairs again for it. A pairing the current release made by the hosted address hears the computer's signed route over
 * its connection and becomes a Relay route in place, so it looks for the LAN through tunnels pinned to that route's key.
 */
const APP = "./FakeDaemonApp.tsx";
const keys = nacl.sign.keyPair.fromSeed(new Uint8Array(32).fill(9));
const PUBLIC_KEY = bytesToHex(keys.publicKey);
const DAEMON_ID = "a".repeat(64);
const ROUTE = "3".repeat(32);
const PIN = "4".repeat(64);
const HOSTED = "wss://quiet-otter.a.link.test/ws";
const hostedPairing = {
  "mewla:v3:servers": JSON.stringify([{ id: "s1", name: "Desk", url: HOSTED, daemonId: DAEMON_ID, daemonPublicKey: PUBLIC_KEY }]),
  "mewla:v1:current_server_id": "s1",
  "mewla:onboarded": "1",
};

function routeFrame() {
  const candidates = [{ name: "region-a", url: HOSTED }];
  const issuedAtMs = Date.now();
  const binding = linkRouteBinding({ daemonId: DAEMON_ID, daemonPublicKey: PUBLIC_KEY, routeId: ROUTE, transportPin: PIN, issuedAtMs, candidates })!;
  const domain = new TextEncoder().encode("mewla-link-route-v1\u0000");
  const signed = new Uint8Array(domain.length + binding.length);
  signed.set(domain);
  signed.set(binding, domain.length);
  return { type: "link_route", version: 1, route_id: ROUTE, transport_pin: PIN, candidates, issued_at_ms: issuedAtMs, signature: bytesToHex(nacl.sign.detached(signed, keys.secretKey)) };
}

const stored = (storage: Record<string, string>) => (JSON.parse(storage["mewla:v3:servers"] ?? "[]") as Record<string, unknown>[])[0] ?? {};

describe("a computer paired by its hosted address tells its Mewla Relay route", () => {
  for (const platform of ["macos", "android"] as const) {
    test(`on ${platform}, the pairing becomes that route in place and looks for the LAN through a pinned tunnel`, async () => {
      const tunnels: string[] = [];
      let checked = false;
      const app = await open(APP, {
        platform,
        storage: hostedPairing,
        host: phoneHost({}, {
          "link.start": ({ key, host, port, pin }: { key: string; host: string; port: number; pin: string }) => {
            tunnels.push(`${key} ${host}:${port} ${pin === PIN ? "pinned" : "WRONG PIN"}`);
            // The route is taken only once a tunnel pinned to its key reached the address the pairing is at.
            if (key === "route-check:s1" && host === "quiet-otter.a.link.test" && port === 443) {
              checked = true;
              return { port: 4500, rttMs: 20 };
            }
            throw new Error("unreachable in this test");
          },
          "link.stop": () => null,
          "link.stopAll": () => null,
          "link.events": () => [],
        }),
      });
      await app.waitUntil(() => Number(app.storage()["fake:sockets"] ?? 0) >= 1, { timeout: 10_000 });
      expect(stored(app.storage()).transportKind ?? "manual").toBe("manual");
      // A page the person has open stays open: the upgrade is no visible step.
      if (platform === "android") {
        await app.press("id:home-cat");
        await app.waitFor("id:computer-page");
      }
      const before = app.texts();

      await app.host.send("app.daemonPush", routeFrame());
      await app.waitUntil(() => stored(app.storage()).transportKind === "link", { timeout: 5_000 });
      const server = stored(app.storage());
      expect(server.id).toBe("s1");
      expect(server.url).toBe(HOSTED);
      expect(server.transportPin).toBe(PIN);
      expect(server.linkRouteId).toBe(ROUTE);
      expect(checked).toBe(true);
      // The page stays as it was; only the path label starts to show (" · Relay" on the computer page).
      expect(app.texts().filter((text) => text !== " · Relay")).toEqual(before);
      if (platform === "android") expect(app.texts()).toContain(" · Relay");

      // Its LAN address, named next: saved on the upgraded pairing, and dialed only through a tunnel pinned to the key.
      await app.host.send("app.daemonPush", { type: "direct_addresses", addresses: [{ host: "192.168.1.20", port: 9443 }] });
      // The first check beside a live relay connection comes after `moveCheckMs` (15 s).
      await app.waitUntil(() => tunnels.some((line) => line.includes("192.168.1.20:9443")), { timeout: 25_000 });
      expect(tunnels.every((line) => line.endsWith("pinned"))).toBe(true);
      const candidates = stored(app.storage()).transportCandidates as { kind: string; url: string }[];
      expect(candidates.map((candidate) => candidate.kind).sort()).toEqual(["direct", "link"]);
      // One connection all along: no new pairing, no reconnect.
      expect(Number(app.storage()["fake:sockets"])).toBe(1);
    }, 60_000);
  }

  test("a host whose pinned tunnel does not reach the computer keeps the pairing it has, which works", async () => {
    const app = await open(APP, {
      platform: "macos",
      storage: hostedPairing,
      host: phoneHost({}, { "link.start": () => { throw new Error("no tunnel"); }, "link.stop": () => null, "link.stopAll": () => null, "link.events": () => [] }),
    });
    await app.waitUntil(() => Number(app.storage()["fake:sockets"] ?? 0) >= 1, { timeout: 10_000 });
    await app.host.send("app.daemonPush", routeFrame());
    await app.host.send("app.daemonPush", { type: "direct_addresses", addresses: [{ host: "192.168.1.20", port: 9443 }] });
    await app.wait(500);
    const server = stored(app.storage());
    expect(server.transportKind ?? "manual").toBe("manual");
    expect(server.url).toBe(HOSTED);
    expect(Number(app.storage()["fake:sockets"])).toBe(1);
  });

  test("in a browser, which carries no pinned tunnel, the pairing stays as it is", async () => {
    const app = await open(APP, { platform: "web", storage: hostedPairing, host: phoneHost({}) });
    await app.waitUntil(() => Number(app.storage()["fake:sockets"] ?? 0) >= 1, { timeout: 10_000 });
    await app.host.send("app.daemonPush", routeFrame());
    await app.wait(300);
    expect(stored(app.storage()).transportKind ?? "manual").toBe("manual");
  });
});
