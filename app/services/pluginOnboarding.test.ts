import { expect, test } from "bun:test";
import { finishPluginReturn, isPluginCallbackUrl, isPluginReturnUrl, matchesPluginReturn, pendingConnectionKey, retainPluginReturn, type PendingConnection } from "./pluginOnboarding";
const pending: PendingConnection = { serverId: "server-a", flow: { id: "flow-a", integration: "linear", status: "waiting", expires: new Date(Date.now() + 60000).toISOString(), authorization_url: "https://mcp.linear.app/authorize?state=secret-state" } };
test("native return is bound to exact URI and original state", () => {
  expect(matchesPluginReturn("mewla://plugins?state=secret-state&code=one", pending.flow)).toBe(true);
  for (const uri of ["other://plugins?state=secret-state&code=one", "mewla://plugins/path?state=secret-state", "mewla://plugins?state=other", "mewla://plugins?state=secret-state#fragment"]) expect(matchesPluginReturn(uri, pending.flow)).toBe(false);
  for (const uri of ["https://evil.test?state=secret-state", "mewla://plugins/path?state=secret-state", "mewla://plugins?state=other", "mewla://plugins?state=secret-state#fragment", "mewla://user@plugins?state=secret-state", "mewla://plugins:123?state=secret-state", "mewla://plugins?state=secret-state&state=secret-state", "mewla://plugins?state=secret-state&code=one&code=two"]) expect(matchesPluginReturn(uri, pending.flow)).toBe(false);
});
test("server switch and expiry never dispatch authorization to another server", async () => {
  let calls = 0;
  const send = async () => { calls++; return {}; };
  await expect(finishPluginReturn(pending, "server-b", "mewla://plugins?state=secret-state&code=one", send)).rejects.toThrow("another connection");
  await expect(finishPluginReturn({ ...pending, flow: { ...pending.flow, expires: new Date(0).toISOString() } }, "server-a", "mewla://plugins?state=secret-state&code=one", send)).rejects.toThrow("expired");
  expect(calls).toBe(0);
});
test("matching native return uses authenticated original server and flow", async () => {
  const response = await finishPluginReturn(pending, "server-a", "mewla://plugins?state=secret-state&code=one", async (server, request) => {
    expect(server).toBe("server-a"); expect(request.flow_id).toBe("flow-a"); expect(request.action).toBe("connect_finish"); return { flow: { ...pending.flow, status: "connected" } };
  });
  expect(response.flow?.status).toBe("connected");
});
test("return during a server switch is saved only for its original unexpired flow", async () => {
  const values = new Map([[pendingConnectionKey("server-a"), JSON.stringify(pending)]]);
  const storage = { getItemAsync: async (key: string) => values.get(key) ?? null, setItemAsync: async (key: string, value: string) => { values.set(key, value); } };
  const callback = "mewla://plugins?state=secret-state&code=one";
  expect(await retainPluginReturn(["server-b", "server-a"], callback, storage)).toBe("server-a");
  expect(JSON.parse(values.get(pendingConnectionKey("server-a"))!).callback).toBe(callback);
  expect(values.has(pendingConnectionKey("server-b"))).toBe(false);
  values.set(pendingConnectionKey("server-a"), JSON.stringify({ ...pending, flow: { ...pending.flow, expires: new Date(0).toISOString() } }));
  expect(await retainPluginReturn(["server-a"], callback, storage)).toBe(null);
  expect(JSON.parse(values.get(pendingConnectionKey("server-a"))!).callback).toBeUndefined();
});

test("only Plugins authorization returns are recognised as returns", () => {
  expect(isPluginReturnUrl("mewla://plugins?state=abc&code=1")).toBe(true);
  expect(isPluginReturnUrl("mewla://plugins")).toBe(false);
  expect(isPluginReturnUrl("mewla://settings?p=abc")).toBe(false);
  expect(isPluginReturnUrl("other://plugins?state=abc")).toBe(false);
  expect(isPluginReturnUrl("https://mewla.example/#pair=abc")).toBe(false);
  expect(isPluginReturnUrl("/plugins?state=abc")).toBe(false);
});

test("bare Plugins callbacks are recognised only on the mewla scheme", () => {
  expect(isPluginCallbackUrl("mewla://plugins")).toBe(true);
  expect(isPluginCallbackUrl("other://plugins")).toBe(false);
  expect(isPluginCallbackUrl("mewla://plugins?state=abc")).toBe(false);
});
