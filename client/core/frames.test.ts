import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { deflateRawSync, inflateRawSync } from "zlib";
import type { StoredServer } from "./storedServerContract";
import { FRAME_DEFLATE_SUBPROTOCOL } from "./frames";
import { FakeSocket, installMemoryPlatform } from "./testing/memoryPlatform";
import { Transport, type Frame } from "./transport";
import { AUTH_SUBPROTOCOL_PREFIX } from "./webSocketAuth";

const server = { id: "s1", name: "Desk", url: "ws://127.0.0.1:9/ws", daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64) } as StoredServer;
const wait = (ms = 5) => new Promise((resolve) => setTimeout(resolve, ms));
let sockets: FakeSocket[];

function platformWith(inflate: boolean) {
  sockets = [];
  installMemoryPlatform({
    openSocket: (url, protocols) => {
      const socket = new FakeSocket(url, protocols);
      sockets.push(socket);
      return socket;
    },
    ...(inflate ? { inflateRaw: (data: Uint8Array) => new Uint8Array(inflateRawSync(data)) } : {}),
  });
}

async function connected(): Promise<{ transport: Transport; socket: FakeSocket; heard: Frame[] }> {
  const transport = new Transport(server);
  const heard: Frame[] = [];
  transport.on("brain_snapshot", (frame) => heard.push(frame));
  transport.connect();
  await wait();
  const socket = sockets.at(-1)!;
  socket.open();
  return { transport, socket, heard };
}

describe("compressed messages", () => {
  beforeEach(() => platformWith(true));
  afterEach(() => platformWith(false));

  test("where the platform inflates, the connection offers mewla.frames.deflate-raw after its auth subprotocol", async () => {
    const { transport, socket } = await connected();
    expect(socket.protocols).toHaveLength(2);
    expect(socket.protocols[0].startsWith(AUTH_SUBPROTOCOL_PREFIX)).toBe(true);
    expect(socket.protocols[1]).toBe(FRAME_DEFLATE_SUBPROTOCOL);
    transport.close();
  });

  test("a binary message is the JSON message inflated, as bytes or as an ArrayBuffer; text still reads as text", async () => {
    const { transport, socket, heard } = await connected();
    const big = { type: "brain_snapshot", brain: { note: "a long line\n".repeat(500) } };
    const deflated = new Uint8Array(deflateRawSync(Buffer.from(JSON.stringify(big))));
    socket.onmessage?.({ data: deflated });
    socket.onmessage?.({ data: deflated.buffer.slice(deflated.byteOffset, deflated.byteOffset + deflated.byteLength) });
    socket.onmessage?.({ data: JSON.stringify({ type: "brain_snapshot", brain: { note: "short" } }) });
    expect(heard.map((frame) => (frame.brain as { note: string }).note)).toEqual([big.brain.note, big.brain.note, "short"]);
    transport.close();
  });
});

describe("without an inflater", () => {
  beforeEach(() => platformWith(false));

  test("nothing is offered beyond the auth subprotocol, and a stray binary message is dropped", async () => {
    const { transport, socket, heard } = await connected();
    expect(socket.protocols).toHaveLength(1);
    socket.onmessage?.({ data: new Uint8Array(deflateRawSync(Buffer.from(JSON.stringify({ type: "brain_snapshot", brain: {} })))) });
    socket.onmessage?.({ data: JSON.stringify({ type: "brain_snapshot", brain: { note: "text" } }) });
    expect(heard).toHaveLength(1);
    expect(transport.state).toBe("connected");
    transport.close();
  });
});
