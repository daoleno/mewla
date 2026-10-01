import { expect, mock, test } from "bun:test";
import React, { useEffect } from "react";
import TestRenderer, { act } from "react-test-renderer";

// Isolate native module substitutes from the other Bun suites. The store,
// subscription transport, React effects and conversation reducer are real.
if (!process.env.ZEN_BRAIN_SWITCH_TEST_CHILD) {
  test("Brain executor switch rebinds the mounted Interface", () => {
    const result = Bun.spawnSync(
      [process.execPath, "test", import.meta.filename],
      {
        env: { ...process.env, ZEN_BRAIN_SWITCH_TEST_CHILD: "1" },
      },
    );
    if (result.exitCode !== 0) {
      throw new Error(
        new TextDecoder().decode(result.stdout) +
          new TextDecoder().decode(result.stderr),
      );
    }
    expect(result.exitCode).toBe(0);
  });
} else {
  mock.module("react-native", () => ({
    Platform: { OS: "android" },
    Keyboard: { dismiss() {} },
  }));
  mock.module("../../services/auth", () => ({
    buildAuthorizationHeader: async () => "fixture",
  }));
  mock.module("../../services/connectionIssue", () => ({
    diagnoseConnectionIssue: async () => null,
  }));
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

  const { wsClient } = await import("../../services/websocket");
  const { BrainProvider, useBrain, useBrainDispatch } =
    await import("../../store/brain");
  const { useInterfaceChatSession } = await import("./InterfaceChatSession");
  const { useInterfaceMessageTransport } =
    await import("./useInterfaceMessageTransport");
  const { buildZenTimeline } = await import("./InterfaceTimelineModel");

  type Wire = Record<string, any>;
  class Socket {
    static OPEN = 1;
    static instance: Socket;
    readyState = 0;
    sent: Wire[] = [];
    onopen?: () => void;
    onmessage?: (event: { data: string }) => void;
    onclose?: () => void;
    constructor() {
      Socket.instance = this;
    }
    send(data: string) {
      this.sent.push(JSON.parse(data));
    }
    receive(data: Wire) {
      this.onmessage?.({ data: JSON.stringify(data) });
    }
    close() {
      this.readyState = 3;
      this.onclose?.();
    }
  }
  const server = {
    id: "brain-switch-server",
    name: "Fixture",
    url: "ws://fixture.test/ws",
    daemonId: "fixture",
    daemonPublicKey: "fixture",
  };
  const scope = "brain-thread:executor-switch";
  const snapshot = (provider: string) => ({
    chat_thread_id: "executor-switch",
    host_executor: {
      id: provider,
      name: provider,
      provider,
      capabilities: { structured_events: true },
    },
    host_worker: {
      id: `host-${provider}`,
      name: "Brain",
      command: provider,
      status: "running",
      started_at: 1000,
    },
  });
  const event = (
    id: string,
    seq: number,
    kind = "assistant_message",
    body = id,
  ) => ({ id, seq, kind, body, timestamp: `2026-10-01T00:00:0${seq}Z` });

  for (const [from, to] of [
    ["claude", "codex"],
    ["codex", "claude"],
  ]) {
    test(`${from} -> ${to}: send, progress, final, error and late old subscription`, async () => {
      const originalSocket = globalThis.WebSocket;
      Object.assign(globalThis, { WebSocket: Socket });
      const previousSocket = Socket.instance;
      wsClient.connectServer(server);
      for (
        let i = 0;
        (Socket.instance === previousSocket || !Socket.instance?.onopen) &&
        i < 20;
        i++
      )
        await Promise.resolve();
      const socket = Socket.instance;
      socket.readyState = Socket.OPEN;
      socket.onopen?.();
      let session!: ReturnType<typeof useInterfaceChatSession>;
      let transport!: ReturnType<typeof useInterfaceMessageTransport>;
      let executor: string | undefined;
      function Interface() {
        const { state } = useBrain();
        const dispatch = useBrainDispatch();
        useEffect(() => {
          const handler = (data: Wire) =>
            dispatch({
              type: "BRAIN_SNAPSHOT",
              serverId: data.serverId,
              serverName: data.serverName,
              serverUrl: data.serverUrl,
              brain: data.brain,
            });
          wsClient.on("brain_snapshot", handler);
          return () => wsClient.off("brain_snapshot", handler);
        }, [dispatch]);
        const brain = state.byServer[server.id];
        executor = brain?.host_executor?.id;
        const workerId = brain?.host_worker?.id ?? "";
        session = useInterfaceChatSession({
          serverId: server.id,
          workerId,
          conversationScopeKey: scope,
          workerInfo: {
            command: brain?.host_worker?.command,
            startedAt: brain?.host_worker?.started_at,
          },
          connectionState: "connected",
          screenFocused: true,
        });
        transport = useInterfaceMessageTransport({
          ...session,
          serverId: server.id,
          workerId,
          conversationScopeKey: scope,
          connectionState: "connected",
          runningActivity: session.conversation?.activity,
          clearComposerNativeText() {},
          requestTurnFocus() {},
        });
        return null;
      }
      let renderer!: TestRenderer.ReactTestRenderer;
      try {
        await act(async () => {
          renderer = TestRenderer.create(
            <BrainProvider>
              <Interface />
            </BrainProvider>,
          );
        });
        await act(async () => {
          socket.receive({ type: "brain_snapshot", brain: snapshot(from) });
        });
        const lastSubscribe = () =>
          socket.sent
            .filter((msg) => msg.type === "codex_conversation_subscribe")
            .at(-1)!;
        const oldSub = lastSubscribe();
        const history = event("durable-history", 1);
        await act(async () => {
          socket.receive({
            type: "codex_conversation_snapshot",
            request_id: oldSub.request_id,
            conversation_id: scope,
            revision: 50,
            conversation: {
              available: true,
              source: "brain_chat",
              session_id: scope,
              path: `/fixture/${from}`,
              activity: {
                id: `${from}-turn`,
                status: "running",
                started_at: "2026-10-01T00:00:01Z",
              },
              events: [history],
            },
          });
        });
        expect(session.conversation?.events[0]?.body).toBe("durable-history");
        await act(async () =>
          session.setDraft("draft survives the host switch"),
        );
        await act(async () => {
          const switched = wsClient.setBrainExecutor(server.id, to);
          const request = socket.sent.at(-1)!;
          expect(request).toMatchObject({
            type: "brain_set_executor",
            executor_id: to,
          });
          socket.receive({
            type: "brain_snapshot",
            request_id: request.request_id,
            brain: snapshot(to),
          });
          await switched;
        });
        expect(executor).toBe(to);
        expect(session.draft).toBe("draft survives the host switch");
        const newSub = lastSubscribe();
        expect(newSub).toMatchObject({
          target_id: `host-${to}`,
          command: to,
          conversation_scope_key: scope,
        });
        expect(newSub.request_id).not.toBe(oldSub.request_id);
        expect(socket.sent).toContainEqual(
          expect.objectContaining({
            type: "codex_conversation_unsubscribe",
            request_id: oldSub.request_id,
          }),
        );
        await act(async () => {
          transport.submitTextToInterface("continue", "continue", []);
        });
        const input = socket.sent.at(-1)!;
        expect(input).toMatchObject({
          type: "send_input",
          worker_id: `host-${to}`,
          conversation_scope_key: scope,
        });
        await act(async () => {
          socket.receive({
            type: "codex_conversation_snapshot",
            request_id: newSub.request_id,
            conversation_id: scope,
            revision: 1,
            conversation: {
              available: true,
              source: "brain_chat",
              session_id: scope,
              path: `/fixture/${to}`,
              events: [history],
            },
          });
        });
        await act(async () => {
          socket.receive({
            type: "codex_conversation_delta",
            request_id: newSub.request_id,
            conversation_id: scope,
            base_revision: 1,
            revision: 2,
            upserts: [
              event("progress", 2, "commentary", "Working on the new host"),
            ],
            deletes: [],
            activity: {
              id: `${to}-turn`,
              status: "running",
              started_at: "2026-10-01T00:00:02Z",
            },
          });
        });
        expect(session.conversation?.activity?.id).toBe(`${to}-turn`);
        expect(session.conversation?.path).toBe(`/fixture/${to}`);
        expect(session.conversation?.session_id).toBe(scope);
        expect(session.conversation?.events.map((item) => item.body)).toContain(
          "Working on the new host",
        );
        await act(async () => {
          socket.receive({
            type: "codex_conversation_delta",
            request_id: newSub.request_id,
            conversation_id: scope,
            base_revision: 2,
            revision: 3,
            upserts: [event("new-reply", 3)],
            deletes: [],
            activity: {
              id: `${to}-turn`,
              status: "completed",
              started_at: "2026-10-01T00:00:02Z",
              settled_at: "2026-10-01T00:00:03Z",
            },
          });
        });
        expect(session.conversation?.events.map((item) => item.body)).toEqual([
          "durable-history",
          "Working on the new host",
          "new-reply",
        ]);
        expect(session.conversation?.activity?.status).toBe("completed");
        const timeline = JSON.stringify(
          buildZenTimeline(session.conversation!.events),
        );
        expect(timeline).toContain("new-reply");
        expect(timeline).toContain("Working on the new host");
        await act(async () => {
          socket.receive({
            type: "error",
            request_id: newSub.request_id,
            message: "New host stream failed",
          });
        });
        expect(session.error).toBe("New host stream failed");
        await act(async () => {
          socket.receive({
            type: "codex_conversation_snapshot",
            request_id: oldSub.request_id,
            conversation_id: scope,
            revision: 999,
            conversation: { available: true, events: [event("stale", 4)] },
          });
        });
        expect(
          session.conversation?.events.map((item) => item.id),
        ).not.toContain("stale");
      } finally {
        await act(async () => {
          renderer?.unmount();
        });
        wsClient.disconnectAll();
        Object.assign(globalThis, { WebSocket: originalSocket });
      }
    });
  }
}
