import { afterAll, beforeEach, describe, expect, mock, test } from "bun:test";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import nacl from "tweetnacl";

// A Mewla Link /pair answer can be lost after the daemon trusted the phone:
// the stream resets, or a repeated scan finds the one-time admission spent
// and the native pinned proxy resets the local socket. These tests drive
// importConnection against a fake daemon to lock in that the app confirms
// with its device key and connects instead of reporting a failure.

// Bun shares module mocks across every file in one `bun test` run, and other
// files replace ./auth and ./storage with fixtures. Real device signing must
// run here, so the suite re-runs this file alone in a fresh process.
const isolated = process.env.MEWLA_PAIRING_RECOVERY_ISOLATED === "1";

if (!isolated) {
  test("Mewla Link pairing recovery passes in its own process", () => {
    const run = Bun.spawnSync([process.execPath, "test", import.meta.path], {
      cwd: `${import.meta.dir}/..`,
      env: { ...process.env, MEWLA_PAIRING_RECOVERY_ISOLATED: "1" },
      stdout: "pipe",
      stderr: "pipe",
    });
    const output = `${run.stdout.toString()}${run.stderr.toString()}`;
    if (run.exitCode !== 0) throw new Error(output);
    expect(output).toContain(" 0 fail");
  }, 30_000);
} else {
  const secureValues = new Map<string, string>();
  const tunnelStarts: string[] = [];
  const tunnelStops: string[] = [];
  let tunnelPort = 0;

  mock.module("react-native", () => ({
    Platform: { OS: "android" },
    Alert: {
      alert: () => {
        throw new Error("Unexpected native prompt in injected test");
      },
    },
  }));
  mock.module("expo-crypto", () => ({
    getRandomBytes: (length: number) =>
      crypto.getRandomValues(new Uint8Array(length)),
    randomUUID: () => crypto.randomUUID(),
  }));
  mock.module("expo-device", () => ({
    deviceName: "Recovery phone",
    modelName: "Recovery phone",
  }));
  mock.module("expo-secure-store", () => ({
    getItemAsync: async (key: string) => secureValues.get(key) || null,
    setItemAsync: async (key: string, value: string) => {
      secureValues.set(key, value);
    },
  }));
  mock.module("@react-native-async-storage/async-storage", () => ({
    default: {
      getItem: async () => null,
      setItem: async () => undefined,
    },
  }));
  mock.module("./storage", () => ({
    getServers: async () => [],
    markOnboarded: async () => undefined,
    saveServer: async (input: Record<string, unknown>) => ({
      id: "recovered-server",
      ...input,
    }),
    setServerAutoConnect: async () => undefined,
  }));
  // Every import here passes confirmScope; keep the prompt module (and its
  // storage reads) out of the graph so other files' storage mocks cannot clash.
  mock.module("./confirmPairingScope", () => ({
    confirmPairingScope: async () => true,
    confirmPairingScopeFor: async () => true,
  }));
  mock.module("../modules/link-transport/src", () => ({
    startPinnedTunnel: async (key: string, _host: string, _port: number, _pin: string, mode: string) => {
      expect(mode).toBe("on-demand");
      tunnelStarts.push(key);
      return { port: tunnelPort, rttMs: 0 };
    },
    stopPinnedTunnel: async (key: string) => {
      tunnelStops.push(key);
    },
  }));

  const { bytesToHex, hexToBytes } = await import("./protocolCrypto");
  const { buildServerAssertionPayload, buildSignaturePayload } = await import(
    "./deviceAuthContract"
  );
  const { importConnection } = await import("./importConnection");

  const daemonKeys = nacl.sign.keyPair.fromSeed(new Uint8Array(32).fill(5));
  const daemonId = "d".repeat(64);
  const daemonPublicKey = bytesToHex(daemonKeys.publicKey);
  const routeId = "a".repeat(32);
  const stableURL = `wss://swift-hill-a9de.m.link.test/ws`;

  type PairMode = "trust-then-drop" | "drop-before-trust" | "code-used";
  let pairMode: PairMode = "trust-then-drop";
  const trusted = new Map<string, string>();
  let daemon: Server;

  function assertion(purpose: string) {
    const timestamp = Date.now().toString();
    const nonce = bytesToHex(crypto.getRandomValues(new Uint8Array(16)));
    return {
      assertion_timestamp: timestamp,
      assertion_nonce: nonce,
      assertion_signature: bytesToHex(
        nacl.sign.detached(
          buildServerAssertionPayload(purpose, daemonId, timestamp, nonce),
          daemonKeys.secretKey,
        ),
      ),
    };
  }

  function authorizedDevice(header: string | undefined): string | null {
    const [, deviceId, signedDaemon, timestamp, nonce, signature] =
      (header || "").replace(/^Device v1:/, "v1:").split(":");
    const publicKey = trusted.get(deviceId || "");
    if (!publicKey || signedDaemon !== daemonId) return null;
    const valid = nacl.sign.detached.verify(
      buildSignaturePayload("mewla-probe", daemonId, deviceId, timestamp, nonce),
      hexToBytes(signature),
      hexToBytes(publicKey),
    );
    return valid ? deviceId : null;
  }

  function pairingLink(): string {
    const payload = {
      v: 2,
      d: daemonId,
      k: daemonPublicKey,
      e: "e".repeat(64),
      r: routeId,
      p: "f".repeat(64),
      c: [
        {
          n: "main",
          a: "wss://0123456789abcdef0123456789abcdef.m.link.test/ws",
          s: stableURL,
        },
      ],
      x: Date.now() + 60_000,
      z: "",
    };
    const binding = new TextEncoder().encode(
      [
        "2",
        payload.d,
        payload.k,
        payload.e,
        payload.r,
        payload.p,
        payload.x.toString(),
        "main",
        payload.c[0].a,
        payload.c[0].s,
      ].join("\n"),
    );
    const domain = new TextEncoder().encode("mewla-link-pairing-v2\u0000");
    const signed = new Uint8Array(domain.length + binding.length);
    signed.set(domain);
    signed.set(binding, domain.length);
    payload.z = bytesToHex(nacl.sign.detached(signed, daemonKeys.secretKey));
    return `mewla://settings?v=2&p=${Buffer.from(JSON.stringify(payload)).toString("base64url")}`;
  }

  daemon = createServer((request, response) => {
    let body = "";
    request.on("data", (chunk) => {
      body += chunk;
    });
    request.on("end", () => {
      if (request.method === "POST" && request.url === "/pair") {
        const pair = JSON.parse(body) as {
          device_id: string;
          device_public_key: string;
        };
        if (pairMode === "code-used") {
          response.writeHead(401).end("invalid pairing token\n");
          return;
        }
        if (pairMode === "trust-then-drop") {
          trusted.set(pair.device_id, pair.device_public_key);
        }
        // The reply never reaches the phone.
        request.socket.destroy();
        return;
      }
      if (request.method === "GET" && request.url === "/auth-check") {
        const deviceId = authorizedDevice(request.headers.authorization);
        if (!deviceId) {
          response.writeHead(401).end("unauthorized\n");
          return;
        }
        response.writeHead(200, { "Content-Type": "application/json" }).end(
          JSON.stringify({
            ok: true,
            device_id: deviceId,
            daemon_id: daemonId,
            daemon_public_key: daemonPublicKey,
            ...assertion("mewla-probe"),
          }),
        );
        return;
      }
      response.writeHead(404).end();
    });
  });
  await new Promise<void>((resolve) => daemon.listen(0, "127.0.0.1", resolve));
  const daemonPort = (daemon.address() as AddressInfo).port;

  afterAll(() => {
    daemon.close();
    mock.restore();
  });

  beforeEach(() => {
    tunnelStarts.length = 0;
    tunnelStops.length = 0;
    trusted.clear();
    tunnelPort = daemonPort;
  });

  function expectNoInternalDetails(message: string) {
    expect(message).not.toContain("127.0.0.1");
    expect(message.toLowerCase()).not.toContain("cleartext");
    expect(message).not.toContain("/health");
  }

  describe("Mewla Link pairing recovery", () => {
    test("a /pair reply lost after the daemon trusted the phone still connects", async () => {
      pairMode = "trust-then-drop";
      const progress: string[] = [];
      const imported = await importConnection(pairingLink(), {
        confirmScope: async () => true,
        onProgress: (message) => progress.push(message),
      });

      // Bun shares ./storage mocks across files; the saved server echoes its
      // input under every mock, so assert on what importConnection returns.
      expect(imported).toEqual(
        expect.objectContaining({
          url: stableURL,
          daemonId,
          daemonPublicKey,
          transportKind: "link",
          linkRouteId: routeId,
        }),
      );
      expect(tunnelStarts).toEqual([`pair:${routeId}`, `pair-confirm:${routeId}`]);
      expect(tunnelStops).toEqual([`pair:${routeId}`, `pair-confirm:${routeId}`]);
      expect(progress[0]).toBe("Connecting to swift-hill-a9de through Mewla…");
      progress.forEach(expectNoInternalDetails);
    });

    test("a rescan of a spent code connects when this phone is already trusted", async () => {
      pairMode = "code-used";
      const link = pairingLink();
      const identity = await (await import("./auth")).getOrCreateLocalDeviceIdentity();
      trusted.set(identity.deviceId, identity.publicKeyHex);

      const imported = await importConnection(link, { confirmScope: async () => true });

      expect(imported).toEqual(
        expect.objectContaining({ url: stableURL, transportKind: "link" }),
      );
    });

    test("a spent code on an untrusted phone asks for a fresh code", async () => {
      pairMode = "code-used";
      const error = await importConnection(pairingLink(), {
        confirmScope: async () => true,
      }).catch((caught: Error) => caught);

      expect(error).toBeInstanceOf(Error);
      expect((error as Error).message).toContain("already used or has expired");
      expect((error as Error).message).toContain("mewla pair");
      expectNoInternalDetails((error as Error).message);
    });

    test("a stream that broke before the daemon trusted the phone asks for a fresh code", async () => {
      pairMode = "drop-before-trust";
      const error = await importConnection(pairingLink(), {
        confirmScope: async () => true,
      }).catch((caught: Error) => caught);

      expect((error as Error).message).toContain("mewla pair");
      expectNoInternalDetails((error as Error).message);
    });

    test("an unreachable computer names the remote hop, not loopback internals", async () => {
      const closed = createServer();
      await new Promise<void>((resolve) => closed.listen(0, "127.0.0.1", resolve));
      tunnelPort = (closed.address() as AddressInfo).port;
      await new Promise<void>((resolve) => closed.close(() => resolve()));

      const error = await importConnection(pairingLink(), {
        confirmScope: async () => true,
      }).catch((caught: Error) => caught);

      expect((error as Error).message).toContain("couldn't reach this computer");
      expect((error as Error).message).toContain("mewla doctor");
      expectNoInternalDetails((error as Error).message);
      expect(tunnelStops).toEqual([`pair:${routeId}`, `pair-confirm:${routeId}`]);
    });
  });

  describe("direct pairing codes", () => {
    test("a code naming loopback explains that it points at the phone", async () => {
      const { enrollWithDaemon } = await import("./pairing");
      const closed = createServer();
      await new Promise<void>((resolve) => closed.listen(0, "127.0.0.1", resolve));
      const port = (closed.address() as AddressInfo).port;
      await new Promise<void>((resolve) => closed.close(() => resolve()));

      const error = await enrollWithDaemon({
        serverUrl: `https://127.0.0.1:${port}`,
        daemonId,
        daemonPublicKey,
        enrollmentToken: "e".repeat(64),
        transport: "direct",
      }).catch((caught: Error) => caught);

      expect((error as Error).message).toContain("points at 127.0.0.1");
      expect((error as Error).message).toContain("mewla pair");
      expect((error as Error).message.toLowerCase()).not.toContain("cleartext");
    });
  });
}
