import { describe, expect, mock, test } from "bun:test";
import React, { useEffect, useLayoutEffect, useState } from "react";
import TestRenderer, { act } from "react-test-renderer";

// Real WebSocketClient and real chat hook over a fake socket; run in a child so
// the module mocks stay out of the shared Bun process.
if (!process.env.MEWLA_CHAT_CONNECTION_RACE_CHILD) {
  test("chat connection race scenarios", () => {
    const result = Bun.spawnSync(
      [process.execPath, "test", import.meta.filename],
      { env: { ...process.env, MEWLA_CHAT_CONNECTION_RACE_CHILD: "1" } },
    );
    const output =
      new TextDecoder().decode(result.stdout) +
      new TextDecoder().decode(result.stderr);
    if (result.exitCode !== 0) throw new Error(output);
    expect(output).not.toContain("(fail)");
  });
} else {
  mock.module("react-native", () => ({ Platform: { OS: "web" } }));
  mock.module("../../services/auth", () => ({
    buildAuthorizationHeader: async () => "test-authorization",
  }));
  mock.module("../../services/connectionIssue", () => ({
    diagnoseConnectionIssue: async () => null,
  }));

  class FakeWebSocket {
    static readonly CONNECTING = 0;
    static readonly OPEN = 1;
    static readonly CLOSED = 3;
    static instances: FakeWebSocket[] = [];
    readyState = FakeWebSocket.CONNECTING;
    sent: any[] = [];
    onopen: ((event?: unknown) => void) | null = null;
    onmessage: ((event: { data: string }) => void) | null = null;
    onclose: ((event?: unknown) => void) | null = null;
    onerror: ((event?: unknown) => void) | null = null;
    constructor(readonly url: string) {
      FakeWebSocket.instances.push(this);
    }
    open() {
      this.readyState = FakeWebSocket.OPEN;
      this.onopen?.();
    }
    send(value: string) {
      if (this.readyState !== FakeWebSocket.OPEN) throw new Error("socket is not open");
      this.sent.push(JSON.parse(value));
    }
    close() {
      if (this.readyState === FakeWebSocket.CLOSED) return;
      this.readyState = FakeWebSocket.CLOSED;
      this.onclose?.();
    }
  }
  Object.assign(globalThis, { WebSocket: FakeWebSocket });
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

  const { wsClient } = await import("../../services/websocket");
  const { useInterfaceChatSession } = await import("./InterfaceChatSession");
  type ConnectionState = "connected" | "connecting" | "offline";

  const server = {
    id: "server-a",
    name: "Server A",
    url: "ws://server-a.test/ws",
    daemonId: "daemon-a",
    daemonPublicKey: "public-key-a",
  };

  const settle = async () => {
    for (let index = 0; index < 8; index += 1) await Promise.resolve();
  };

  /**
   * The app shell keeps a per-server connection state from the client's events
   * and hands it to the chat. `endAfterCommit` ends the socket the moment the
   * commit that says "connected" lands, before React flushes passive effects.
   * `Binder` is the app's CurrentServerConnectionBinder: a sibling mounted
   * before the chat, so its effect runs first in a flush it shares with the chat.
   */
  function mountChat(options: { endAfterCommit?: boolean } = {}) {
    let endsLeft = options.endAfterCommit ? 1 : 0;
    let state: ConnectionState = "connecting";
    function Binder({ revision }: { revision: number }) {
      useEffect(() => {
        if (revision === 0) return;
        wsClient.disconnect();
        void Promise.resolve().then(() => wsClient.connect({ ...server }));
      }, [revision]);
      return null;
    }
    function Chat({ startedAt }: { startedAt: number }) {
      const [connection, setConnection] = useState<ConnectionState>("connecting");
      useEffect(() => {
        const onConnecting = () => setConnection("connecting");
        const onConnected = () => setConnection("connected");
        const onDisconnected = () => setConnection("connecting");
        wsClient.on("connecting", onConnecting);
        wsClient.on("connected", onConnected);
        wsClient.on("disconnected", onDisconnected);
        return () => {
          wsClient.off("connecting", onConnecting);
          wsClient.off("connected", onConnected);
          wsClient.off("disconnected", onDisconnected);
        };
      }, []);
      state = connection;
      useLayoutEffect(() => {
        if (endsLeft > 0 && connection === "connected") {
          endsLeft -= 1;
          FakeWebSocket.instances.at(-1)!.close();
        }
      }, [connection]);
      useInterfaceChatSession({
        serverId: server.id,
        workerId: "host",
        conversationScopeKey: "brain-thread:t1",
        workerInfo: { startedAt },
        connectionState: connection,
        screenFocused: true,
      });
      return null;
    }
    const tree = (props: { revision: number; startedAt: number }) => (
      <>
        <Binder revision={props.revision} />
        <Chat startedAt={props.startedAt} />
      </>
    );
    let renderer!: TestRenderer.ReactTestRenderer;
    act(() => {
      renderer = TestRenderer.create(tree({ revision: 0, startedAt: 1 }));
    });
    return {
      state: () => state,
      update: (props: { revision: number; startedAt: number }) =>
        act(() => renderer.update(tree(props))),
      unmount: () => act(() => renderer.unmount()),
    };
  }

  const subscribeFrames = (socket: FakeWebSocket) =>
    socket.sent.filter((frame) => frame.type === "codex_conversation_subscribe");
  /** The next socket exists: a foreground resume skips the reconnect backoff. */
  const reconnected = () =>
    act(async () => {
      wsClient.resumeReconnect();
      await settle();
    });

  describe("chat subscription while the connection changes under it", () => {
    test("a socket that ends between the connected commit and its effects does not crash the chat", async () => {
      wsClient.connect(server);
      await settle();
      const first = FakeWebSocket.instances.at(-1)!;
      const chat = mountChat({ endAfterCommit: true });
      // The effect for the "connected" commit runs after the socket is gone:
      // that must not throw out of React, and the chat must be waiting.
      expect(() => act(() => first.open())).not.toThrow();
      expect(chat.state()).toBe("connecting");
      expect(subscribeFrames(first)).toEqual([]);

      // The reconnect subscribes.
      await reconnected();
      const second = FakeWebSocket.instances.at(-1)!;
      expect(second).not.toBe(first);
      act(() => second.open());
      expect(chat.state()).toBe("connected");
      expect(subscribeFrames(second)).toHaveLength(1);
      chat.unmount();
      wsClient.disconnect();
    });

    test("the server binder's reconnect in the chat's own effect flush does not crash the chat", async () => {
      wsClient.connect(server);
      await settle();
      const first = FakeWebSocket.instances.at(-1)!;
      const chat = mountChat();
      act(() => first.open());
      expect(chat.state()).toBe("connected");
      expect(subscribeFrames(first)).toHaveLength(1);

      // The current server's record changes (revision) in the render that also
      // changes what the chat subscribes to: the binder's effect disconnects the
      // live socket first, then the chat's effect runs against a store that
      // still says "connected".
      expect(() => chat.update({ revision: 1, startedAt: 2 })).not.toThrow();
      await reconnected();
      const second = FakeWebSocket.instances.at(-1)!;
      expect(second).not.toBe(first);
      act(() => second.open());
      expect(chat.state()).toBe("connected");
      expect(subscribeFrames(second)).toHaveLength(1);
      chat.unmount();
      wsClient.disconnect();
    });
  });
}
