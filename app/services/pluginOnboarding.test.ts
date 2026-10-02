import { expect, test } from "bun:test";
import { finishPluginReturn, matchesPluginReturn, type PendingConnection } from "./pluginOnboarding";
const pending: PendingConnection = { serverId: "server-a", flow: { id: "flow-a", integration: "linear", status: "waiting", expires: new Date(Date.now() + 60000).toISOString(), authorization_url: "https://mcp.linear.app/authorize?state=secret-state" } };
test("native return is bound to exact URI and original state", () => {
  expect(matchesPluginReturn("zen://plugins?state=secret-state&code=one", pending.flow)).toBe(true);
  for (const uri of ["https://evil.test?state=secret-state", "zen://plugins/path?state=secret-state", "zen://plugins?state=other", "zen://plugins?state=secret-state#fragment"]) expect(matchesPluginReturn(uri, pending.flow)).toBe(false);
});
test("server switch and expiry never dispatch authorization to another server", async () => {
  let calls = 0;
  const send = async () => { calls++; return {}; };
  await expect(finishPluginReturn(pending, "server-b", "zen://plugins?state=secret-state&code=one", send)).rejects.toThrow("another connection");
  await expect(finishPluginReturn({ ...pending, flow: { ...pending.flow, expires: new Date(0).toISOString() } }, "server-a", "zen://plugins?state=secret-state&code=one", send)).rejects.toThrow("expired");
  expect(calls).toBe(0);
});
test("matching native return uses authenticated original server and flow", async () => {
  const response = await finishPluginReturn(pending, "server-a", "zen://plugins?state=secret-state&code=one", async (server, request) => {
    expect(server).toBe("server-a"); expect(request.flow_id).toBe("flow-a"); expect(request.action).toBe("connect_finish"); return { flow: { ...pending.flow, status: "connected" } };
  });
  expect(response.flow?.status).toBe("connected");
});
