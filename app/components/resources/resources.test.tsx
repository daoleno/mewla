import { afterEach, expect, mock, spyOn, test } from "bun:test";
import React, { useEffect } from "react";
import TestRenderer, { act } from "react-test-renderer";

// Native and transport mocks stay isolated from other Bun test suites.
if (!process.env.ZEN_RESOURCE_TEST_CHILD) {
  test("Resources lifecycle, states and terminal menu", () => {
    const result = Bun.spawnSync([process.execPath, "test", import.meta.filename], {
      env: { ...process.env, ZEN_RESOURCE_TEST_CHILD: "1" },
    });
    if (result.exitCode !== 0) throw new Error(new TextDecoder().decode(result.stderr));
    expect(result.exitCode).toBe(0);
  });
} else {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  let serverId: string | null = "a";
  let connected = "connected";
  let focused = true;
  const listeners = new Set<(state: string) => void>();
  const appState = {
    currentState: "active",
    addEventListener: (_: string, callback: (state: string) => void) => {
      listeners.add(callback);
      return { remove: () => listeners.delete(callback) };
    },
  };
  const isCurrentServer = (id: string) => id === serverId;
  const calls: { serverId: string; signal: AbortSignal; resolve(value: unknown): void; reject(error: Error): void }[] = [];
  mock.module("react-native", () => ({
    AppState: appState, View: "View", Text: "Text", ScrollView: "ScrollView",
    StyleSheet: { create: (styles: unknown) => styles },
  }));
  mock.module("expo-router", () => ({
    useFocusEffect: (callback: () => (() => void) | undefined) => {
      useEffect(() => focused ? callback() : undefined, [callback, focused]);
    },
  }));
  mock.module("../../store/currentServer", () => ({
    useCurrentServer: () => ({ currentServer: serverId ? { id: serverId } : null, isCurrentServer }),
  }));
  mock.module("../../store/workers", () => ({
    useWorkerServerSummary: () => ({ serverConnections: serverId ? { [serverId]: connected } : {} }),
  }));
  mock.module("../../services/websocket", () => ({
    wsClient: { getResourceTelemetry: (id: string, signal: AbortSignal) => new Promise((resolve, reject) => {
      calls.push({ serverId: id, signal, resolve, reject });
      signal.addEventListener("abort", () => reject(new Error("cancelled")));
    }) },
  }));
  mock.module("react-native-safe-area-context", () => ({ useSafeAreaInsets: () => ({ bottom: 0 }) }));
  mock.module("../../constants/tokens", () => ({ useAppTheme: () => ({ colors: {} }) }));
  mock.module("./resourceStyles", () => ({ createResourceStyles: () => ({}), RESOURCES_CONTENT_MAX_WIDTH: 800 }));
  mock.module("../ui/AnimatedPressable", () => ({ AnimatedPressable: "Pressable" }));
  mock.module("../ui/EmptyState", () => ({ EmptyState: "EmptyState" }));
  mock.module("../ui/ActionMenu", () => ({ ActionMenu: "ActionMenu" }));
  mock.module("./ResourceOverviewSections", () => ({ PressureHeadline: "PressureHeadline", PressureSignals: "PressureSignals", CpuSection: "CPU", MemorySection: "Memory" }));
  mock.module("./ResourceDetailSections", () => ({ DiskSection: "Disk", PressureSection: "Pressure" }));
  mock.module("./ResourceConsumersSection", () => ({ ConsumersSection: "Consumers" }));
  const { useResourceTelemetry } = await import("./useResourceTelemetry");
  const { ResourcesView } = await import("./ResourcesView");
  const { TerminalActionPopover } = await import("../terminal/TerminalActionPopover");
  const { resolveTerminalTheme, buildTerminalChrome } = await import("../../constants/terminalThemes");
  const { normalizeResourceTelemetry } = await import("../../services/resourceTelemetry");
  const sample = normalizeResourceTelemetry({ sampled_at: "2026-10-01T06:00:00Z", version: 2 })!;
  let renderer: TestRenderer.ReactTestRenderer | undefined;
  afterEach(() => {
    act(() => renderer?.unmount());
    renderer = undefined;
    calls.length = 0;
    serverId = "a";
    connected = "connected";
    focused = true;
    appState.currentState = "active";
    mock.restore();
  });
  const stateChange = async (state: string) => {
    await act(async () => {
      appState.currentState = state;
      listeners.forEach((listener) => listener(state));
    });
  };
  function mountHook() {
    let state!: ReturnType<typeof useResourceTelemetry>;
    function Host() { state = useResourceTelemetry(); return null; }
    act(() => { renderer = TestRenderer.create(<Host />); });
    return {
      get state() { return state; },
      rerender: async () => { await act(async () => renderer!.update(<Host />)); },
    };
  }

  test("polls at five seconds; background, blur and unmount cancel reads and stop polling", async () => {
    const timers = new Map<number, () => void>();
    let next = 0;
    spyOn(globalThis, "setTimeout").mockImplementation(((fn: () => void, ms: number) => {
      expect(ms).toBe(5000); timers.set(++next, fn); return next;
    }) as typeof setTimeout);
    spyOn(globalThis, "clearTimeout").mockImplementation(((id: number) => { timers.delete(id); }) as typeof clearTimeout);
    const hook = mountHook();
    expect(hook.state.loading).toBe(true);
    expect(calls).toHaveLength(1);
    await act(async () => calls[0].resolve(sample));
    expect(hook.state.telemetry).toEqual(sample);
    expect(timers.size).toBe(1);
    await act(async () => { const timer = [...timers.values()][0]; timers.clear(); timer(); });
    expect(calls).toHaveLength(2);
    await stateChange("background");
    expect(calls[1].signal.aborted).toBe(true);
    expect(timers.size).toBe(0);
    await stateChange("active");
    expect(calls).toHaveLength(3);
    await stateChange("active");
    expect(calls).toHaveLength(3);
    focused = false;
    await hook.rerender();
    expect(calls[2].signal.aborted).toBe(true);
    expect(listeners.size).toBe(0);
    expect(timers.size).toBe(0);
    focused = true;
    await hook.rerender();
    expect(calls).toHaveLength(4);
    act(() => renderer!.unmount());
    expect(calls[3].signal.aborted).toBe(true);
    renderer = undefined;
  });

  test("switching current server clears data and rejects late old-server results", async () => {
    const hook = mountHook();
    await act(async () => calls[0].resolve(sample));
    expect(hook.state.telemetry).not.toBeNull();
    await act(async () => hook.state.retry());
    const old = calls[1];
    serverId = "b";
    await hook.rerender();
    expect(old.signal.aborted).toBe(true);
    expect(hook.state.telemetry).toBeNull();
    expect(calls.at(-1)!.serverId).toBe("b");
    await act(async () => old.resolve(sample));
    expect(hook.state.telemetry).toBeNull();
    await act(async () => calls.at(-1)!.resolve({ ...sample, state: "critical" }));
    expect(hook.state.telemetry?.state).toBe("critical");
  });

  test("no server and disconnect make no requests; reconnect, error and retry recover", async () => {
    serverId = null;
    const hook = mountHook();
    expect(calls).toHaveLength(0);
    serverId = "a";
    connected = "connecting";
    await hook.rerender();
    expect(hook.state.loading).toBe(true);
    expect(calls).toHaveLength(0);
    connected = "connected";
    await hook.rerender();
    await act(async () => calls[0].reject(new Error("First sample unavailable")));
    expect(hook.state.error).toBe("First sample unavailable");
    expect(hook.state.loading).toBe(false);
    await act(async () => hook.state.retry());
    await act(async () => calls[1].resolve(sample));
    expect(hook.state.error).toBeNull();
    connected = "disconnected";
    await hook.rerender();
    expect(hook.state.connected).toBe(false);
    expect(hook.state.telemetry).toEqual(sample);
    expect(calls).toHaveLength(2);
    connected = "connected";
    await hook.rerender();
    expect(calls).toHaveLength(3);
  });

  test("empty, connecting, loading, error and stale states expose usable actions", () => {
    const action = mock(() => {});
    const base = { telemetry: null, loading: false, error: null, connected: false, hasServer: false, onRetry: action, onOpenSettings: action };
    act(() => { renderer = TestRenderer.create(<ResourcesView {...base} />); });
    const empty = () => renderer!.root.findByType("EmptyState" as any).props;
    expect(empty().title).toBe("No current server");
    empty().action.onPress(); expect(action).toHaveBeenCalledTimes(1);
    act(() => renderer!.update(<ResourcesView {...base} hasServer loading />));
    expect(empty().title).toBe("Connecting to server");
    act(() => renderer!.update(<ResourcesView {...base} hasServer connected loading />));
    expect(empty().title).toBe("Reading the machine");
    act(() => renderer!.update(<ResourcesView {...base} hasServer connected error="Unavailable" />));
    expect(empty().detail).toBe("Unavailable");
    expect(empty().action.label).toBe("Try again");
    act(() => renderer!.update(<ResourcesView {...base} hasServer telemetry={sample} />));
    expect(empty().title).toBe("Server offline");
    expect(renderer!.root.findByType("PressureHeadline" as any).props.statusLabel).toBe("Last sample");
    act(() => renderer!.update(<ResourcesView {...base} hasServer connected telemetry={sample} error="Refresh error" />));
    expect(empty().title).toBe("Refresh failed");
    expect(empty().detail).toBe("Refresh error");
  });

  test("terminal action menu keeps only Session actions; Resources lives in the drawer", () => {
    const open = mock(() => {});
    const theme = resolveTerminalTheme("dark");
    const props: React.ComponentProps<typeof TerminalActionPopover> = {
      visible: true, title: "My session",
      left: 0, top: 0, creatingSession: false, newTerminalLabel: "New Terminal",
      newTerminalDisabled: false, showLinkedWork: false, theme,
      chrome: buildTerminalChrome(theme), onClose: open, onNewTerminal: open,
      onRename: open, onOpenLinkedWork: open, onTerminate: open,
    };
    act(() => { renderer = TestRenderer.create(<TerminalActionPopover {...props} />); });
    const menu = renderer!.root.findByType("ActionMenu" as any).props;
    expect(menu.title).toBe("My session");
    expect(menu.items.map((item: { key: string }) => item.key)).not.toContain("resources");
    expect(menu.items.at(-1).key).toBe("terminate");
  });
}
