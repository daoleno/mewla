import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import nacl from "./vendor/nacl";
import { daemonProves, noteDaemonProves, resetDaemonProofsForTests } from "./daemonProof";
import { bindingHost, buildBoundSignaturePayload, buildServerAssertionPayload, deviceBinding } from "./deviceAuthContract";
import { bytesToHex } from "./protocolCrypto";
import type { StoredServer } from "./storedServerContract";
import { FakeSocket, installMemoryPlatform } from "./testing/memoryPlatform";
import { Transport, transportTiming, type PathMoves } from "./transport";
import { AUTH_SUBPROTOCOL_PREFIX } from "./webSocketAuth";

// Security review 11-23, PR B item 6: the app checks the computer's identity on connect (its first frame signs the
// nonce of the app's own connect) and binds its connect to the host it goes to, so a host the app was made to talk to
// can neither answer as the computer nor hand the app's signature on to the real one.
const keys = nacl.sign.keyPair.fromSeed(new Uint8Array(32).fill(31));
const DAEMON_ID = "c3".repeat(32);
const server: StoredServer = { id: "s1", name: "Desk", url: "wss://desk.example/ws", daemonId: DAEMON_ID, daemonPublicKey: bytesToHex(keys.publicKey) };

const wait = (ms = 5) => new Promise((resolve) => setTimeout(resolve, ms));
let sockets: FakeSocket[];
const saved = { ...transportTiming };

beforeEach(() => {
  sockets = [];
  resetDaemonProofsForTests();
  installMemoryPlatform({
    openSocket: (url, protocols) => {
      const socket = new FakeSocket(url, protocols);
      sockets.push(socket);
      return socket;
    },
  });
  Object.assign(transportTiming, { firstRetryMs: 10, maxRetryMs: 40, checkMs: 10, silentLimitMs: 50 });
});
afterEach(() => {
  Object.assign(transportTiming, saved);
  resetDaemonProofsForTests();
});

/** The device header the socket was opened with. */
const headerOf = (socket: FakeSocket) => Buffer.from(socket.protocols[0].slice(AUTH_SUBPROTOCOL_PREFIX.length), "base64url").toString();
const nonceOf = (socket: FakeSocket) => headerOf(socket).split(":")[4];

/** The computer's first frame: its key's signature over `nonce` (daemon/server handleWS). */
function proofFrame(nonce: string, signer = keys, purpose = "mewla-connect-proof") {
  const timestamp = new Date().toISOString();
  const signature = nacl.sign.detached(buildServerAssertionPayload(purpose, DAEMON_ID, timestamp, nonce), signer.secretKey);
  return { type: "daemon_proof", purpose, timestamp, nonce, signature: bytesToHex(signature) };
}

async function opened(): Promise<{ transport: Transport; socket: FakeSocket; heard: string[] }> {
  const transport = new Transport(server);
  const heard: string[] = [];
  for (const type of ["connected", "brain_snapshot", "daemon_proof"]) transport.on(type, () => void heard.push(type));
  transport.connect();
  const deadline = Date.now() + 2_000;
  while (!sockets.length && Date.now() < deadline) await wait(1);
  const socket = sockets.at(-1)!;
  socket.open();
  await wait(1);
  return { transport, socket, heard };
}

describe("a computer that has proved before is held to its proof on connect", () => {
  beforeEach(() => noteDaemonProves(server.daemonPublicKey));

  test("its connect is bound to the host it goes to, and it is connected only once the first frame proves it", async () => {
    const { transport, socket, heard } = await opened();
    expect(headerOf(socket)).toStartWith("Device v2:");
    expect(Buffer.from(headerOf(socket).split(":")[5], "base64url").toString()).toBe("host:desk.example");
    // Open, not proved: nothing is sent and nothing is heard.
    expect(transport.state).toBe("connecting");
    expect(() => transport.send({ type: "ping" })).toThrow("not connected");
    socket.receive(proofFrame(nonceOf(socket)));
    socket.receive({ type: "brain_snapshot" });
    const deadline = Date.now() + 2_000;
    while (transport.state !== "connected" && Date.now() < deadline) await wait(1);
    expect(transport.state).toBe("connected");
    await wait(5);
    expect(heard).toEqual(["connected", "brain_snapshot"]);
    transport.close();
  });

  for (const [why, first] of [
    ["a recorded proof (another connect's nonce)", () => proofFrame("e".repeat(32))],
    ["another key's proof", (nonce: string) => proofFrame(nonce, nacl.sign.keyPair.fromSeed(new Uint8Array(32).fill(32)))],
    ["the /health answer, which anyone can have for any challenge", (nonce: string) => proofFrame(nonce, keys, "mewla-health-challenge")],
    ["no proof at all (a host in the middle that answers as the computer)", () => ({ type: "brain_snapshot" })],
  ] as const) {
    test(`${why}: the socket is given up, never connected`, async () => {
      const { transport, socket, heard } = await opened();
      socket.receive((first as (nonce: string) => object)(nonceOf(socket)));
      const deadline = Date.now() + 2_000;
      while (socket.readyState !== 3 && Date.now() < deadline) await wait(1);
      expect(socket.readyState).toBe(3);
      expect(heard).toEqual([]);
      expect(transport.problem).toContain("did not prove it is the computer this app paired");
      transport.close();
    });
  }
});

describe("a computer that has not proved yet (older mewla, or before its first connect)", () => {
  test("connects as before with an unbound connect, and is held to proofs once it sends a good one", async () => {
    const { transport, socket, heard } = await opened();
    expect(headerOf(socket)).toStartWith("Device v1:");
    expect(transport.state).toBe("connected");
    expect(await daemonProves(server.daemonPublicKey)).toBe(false);
    socket.receive(proofFrame(nonceOf(socket)));
    await wait(10);
    expect(await daemonProves(server.daemonPublicKey)).toBe(true);
    expect(heard).toEqual(["connected"]);
    transport.close();
  });

  test("a proof that does not verify proves nothing", async () => {
    const { transport, socket } = await opened();
    socket.receive(proofFrame("e".repeat(32)));
    await wait(10);
    expect(await daemonProves(server.daemonPublicKey)).toBe(false);
    transport.close();
  });
});

describe("the binding", () => {
  test("a bound header signs the same bytes as the daemon (daemon/auth TestBoundSignaturePayloadIsLineForLine)", () => {
    const payload = buildBoundSignaturePayload(" mewla-connect ", "AA".repeat(32), " phone ", "1700000000000", "BB".repeat(16), "host:desk.example");
    expect(new TextDecoder().decode(payload)).toBe(`mewla-device-v2\nmewla-connect\n${"aa".repeat(32)}\nphone\n1700000000000\n${"bb".repeat(16)}\nhost:desk.example`);
  });

  test("names the hostname the daemon reads from Host (daemon/server bindingHostname, line for line)", () => {
    expect(bindingHost("Desk.Example:443")).toBe("desk.example");
    expect(bindingHost("[FD7A:115C::1]:9876")).toBe("fd7a:115c::1");
    expect(bindingHost("desk.example.")).toBe("desk.example");
    expect(bindingHost("192.168.1.20")).toBe("192.168.1.20");
    expect(deviceBinding({ url: "wss://Desk.Example:8443/ws" })).toBe("host:desk.example");
    expect(deviceBinding({ url: "ws://[fd7a:115c::1]:9876/ws" })).toBe("host:fd7a:115c::1");
    expect(deviceBinding({ transportPin: "AB".repeat(32) })).toBe(`pin:${"ab".repeat(32)}`);
    expect(deviceBinding({ transportPin: "ab" })).toBeNull();
    expect(deviceBinding({ url: "not a url" })).toBeNull();
  });
});

// Brain 11-23 (no silent failures): a computer held to proofs that now runs a Mewla from before them (a downgrade)
// refuses the bound connect. The app says so, by name, where the connect error shows, not a bare "error".
describe("a computer held to proofs that runs an older Mewla now", () => {
  /** The computer's /health answer to the app's challenge, as v0.4.25 gives it (no `connect_proof`) or as now. */
  function healthAnswer(url: string, provesItself: boolean) {
    const challenge = new URL(url).searchParams.get("challenge")!;
    const timestamp = new Date().toISOString();
    const signature = nacl.sign.detached(buildServerAssertionPayload("mewla-health-challenge", DAEMON_ID, timestamp, challenge), keys.secretKey);
    return {
      status: "ok", daemon_id: DAEMON_ID, daemon_public_key: server.daemonPublicKey, version: provesItself ? "0.4.26" : "0.4.25",
      ...(provesItself ? { connect_proof: true } : {}),
      assertion_purpose: "mewla-health-challenge", assertion_timestamp: timestamp, assertion_nonce: challenge, assertion_signature: bytesToHex(signature),
    };
  }
  async function refusedConnect(provesItself: boolean): Promise<{ transport: Transport; asked: string[] }> {
    const asked: string[] = [];
    installMemoryPlatform({
      openSocket: (url, protocols) => {
        const socket = new FakeSocket(url, protocols);
        sockets.push(socket);
        return socket;
      },
      fetch: async (input) => {
        asked.push(String(input));
        return new Response(JSON.stringify(healthAnswer(String(input), provesItself)), { status: 200 });
      },
    });
    await noteDaemonProves(server.daemonPublicKey);
    const transport = new Transport(server);
    transport.connect();
    const deadline = Date.now() + 2_000;
    while (!sockets.length && Date.now() < deadline) await wait(1);
    // The older daemon refuses the bound connect at the handshake: the socket ends before it opens.
    sockets.at(-1)!.close(1006, "");
    return { transport, asked };
  }

  test("says the computer runs an older Mewla, and to update Mewla on it", async () => {
    const { transport, asked } = await refusedConnect(false);
    const deadline = Date.now() + 2_000;
    while (!transport.problem.includes("older") && Date.now() < deadline) await wait(1);
    expect(transport.problem).toBe("Desk runs an older Mewla. Update Mewla on Desk.");
    expect(asked[0]).toStartWith("https://desk.example/health?challenge=");
    // It stays the reason through the retries that follow.
    while (sockets.length < 2 && Date.now() < deadline + 2_000) await wait(1);
    sockets.at(-1)!.close(1006, "");
    await wait(5);
    expect(transport.problem).toBe("Desk runs an older Mewla. Update Mewla on Desk.");
    transport.close();
  });

  test("a computer that does prove itself (a connect that failed for another reason) is not called older", async () => {
    const { transport, asked } = await refusedConnect(true);
    const deadline = Date.now() + 500;
    while (!asked.length && Date.now() < deadline) await wait(1);
    await wait(20);
    expect(transport.problem).not.toContain("older");
    transport.close();
  });
});

// Security review 11-23, round 2 F3: the move's socket (relay → LAN) is held to a proof over its own nonce before the
// Transport moves onto it. Until then nothing goes out on it and nothing it sends is heard.
describe("a move to a direct path, for a computer held to its proof", () => {
  const PIN = "ab".repeat(32);
  const relayServer: StoredServer = { ...server, url: "wss://quiet-otter.m.mewla.ai/ws", transportKind: "link", transportPin: PIN };
  const DIRECT = "wss://127.0.0.1:47001/ws";
  beforeEach(() => {
    noteDaemonProves(server.daemonPublicKey);
    Object.assign(transportTiming, { silentLimitMs: 60_000, moveCheckMs: 2, moveCheckMaxMs: 8, moveHealthyChecks: 1, moveOpenLimitMs: 2_000, moveBackoffMs: 60_000, retireLimitMs: 2_000 });
  });

  async function until(condition: () => boolean, what: string): Promise<void> {
    const deadline = Date.now() + 2_000;
    while (!condition() && Date.now() < deadline) await wait(1);
    if (!condition()) throw new Error(`never: ${what}`);
  }

  /** Connected and proved on the relay, a move's socket to the direct path open beside it, a frame waiting to go out. */
  async function moving() {
    const record = { moved: 0, abandoned: 0, path: "relay" };
    const moves: PathMoves = {
      current: () => record.path,
      check: async () => ({ url: DIRECT, path: "lan" }),
      moved: () => {
        record.moved++;
        record.path = "lan";
      },
      abandoned: () => void record.abandoned++,
    };
    const transport = new Transport(relayServer, async (stored) => stored.url, moves);
    const heard: string[] = [];
    for (const type of ["connected", "moved", "brain_snapshot", "daemon_proof"]) transport.on(type, () => void heard.push(type));
    transport.connect();
    await until(() => sockets.length === 1, "the relay socket");
    const relay = sockets[0]!;
    relay.open();
    relay.receive(proofFrame(nonceOf(relay)));
    await until(() => transport.state === "connected", "connected on the relay");
    await until(() => sockets.length === 2, "the move's socket");
    const direct = sockets[1]!;
    expect(direct.url).toBe(DIRECT);
    expect(headerOf(direct)).toStartWith("Device v2:");
    expect(Buffer.from(headerOf(direct).split(":")[5], "base64url").toString()).toBe(`pin:${PIN}`);
    // Its own nonce, not the relay connect's.
    expect(nonceOf(direct)).not.toBe(nonceOf(relay));
    direct.open();
    await wait(5);
    return { transport, relay, direct, record, heard };
  }

  test("open is not enough: the Transport stays on the relay and sends nothing on it until it proves", async () => {
    const { transport, relay, direct, record } = await moving();
    expect(record.moved).toBe(0);
    transport.send({ type: "ping_while_moving" });
    expect(relay.sent.map((text) => JSON.parse(text).type)).toContain("ping_while_moving");
    expect(direct.sent).toEqual([]);
    transport.close();
  });

  test("a proof over its own nonce moves it, and what came behind the proof is heard after the move", async () => {
    const { transport, direct, record, heard } = await moving();
    direct.receive(proofFrame(nonceOf(direct)));
    direct.receive({ type: "brain_snapshot" });
    await until(() => record.moved === 1, "the move");
    await wait(10);
    expect(heard).toEqual(["connected", "moved", "brain_snapshot"]);
    transport.send({ type: "ping_after" });
    expect(direct.sent.map((text) => JSON.parse(text).type)).toContain("ping_after");
    transport.close();
  });

  for (const [why, first] of [
    ["the relay connect's proof played back (another nonce)", (_direct: FakeSocket, relay: FakeSocket) => proofFrame(nonceOf(relay))],
    ["no proof at all", () => ({ type: "brain_snapshot" })],
  ] as const) {
    test(`${why}: the move is given up, nothing ever went out on it, and the relay connection carries on`, async () => {
      const { transport, relay, direct, record, heard } = await moving();
      const queued = relay.sent.length;
      direct.receive((first as (direct: FakeSocket, relay: FakeSocket) => object)(direct, relay));
      direct.receive({ type: "brain_snapshot" });
      await until(() => record.abandoned === 1, "the move given up");
      expect(record.moved).toBe(0);
      expect(direct.sent).toEqual([]);
      expect(heard).toEqual(["connected"]);
      expect(transport.state).toBe("connected");
      transport.send({ type: "ping_after" });
      expect(relay.sent.length).toBe(queued + 1);
      transport.close();
    });
  }
});
