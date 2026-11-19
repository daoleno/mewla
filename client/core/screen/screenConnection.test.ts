import { describe, expect, test } from "bun:test";
import { createHash, generateKeyPairSync, sign } from "node:crypto";
import type { StoredServer } from "../storedServerContract";
import { FakeSocket, installMemoryPlatform } from "../testing/memoryPlatform";
import { listDisplays, ScreenConnection, SCREEN_TOO_OLD } from "./screen";
import { screenDtlsPurpose, sdpFingerprints } from "./screenIdentity";
import type { ScreenSignal } from "./screenViewerProtocol";

const SDP = "v=0\r\na=fingerprint:sha-256 AB:CD:01\r\nm=video\r\n";

function computer(id: string) {
  const { publicKey, privateKey } = generateKeyPairSync("ed25519");
  const raw = publicKey.export({ format: "der", type: "spki" }).subarray(-32);
  const daemonId = createHash("sha256").update(raw).digest("hex");
  const server = { id, name: "Desk", url: "ws://desk.test:9877/ws", daemonId, daemonPublicKey: Buffer.from(raw).toString("hex") } as StoredServer;
  const assertion = async (sdp: string) => {
    const timestamp = new Date().toISOString();
    const nonce = "0123456789abcdef0123456789abcdef";
    const purpose = await screenDtlsPurpose(sdpFingerprints(sdp));
    return { timestamp, nonce_hex: nonce, signature_hex: sign(null, Buffer.from([purpose, daemonId, timestamp, nonce].join("\n")), privateKey).toString("hex") };
  };
  return { server, assertion };
}

function connect(server: StoredServer) {
  const sockets: FakeSocket[] = [];
  installMemoryPlatform({
    openSocket: (url, protocols) => {
      const socket = new FakeSocket(url, protocols ?? []);
      sockets.push(socket);
      return socket;
    },
    fetch: async () => new Response(JSON.stringify({ displays: [], available: true }), { headers: { "content-type": "application/json" } }),
  });
  const signals: ScreenSignal[] = [];
  const closed: { reason: string | null; outdated: boolean }[] = [];
  const connection = new ScreenConnection(server, {
    open() {},
    ready() {},
    rtt() {},
    signal: (s) => signals.push(s),
    closed: (reason, outdated) => closed.push({ reason, outdated }),
  });
  return { connection, sockets, signals, closed };
}

const settle = () => new Promise((resolve) => setTimeout(resolve, 20));

describe("ScreenConnection", () => {
  test("an offer reaches the viewer page only with the computer's signature over its certificates", async () => {
    const { server, assertion } = computer("a");
    const good = connect(server);
    await good.connection.connect();
    expect(good.sockets[0].url).toContain("video=webrtc");
    good.sockets[0].open();
    good.sockets[0].receive({ type: "rtc", sdp_type: "offer", sdp: SDP, assertion: await assertion(SDP) });
    await settle();
    expect(good.signals.map((s) => s.type)).toEqual(["rtc"]);
    expect(good.closed).toEqual([]);
    good.connection.close();

    const forged = connect(server);
    await forged.connection.connect();
    forged.sockets[0].open();
    forged.sockets[0].receive({ type: "rtc", sdp_type: "offer", sdp: SDP.replace("01", "02"), assertion: await assertion(SDP) });
    forged.sockets[0].receive({ type: "ice", candidate: { candidate: "candidate:1 1 udp 1 1.2.3.4 9 typ host" } });
    await settle();
    expect(forged.signals).toEqual([]);
    expect(forged.closed).toEqual([{ reason: "The computer's video could not be verified", outdated: false }]);
  });

  test("an older computer's RFB bytes end the view: it says to update Mewla there", async () => {
    const { server } = computer("b");
    const old = connect(server);
    await old.connection.connect();
    old.sockets[0].open();
    old.sockets[0].onmessage?.({ data: new TextEncoder().encode("RFB 003.008\n").buffer });
    expect(old.closed).toEqual([{ reason: SCREEN_TOO_OLD, outdated: true }]);
  });

  test("a computer whose display list has no video opens no socket", async () => {
    const { server } = computer("c");
    const old = connect(server);
    await listDisplays(server);
    await old.connection.connect();
    expect(old.sockets).toHaveLength(0);
    expect(old.closed).toEqual([{ reason: SCREEN_TOO_OLD, outdated: true }]);
  });
});
