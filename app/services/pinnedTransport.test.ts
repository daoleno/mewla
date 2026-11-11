import { beforeEach, describe, expect, mock, test } from "bun:test";
import nacl from "tweetnacl";

const starts: Array<{
  key: string;
  host: string;
  port: number;
  pin: string;
  mode?: string;
}> = [];
const stops: string[] = [];
let failures = new Set<string>();

let hangingHosts = new Set<string>();

mock.module("expo-crypto", () => ({
  getRandomBytes: (size: number) => crypto.getRandomValues(new Uint8Array(size)),
}));

// Direct hosts get a port each so a test can tell which tunnel it was given.
const DIRECT_PORTS: Record<string, number> = {
  "192.168.1.20": 20001,
  "100.64.0.9": 20002,
  "10.0.2.2": 20003,
};

mock.module("../modules/link-transport/src", () => ({
  startPinnedTunnel: async (
    key: string,
    host: string,
    port: number,
    pin: string,
    mode?: string,
  ) => {
    starts.push({ key, host, port, pin, mode });
    if (hangingHosts.has(host)) {
      return new Promise(() => undefined);
    }
    if (failures.has(host)) {
      throw new Error(`offline ${host}`);
    }
    if (DIRECT_PORTS[host]) {
      return { port: DIRECT_PORTS[host], rttMs: 5 };
    }
    return {
      port: host.startsWith("fast.") ? 19002 : 19001,
      rttMs: host.startsWith("fast.") ? 10 : 40,
    };
  },
  stopPinnedTunnel: async (key: string) => {
    stops.push(key);
  },
}));

const pinnedTransportForDirect = await import("./pinnedTransport");
const {
  releasePairingLinkTransport,
  resolveCanonicalServerURL,
  resolvePairingLinkURL,
  resolveStoredServerURL,
} = pinnedTransportForDirect;
const { bytesToHex } = await import("./protocolCrypto");
const { buildServerAssertionPayload } = await import("./deviceAuthContract");

beforeEach(() => {
  starts.length = 0;
  stops.length = 0;
  failures = new Set();
  hangingHosts = new Set();
});

describe("shared Android/iOS pinned transport owner", () => {
  test("chooses measured Link candidates inside one StoredServer", async () => {
    const resolved = await resolveStoredServerURL({
      id: "one-current-server",
      name: "Workstation",
      url: "wss://slow.link.test/ws",
      daemonId: "1".repeat(64),
      daemonPublicKey: "2".repeat(64),
      transportKind: "link",
      transportPin: "3".repeat(64),
      linkRouteId: "4".repeat(32),
      transportCandidates: [
        { name: "a-slow", kind: "link", url: "wss://slow.link.test/ws" },
        { name: "b-fast", kind: "link", url: "wss://fast.link.test/ws" },
      ],
    });

    expect(starts.map((entry) => entry.host).sort()).toEqual([
      "fast.link.test",
      "slow.link.test",
    ]);
    expect(resolved).toBe("ws://127.0.0.1:19002/ws");
    expect(stops).toEqual(["server:one-current-server:0"]);
  });

  test("fails closed when every pinned candidate is offline", async () => {
    failures = new Set(["a.link.test", "b.link.test"]);
    await expect(
      resolveStoredServerURL({
        id: "offline-server",
        name: "Offline",
        url: "wss://a.link.test/ws",
        daemonId: "1".repeat(64),
        daemonPublicKey: "2".repeat(64),
        transportKind: "link",
        transportPin: "3".repeat(64),
        linkRouteId: "4".repeat(32),
        transportCandidates: [
          { name: "a", kind: "link", url: "wss://a.link.test/ws" },
          { name: "b", kind: "link", url: "wss://b.link.test/ws" },
        ],
      }),
    ).rejects.toThrow("Mewla Relay is offline");
  });

  test("fails closed instead of returning an unpinned Link endpoint", async () => {
    const linkServer = {
      id: "offline-canonical-server",
      name: "Offline",
      url: "wss://offline.link.test/ws",
      daemonId: "1".repeat(64),
      daemonPublicKey: "2".repeat(64),
      transportKind: "link" as const,
      transportPin: "3".repeat(64),
      linkRouteId: "4".repeat(32),
      transportCandidates: [
        {
          name: "offline",
          kind: "link" as const,
          url: "wss://offline.link.test/ws",
        },
      ],
    };
    failures = new Set(["offline.link.test"]);

    await expect(resolveCanonicalServerURL(linkServer)).rejects.toThrow(
      "Mewla Relay is offline",
    );
  });

  test("keeps a manual V1 endpoint on its original transport", async () => {
    await expect(
      resolveCanonicalServerURL({
        id: "manual",
        name: "Self-managed",
        url: "wss://cloudflare.example/ws",
        daemonId: "1".repeat(64),
        daemonPublicKey: "2".repeat(64),
        transportKind: "manual",
      }),
    ).resolves.toBe("wss://cloudflare.example/ws");
    expect(starts).toHaveLength(0);
  });

  test("rejects an incomplete Link transport instead of using its raw URL", async () => {
    await expect(
      resolveStoredServerURL({
        id: "needs-setup",
        name: "Needs setup",
        url: "wss://raw.link.test/ws",
        daemonId: "1".repeat(64),
        daemonPublicKey: "2".repeat(64),
        transportKind: "link",
      }),
    ).rejects.toThrow("Mewla Relay needs setup");
    expect(starts).toHaveLength(0);
  });

  test("releases the one-time enrollment proxy after pairing", async () => {
    await releasePairingLinkTransport("route-one");
    expect(stops).toEqual(["pair:route-one"]);
  });

  test("opens pairing transport on demand without an admission preflight", async () => {
    const resolved = await resolvePairingLinkURL({
      routeId: "4".repeat(32),
      transportPin: "3".repeat(64),
      url: "wss://admission.link.test/ws",
    });
    expect(resolved).toBe("ws://127.0.0.1:19001/ws");
    expect(starts).toEqual([
      {
        key: `pair:${"4".repeat(32)}`,
        host: "admission.link.test",
        port: 443,
        pin: "3".repeat(64),
        mode: "on-demand",
      },
    ]);
  });
});

describe("direct path beside the relay", () => {
  const { currentTransportPath, transportPathOfURL, directPathBecameAvailable, invalidateStoredServerTransport, transportFailed } =
    pinnedTransportForDirect;
  const daemonKeys = nacl.sign.keyPair.fromSeed(new Uint8Array(32).fill(7));
  const otherKeys = nacl.sign.keyPair.fromSeed(new Uint8Array(32).fill(9));
  const daemonId = "d".repeat(64);
  const daemonPublicKey = bytesToHex(daemonKeys.publicKey);
  let nextId = 0;
  let answer: "good" | "wrong-key" | "replay" | "legacy" | "error" = "good";
  let recorded: Record<string, unknown> | null = null;
  const requests: Array<{ url: string; headers: unknown }> = [];

  function signedAnswer(challenge: string, keys = daemonKeys) {
    const timestamp = new Date().toISOString();
    return {
      status: "ok",
      daemon_id: daemonId,
      daemon_public_key: bytesToHex(keys.publicKey),
      assertion_purpose: "mewla-health-challenge",
      assertion_timestamp: timestamp,
      assertion_nonce: challenge,
      assertion_signature: bytesToHex(
        nacl.sign.detached(
          buildServerAssertionPayload("mewla-health-challenge", daemonId, timestamp, challenge),
          keys.secretKey,
        ),
      ),
    };
  }

  beforeEach(() => {
    answer = "good";
    recorded = null;
    requests.length = 0;
    globalThis.fetch = (async (input: unknown, init?: { headers?: unknown }) => {
      const url = String(input);
      requests.push({ url, headers: init?.headers });
      const challenge = new URL(url).searchParams.get("challenge") ?? "";
      const respond = (body: unknown, status = 200) =>
        new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
      switch (answer) {
        case "error":
          return respond({ error: "no" }, 500);
        case "wrong-key":
          return respond(signedAnswer(challenge, otherKeys));
        case "replay":
          // A recorder replays one genuine answer to every later challenge.
          recorded ??= signedAnswer("00".repeat(16));
          return respond(recorded);
        case "legacy":
          // A daemon that predates challenges ignores it and signs its own nonce.
          return respond({ ...signedAnswer("11".repeat(16)), assertion_purpose: "mewla-health" });
        default:
          return respond(signedAnswer(challenge));
      }
    }) as typeof fetch;
  });

  function linkServer(addresses: Array<{ host: string; port: number }>) {
    const id = `direct-${nextId++}`;
    return {
      id,
      name: "Workstation",
      url: "wss://relay.link.test/ws",
      daemonId,
      daemonPublicKey,
      transportKind: "link" as const,
      transportPin: "3".repeat(64),
      linkRouteId: "4".repeat(32),
      transportCandidates: [
        { name: "Mewla Relay", kind: "link" as const, url: "wss://relay.link.test/ws" },
        ...addresses.map((address) => ({
          name: "LAN",
          kind: "direct" as const,
          url: `wss://${address.host}:${address.port}/ws`,
        })),
      ],
    };
  }

  test("a verified LAN address is used, and the relay stays open behind it", async () => {
    const server = linkServer([{ host: "192.168.1.20", port: 9876 }]);
    const resolved = await resolveStoredServerURL(server);

    expect(resolved).toBe("ws://127.0.0.1:20001/ws");
    expect(currentTransportPath(server.id)).toBe("lan");
    expect(starts.find((entry) => entry.host === "192.168.1.20")).toMatchObject({
      pin: "3".repeat(64),
      mode: "measure",
      port: 9876,
    });
    // Nothing closes the relay tunnel: it is the instant fallback.
    expect(stops).not.toContain(`server:${server.id}:0`);
    await invalidateStoredServerTransport(server);
  });

  test("a resolved URL names the path it is carried on, even after the choice moves", async () => {
    const server = linkServer([{ host: "192.168.1.20", port: 9876 }]);
    const direct = await resolveStoredServerURL(server);
    expect(transportPathOfURL(server.id, direct)).toBe("lan");
    expect(transportPathOfURL(server.id, "ws://127.0.0.1:1/ws")).toBeNull();
    expect(transportPathOfURL("never-resolved", direct)).toBeNull();
    // The relay tunnel kept open behind it is a different path.
    expect(transportPathOfURL(server.id, "ws://127.0.0.1:19001/ws")).toBe("relay");
    await invalidateStoredServerTransport(server);
  });

  test("the challenge goes to the pinned loopback tunnel with no device header", async () => {
    const server = linkServer([{ host: "192.168.1.20", port: 9876 }]);
    await resolveStoredServerURL(server);

    expect(requests).toHaveLength(1);
    expect(requests[0].url).toMatch(/^http:\/\/127\.0\.0\.1:20001\/health\?challenge=[0-9a-f]{32}$/);
    expect(JSON.stringify(requests[0].headers)).not.toMatch(/authorization/i);
    await invalidateStoredServerTransport(server);
  });

  for (const bad of ["wrong-key", "replay", "legacy", "error"] as const) {
    test(`an answerer that fails the challenge (${bad}) is closed and the relay is used`, async () => {
      answer = bad;
      const server = linkServer([{ host: "192.168.1.20", port: 9876 }]);
      const resolved = await resolveStoredServerURL(server);

      expect(resolved).toBe("ws://127.0.0.1:19001/ws");
      expect(currentTransportPath(server.id)).toBe("relay");
      expect(stops).toContain(`server:${server.id}:direct:192.168.1.20:9876`);
      await invalidateStoredServerTransport(server);
    });
  }

  test("a direct path that worked before is re-verified past the short grace", async () => {
    const server = linkServer([{ host: "192.168.1.20", port: 9876 }]);
    await resolveStoredServerURL(server);
    expect(currentTransportPath(server.id)).toBe("lan");

    // Its verification has expired and the answer is now slow, as on a
    // loaded phone: slower than the grace given to a path never seen working.
    const realFetch = globalThis.fetch;
    globalThis.fetch = (async (...args: Parameters<typeof fetch>) => {
      await new Promise((done) => setTimeout(done, 400));
      return realFetch(...args);
    }) as typeof fetch;
    const realNow = Date.now;
    Date.now = () => realNow() + 31_000;
    try {
      expect(await resolveStoredServerURL(server)).toBe("ws://127.0.0.1:20001/ws");
    } finally {
      Date.now = realNow;
      globalThis.fetch = realFetch;
    }
    expect(currentTransportPath(server.id)).toBe("lan");
    await invalidateStoredServerTransport(server);
  });

  test("overlapping resolves share one choice and never stop each other's tunnel", async () => {
    const server = linkServer([
      { host: "192.168.1.20", port: 9876 },
      { host: "100.64.0.9", port: 9876 },
    ]);
    const results = await Promise.all(
      Array.from({ length: 5 }, () => resolveStoredServerURL(server)),
    );
    expect(new Set(results).size).toBe(1);
    expect(starts.filter((entry) => entry.host === "192.168.1.20")).toHaveLength(1);
    expect(starts.filter((entry) => entry.host === "100.64.0.9")).toHaveLength(1);
    await new Promise((done) => setTimeout(done, 30));
    const chosen = results[0].includes("20001") ? "192.168.1.20" : "100.64.0.9";
    expect(stops).not.toContain(`server:${server.id}:direct:${chosen}:9876`);
    await invalidateStoredServerTransport(server);
  });

  test("a slow re-check of the path in use keeps it and its tunnel", async () => {
    const server = linkServer([{ host: "192.168.1.20", port: 9876 }]);
    await resolveStoredServerURL(server);
    stops.length = 0;

    const realFetch = globalThis.fetch;
    globalThis.fetch = (async (...args: Parameters<typeof fetch>) => {
      await new Promise((done) => setTimeout(done, 1500));
      return realFetch(...args);
    }) as typeof fetch;
    const realNow = Date.now;
    Date.now = () => realNow() + 31_000;
    try {
      expect(await resolveStoredServerURL(server)).toBe("ws://127.0.0.1:20001/ws");
    } finally {
      Date.now = realNow;
      globalThis.fetch = realFetch;
    }
    await new Promise((done) => setTimeout(done, 400));
    expect(currentTransportPath(server.id)).toBe("lan");
    expect(stops).not.toContain(`server:${server.id}:direct:192.168.1.20:9876`);
    await invalidateStoredServerTransport(server);
  }, 10_000);

  test("the path in use is re-checked on its own, not swapped for a faster address", async () => {
    const server = linkServer([
      { host: "192.168.1.20", port: 9876 },
      { host: "100.64.0.9", port: 9876 },
    ]);
    const first = await resolveStoredServerURL(server);
    const inUsePort = first.includes("20001") ? "20001" : "20002";
    stops.length = 0;

    const realFetch = globalThis.fetch;
    globalThis.fetch = (async (...args: Parameters<typeof fetch>) => {
      if (String(args[0]).includes(`:${inUsePort}/`)) {
        await new Promise((done) => setTimeout(done, 300));
      }
      return realFetch(...args);
    }) as typeof fetch;
    const realNow = Date.now;
    Date.now = () => realNow() + 31_000;
    try {
      expect(await resolveStoredServerURL(server)).toBe(first);
    } finally {
      Date.now = realNow;
      globalThis.fetch = realFetch;
    }
    expect(stops).toEqual([]);
    await invalidateStoredServerTransport(server);
  });

  test("an unreachable address does not hold up the relay", async () => {
    hangingHosts = new Set(["192.168.1.20"]);
    const server = linkServer([{ host: "192.168.1.20", port: 9876 }]);
    const began = Date.now();
    const resolved = await resolveStoredServerURL(server);

    expect(resolved).toBe("ws://127.0.0.1:19001/ws");
    expect(Date.now() - began).toBeLessThan(700);
    // The hung dial is closed once its cap passes.
    await new Promise((done) => setTimeout(done, 1400));
    expect(stops).toContain(`server:${server.id}:direct:192.168.1.20:9876`);
    // And it is not tried again straight away.
    starts.length = 0;
    await invalidateStoredServerTransport(server);
    const again = await resolveStoredServerURL(server);
    expect(again).toBe("ws://127.0.0.1:19001/ws");
    expect(starts.some((entry) => entry.host === "192.168.1.20")).toBe(false);
    await invalidateStoredServerTransport(server);
  }, 10_000);

  test("only private-network IP addresses are ever dialed", async () => {
    const server = linkServer([
      { host: "evil.example", port: 9876 },
      { host: "8.8.8.8", port: 9876 },
      { host: "127.0.0.1", port: 9876 },
      { host: "172.32.0.1", port: 9876 },
    ]);
    const resolved = await resolveStoredServerURL(server);

    expect(resolved).toBe("ws://127.0.0.1:19001/ws");
    expect(starts.map((entry) => entry.host)).toEqual(["relay.link.test"]);
    expect(requests).toHaveLength(0);
    await invalidateStoredServerTransport(server);
  });

  test("whatever is chosen resolves to the phone's own loopback", async () => {
    for (const addresses of [[], [{ host: "100.64.0.9", port: 9876 }], [{ host: "10.0.2.2", port: 9876 }]]) {
      const server = linkServer(addresses);
      const resolved = new URL(await resolveStoredServerURL(server));
      expect(resolved.protocol).toBe("ws:");
      expect(resolved.hostname).toBe("127.0.0.1");
      await invalidateStoredServerTransport(server);
    }
  });

  test("a lost direct address falls back to the relay that was kept open", async () => {
    const server = linkServer([{ host: "192.168.1.20", port: 9876 }]);
    await resolveStoredServerURL(server);
    expect(currentTransportPath(server.id)).toBe("lan");

    // The address stops answering; the connection drops.
    failures = new Set(["192.168.1.20"]);
    stops.length = 0;
    await transportFailed(server);
    expect(stops).toEqual([`server:${server.id}:direct:192.168.1.20:9876`]);

    const resolved = await resolveStoredServerURL(server);
    expect(resolved).toBe("ws://127.0.0.1:19001/ws");
    expect(currentTransportPath(server.id)).toBe("relay");
    expect(stops).not.toContain(`server:${server.id}:0`);
    await invalidateStoredServerTransport(server);
  });

  test("a phone on the relay moves to a direct address that becomes reachable", async () => {
    failures = new Set(["192.168.1.20"]);
    const server = linkServer([{ host: "192.168.1.20", port: 9876 }]);
    await resolveStoredServerURL(server);
    expect(currentTransportPath(server.id)).toBe("relay");

    expect(await directPathBecameAvailable(server)).toBe(false);

    // It came back, but a failed address is left alone for a short while.
    failures = new Set();
    expect(await directPathBecameAvailable(server)).toBe(false);
    const realNow = Date.now;
    Date.now = () => realNow() + 21_000;
    try {
      expect(await directPathBecameAvailable(server)).toBe(true);
    } finally {
      Date.now = realNow;
    }
    expect(await resolveStoredServerURL(server)).toBe("ws://127.0.0.1:20001/ws");
    expect(currentTransportPath(server.id)).toBe("lan");
    await invalidateStoredServerTransport(server);
  });

  test("the nearest of several verified addresses wins and the rest are closed", async () => {
    const server = linkServer([
      { host: "192.168.1.20", port: 9876 },
      { host: "100.64.0.9", port: 9876 },
    ]);
    const resolved = await resolveStoredServerURL(server);
    expect(["ws://127.0.0.1:20001/ws", "ws://127.0.0.1:20002/ws"]).toContain(resolved);
    await new Promise((done) => setTimeout(done, 20));
    const winner = resolved.includes("20001") ? "192.168.1.20" : "100.64.0.9";
    const loser = winner === "192.168.1.20" ? "100.64.0.9" : "192.168.1.20";
    expect(stops).toContain(`server:${server.id}:direct:${loser}:9876`);
    expect(stops).not.toContain(`server:${server.id}:direct:${winner}:9876`);
    await invalidateStoredServerTransport(server);
  });

  describe("the live path the app shows", () => {
    const { subscribeTransportPath, transportPathInfo } = pinnedTransportForDirect;
    // What a subscriber would have rendered, one entry per notification.
    function watch(serverId: string) {
      const seen: Array<string | null> = [];
      const stop = subscribeTransportPath(() => seen.push(transportPathInfo(serverId)?.path ?? null));
      return { seen, stop };
    }

    test("a LAN address is announced with its address and measured round trip", async () => {
      const server = linkServer([{ host: "192.168.1.20", port: 9876 }]);
      expect(transportPathInfo(server.id)).toBeNull();
      const { seen, stop } = watch(server.id);
      await resolveStoredServerURL(server);
      expect(seen).toEqual(["lan"]);
      expect(transportPathInfo(server.id)).toEqual({ path: "lan", host: "192.168.1.20", rttMs: 5 });
      stop();
      await invalidateStoredServerTransport(server);
    });

    test("Tailscale and the relay are told apart", async () => {
      const tailnet = linkServer([{ host: "100.64.0.9", port: 9876 }]);
      await resolveStoredServerURL(tailnet);
      expect(transportPathInfo(tailnet.id)).toEqual({ path: "tailscale", host: "100.64.0.9", rttMs: 5 });
      await invalidateStoredServerTransport(tailnet);

      const relayOnly = linkServer([]);
      await resolveStoredServerURL(relayOnly);
      expect(transportPathInfo(relayOnly.id)).toEqual({ path: "relay", host: null, rttMs: 40 });
      await invalidateStoredServerTransport(relayOnly);
    });

    test("relay to direct: the path moves only when the connection does", async () => {
      failures = new Set(["192.168.1.20"]);
      const server = linkServer([{ host: "192.168.1.20", port: 9876 }]);
      const { seen, stop } = watch(server.id);
      await resolveStoredServerURL(server);
      expect(seen).toEqual(["relay"]);

      failures = new Set();
      const realNow = Date.now;
      Date.now = () => realNow() + 21_000;
      try {
        expect(await directPathBecameAvailable(server)).toBe(true);
      } finally {
        Date.now = realNow;
      }
      // Verified, but the live connection is still on the relay.
      expect(transportPathInfo(server.id)?.path).toBe("relay");
      expect(seen).toEqual(["relay"]);

      await resolveStoredServerURL(server);
      expect(seen).toEqual(["relay", "lan"]);
      expect(transportPathInfo(server.id)?.host).toBe("192.168.1.20");
      stop();
      await invalidateStoredServerTransport(server);
    });

    test("direct lost: nothing is claimed while it fails, then the relay", async () => {
      const server = linkServer([{ host: "192.168.1.20", port: 9876 }]);
      await resolveStoredServerURL(server);
      const { seen, stop } = watch(server.id);

      failures = new Set(["192.168.1.20"]);
      await transportFailed(server);
      expect(transportPathInfo(server.id)).toBeNull();
      await resolveStoredServerURL(server);
      expect(seen).toEqual([null, "relay"]);
      expect(transportPathInfo(server.id)?.rttMs).toBe(40);
      stop();
      await invalidateStoredServerTransport(server);
    });

    for (const bad of ["wrong-key", "replay", "legacy", "error"] as const) {
      test(`a failed proof (${bad}) never shows a direct path`, async () => {
        answer = bad;
        const server = linkServer([{ host: "192.168.1.20", port: 9876 }]);
        const { seen, stop } = watch(server.id);
        await resolveStoredServerURL(server);
        expect(seen).toEqual(["relay"]);
        expect(transportPathInfo(server.id)?.path).toBe("relay");
        stop();
        await invalidateStoredServerTransport(server);
      });
    }

    test("forgetting a computer's transport clears its path", async () => {
      const server = linkServer([{ host: "192.168.1.20", port: 9876 }]);
      await resolveStoredServerURL(server);
      const { seen, stop } = watch(server.id);
      await invalidateStoredServerTransport(server);
      expect(seen).toEqual([null]);
      expect(transportPathInfo(server.id)).toBeNull();
      stop();
    });

    test("a snapshot that did not change is not announced again", async () => {
      const server = linkServer([{ host: "192.168.1.20", port: 9876 }]);
      await resolveStoredServerURL(server);
      const first = transportPathInfo(server.id);
      const { seen, stop } = watch(server.id);
      await resolveStoredServerURL(server);
      expect(seen).toEqual([]);
      expect(transportPathInfo(server.id)).toBe(first);
      stop();
      await invalidateStoredServerTransport(server);
    });
  });
});
