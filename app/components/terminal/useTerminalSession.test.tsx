import { describe, expect, test, mock } from "bun:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";

// Keep the websocket mock out of the shared Bun process.
if (!process.env.ZEN_TERMINAL_SESSION_HOOK_CHILD) {
  test("terminal session hook behavioral scenarios", () => {
    const result = Bun.spawnSync(
      [process.execPath, "test", import.meta.filename],
      { env: { ...process.env, ZEN_TERMINAL_SESSION_HOOK_CHILD: "1" } },
    );
    const output =
      new TextDecoder().decode(result.stdout) +
      new TextDecoder().decode(result.stderr);
    if (result.exitCode !== 0) throw new Error(output);
    expect(output).not.toContain("(fail)");
  });
} else {
  type Listener = (payload: any) => void;
  const listeners = new Map<string, Set<Listener>>();
  const opens: unknown[][] = [];
  let connected = false;
  const fakeClient = {
    on(event: string, listener: Listener) {
      if (!listeners.has(event)) listeners.set(event, new Set());
      listeners.get(event)!.add(listener);
    },
    off(event: string, listener: Listener) {
      listeners.get(event)?.delete(listener);
    },
    emit(event: string, payload: unknown) {
      listeners.get(event)?.forEach((listener) => listener(payload));
    },
    isConnected: () => connected,
    openTerminal(...args: unknown[]) {
      if (!connected) throw new Error("Daemon is not connected.");
      opens.push(args);
    },
    closeTerminal() {},
    resizeTerminal() {},
    sendTerminalInput() {},
  };
  mock.module("../../services/websocket", () => ({ wsClient: fakeClient }));
  const { useTerminalSession } = await import("./useTerminalSession");
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

  type Session = ReturnType<typeof useTerminalSession>;
  const mount = () => {
    const ref: { current: Session | null } = { current: null };
    function Probe() {
      ref.current = useTerminalSession("server-1", "target-1", "tmux", {});
      return null;
    }
    let renderer: TestRenderer.ReactTestRenderer;
    act(() => {
      renderer = TestRenderer.create(<Probe />);
    });
    return { session: () => ref.current!, unmount: () => act(() => renderer.unmount()) };
  };

  describe("useTerminalSession", () => {
    test("defers the open until the server connects", () => {
      connected = false;
      opens.length = 0;
      const probe = mount();

      expect(() => probe.session().resize(80, 24)).not.toThrow();
      expect(opens).toEqual([]);

      connected = true;
      act(() => fakeClient.emit("connected", { serverId: "server-1" }));
      expect(opens).toEqual([["server-1", "target-1", "tmux", 80, 24]]);
      probe.unmount();
    });

    test("opens immediately when already connected", () => {
      connected = true;
      opens.length = 0;
      const probe = mount();

      expect(probe.session().resize(100, 30)).toBe(true);
      expect(opens).toEqual([["server-1", "target-1", "tmux", 100, 30]]);
      probe.unmount();
    });
  });
}
