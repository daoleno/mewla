import { expect, mock, test } from "bun:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";

if (!process.env.MEWLA_PLUGIN_FLOW_TEST_CHILD) {
  test("address submission preserves a waiting flow and waits for background polling", () => {
    const result = Bun.spawnSync([process.execPath, "test", import.meta.filename], {
      env: { ...process.env, MEWLA_PLUGIN_FLOW_TEST_CHILD: "1" },
    });
    if (result.exitCode) throw new Error(new TextDecoder().decode(result.stdout) + new TextDecoder().decode(result.stderr));
    expect(result.exitCode).toBe(0);
  });
} else {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  const noop = () => {};
  const subscription = () => ({ remove: noop });
  mock.module("react-native", () => ({ AppState: { currentState: "active", addEventListener: subscription }, Linking: { getInitialURL: async () => null, addEventListener: subscription }, Platform: { OS: "web" }, StyleSheet: { create: (s: unknown) => s }, View: "view" }));
  const router = { replace: noop, push: noop };
  mock.module("expo-router", () => ({ useRouter: () => router, useGlobalSearchParams: () => ({}), Stack: { Screen: "screen" } }));
  mock.module("react-native-keyboard-controller", () => ({ KeyboardAwareScrollView: "scroll" }));
  mock.module("expo-clipboard", () => ({ setStringAsync: async () => {} }));
  mock.module("../../constants/tokens", () => ({ ContentWidth: {}, Spacing: {}, useAppColors: () => ({}) }));
  mock.module("../ui", () => ({ InlineNotice: "notice" }));
  mock.module("./PluginConnectionViews", () => ({ ConnectStatusCard: "card", NoServerState: "no-server" }));
  const isCurrentServer = () => true;
  mock.module("../../store/currentServer", () => ({ useCurrentServer: () => ({ isCurrentServer }) }));
  const stored = new Map<string, string>();
  mock.module("../../services/secureStorage", () => ({ secureStorage: {
    getItemAsync: async (key: string) => stored.get(key),
    setItemAsync: async (key: string, value: string) => { stored.set(key, value); },
    deleteItemAsync: async (key: string) => { stored.delete(key); },
  } }));
  mock.module("../../services/pluginBrowser", () => ({ openPluginAuthorization: async () => ({ type: "external" }) }));
  const flow = { id: "aws-flow", integration: "aws", status: "waiting", address_required: true, authorization_url: "https://example.com", expires: new Date(Date.now() + 3600000).toISOString() };
  const request = mock(async (_server: string, req: any): Promise<any> => req.action === "list" ? { catalog: [], accounts: [] } : { flow });
  mock.module("../../services/websocket", () => ({ wsClient: { requestConnections: request, on: noop, off: noop } }));
  const { PluginsFlowProvider, usePluginsFlow } = await import("./PluginsFlow");
  let state!: ReturnType<typeof usePluginsFlow>;
  function Capture() { state = usePluginsFlow(); return null; }

  test("bad address is retryable, polling keeps the flow, and Continue during polling is delivered", async () => {
    let tree!: TestRenderer.ReactTestRenderer;
    await act(async () => { tree = TestRenderer.create(<PluginsFlowProvider serverId="server" serverName="Server" connection="connected" deferReturn={async () => {}}><Capture /></PluginsFlowProvider>); });
    await act(async () => state.connect("aws"));
    const message = "That is not the address this sign-in ended on. Copy it from the browser's address bar, whole.";
    request.mockRejectedValueOnce(Object.assign(new Error(message), { code: "plugin_request_failed" }));
    await act(async () => { await expect(state.submitAddress("wrong-address")).rejects.toThrow(message); });
    expect(state.phase).toBe("waiting");
    expect(state.flow?.flow.id).toBe("aws-flow");
    expect(stored.size).toBe(1);
    await act(async () => state.check());
    expect(state.phase).toBe("waiting");
    expect(state.flow?.flow.id).toBe("aws-flow");

    let resolve!: (value: any) => void;
    request.mockImplementationOnce(() => new Promise(r => { resolve = r; }));
    let checking!: Promise<void>;
    let submitting!: Promise<void>;
    await act(async () => {
      checking = state.check();
      submitting = state.submitAddress("http://127.0.0.1:9876/plugins/oauth/callback?state=right&code=valid");
    });
    expect(request.mock.calls.at(-1)?.[1].action).toBe("connect_status");
    await act(async () => { resolve({ flow }); await checking; await submitting; });
    expect(request.mock.calls.at(-1)?.[1]).toEqual({ action: "connect_finish", flow_id: "aws-flow", callback: "http://127.0.0.1:9876/plugins/oauth/callback?state=right&code=valid" });

    request.mockRejectedValueOnce(Object.assign(new Error("This connection expired. Connect again."), { code: "plugin_request_failed" }));
    await act(async () => state.check());
    expect(state.phase).toBe("failed");
    expect(state.flow).toBeNull();
    expect(stored.size).toBe(0);
    await act(async () => state.connect("aws"));
    request.mockImplementationOnce(() => new Promise(r => { resolve = r; }));
    await act(async () => {
      checking = state.check();
      submitting = state.submitAddress("http://127.0.0.1:9876/plugins/oauth/callback?state=right&code=valid");
    });
    await act(async () => state.cancel());
    const finishCount = request.mock.calls.filter(([, req]) => req.action === "connect_finish").length;
    await act(async () => { resolve({ flow }); await checking; await submitting; });
    expect(state.phase).toBe("cancelled");
    expect(state.flow).toBeNull();
    expect(request.mock.calls.filter(([, req]) => req.action === "connect_finish")).toHaveLength(finishCount);
    await act(async () => tree.unmount());
  });
}
