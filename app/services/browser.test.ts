import { expect, mock, test } from "bun:test";

// Bun module mocks are process-wide; keep transport/auth fixtures out of
// upload and pairing tests regardless of test discovery order.
if (!process.env.MEWLA_BROWSER_SERVICE_TEST_CHILD) {
  test("browser contracts in isolation", () => {
    const result = Bun.spawnSync([process.execPath, "test", import.meta.filename], {
      env: { ...process.env, MEWLA_BROWSER_SERVICE_TEST_CHILD: "1" },
    });
    if (result.exitCode) {
      throw new Error(new TextDecoder().decode(result.stdout) + new TextDecoder().decode(result.stderr));
    }
    expect(result.exitCode).toBe(0);
  });
} else {
  mock.module("./auth", () => ({ buildAuthorizationHeader: async () => "signed-fixture" }));
  mock.module("./pinnedTransport", () => ({ resolveStoredServerURL: async (server: { url: string }) => server.url }));
  const { BrowserRequestError, browserRequest } = await import("./browser");
  const server = { id: "server-a", daemonId: "fixture", url: "wss://server-a.test/ws", name: "A" };
  test("plain-text server failures become typed errors instead of JSON parse errors", async () => {
    const fetchOriginal = globalThis.fetch;
    globalThis.fetch = (async () => new Response("404 page not found", { status: 404 })) as any;
    try {
      const error = await browserRequest(server as any, { action: "settings" }).catch((e) => e);
      expect(error).toBeInstanceOf(BrowserRequestError);
      expect(error.status).toBe(404);
      expect(error.detail).toBe("404 page not found");
      globalThis.fetch = (async () => new Response(JSON.stringify({ error: "Browser is unavailable" }), { status: 409 })) as any;
      await expect(browserRequest(server as any, { action: "settings", enabled: true })).rejects.toThrow("Browser is unavailable");
    } finally { globalThis.fetch = fetchOriginal; }
  });
  test("the switch is read and written as a settings request", async () => {
    const fetchOriginal = globalThis.fetch;
    const bodies: string[] = [];
    globalThis.fetch = (async (_url: string, init: RequestInit) => {
      bodies.push(String(init.body));
      return new Response(JSON.stringify({ virtual_screen: true }), { status: 200 });
    }) as any;
    try {
      expect((await browserRequest(server as any, { action: "settings", enabled: true })).virtual_screen).toBe(true);
      expect(bodies).toEqual([JSON.stringify({ action: "settings", enabled: true })]);
    } finally { globalThis.fetch = fetchOriginal; }
  });
}
