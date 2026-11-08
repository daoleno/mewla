import { expect, mock, test } from "bun:test";

// Bun module mocks are process-wide; keep transport/auth fixtures out of
// upload and pairing tests regardless of test discovery order.
if (!process.env.MEWLA_BROWSER_PAGE_MODEL_TEST_CHILD) {
  test("browserPageModel contracts in isolation", () => {
    const result = Bun.spawnSync([process.execPath, "test", import.meta.filename], {
      env: { ...process.env, MEWLA_BROWSER_PAGE_MODEL_TEST_CHILD: "1" },
    });
    if (result.exitCode) {
      throw new Error(new TextDecoder().decode(result.stdout) + new TextDecoder().decode(result.stderr));
    }
    expect(result.exitCode).toBe(0);
  });
} else {
  mock.module("./auth", () => ({ buildAuthorizationHeader: async () => "signed-fixture" }));
  mock.module("react-native", () => ({ Platform: { OS: "ios" } }));
  mock.module("./pinnedTransport", () => ({ resolveStoredServerURL: async (server: { url: string }) => server.url }));
  const { BrowserRequestError } = await import("./browser");
  const { browserIssue, recoveryLabel } = await import("./browserPageModel");

  test("failures read as product states and keep raw text only as diagnostics", () => {
    const missing = browserIssue(new BrowserRequestError("Browser request failed", 404, "404 page not found"), "home");
    expect(missing).toMatchObject({ title: "Browser isn't on this server yet", recovery: "retry" });
    expect(missing!.detail).not.toMatch(/json|daemon|socket|provider|http/i);
    expect(missing!.diagnostic).toBe("HTTP 404 · 404 page not found");
    expect(browserIssue(new BrowserRequestError("x", 401, "unauthorized"), "home")!.recovery).toBe("settings");
    expect(browserIssue(new Error("Browser is controlled by someone else"), "home")!.recovery).toBe("take_control");
    expect(browserIssue(new Error("Browser control changed; request control again"), "home")!.recovery).toBe("take_control");
    expect(browserIssue(new Error("Browser action could not be drained; stop and reopen this browser"), "home")).toMatchObject({ title: "This window needs a fresh start", recovery: "none" });
    expect(browserIssue(new Error("Browser response timed out. Reconnect before continuing."), "home")!.recovery).toBe("reconnect");
    expect(browserIssue(new TypeError("Network request failed"), "home")!.title).toBe("Can't reach home");
    const parse = browserIssue(new SyntaxError("JSON Parse error: Unexpected character: p"), "home")!;
    expect(parse.title).toBe("That didn't finish");
    expect(`${parse.title} ${parse.detail}`).not.toMatch(/json|parse/i);
    expect(browserIssue(new Error("Browser request cancelled"), "home")).toBeNull();
    for (const recovery of ["retry", "reconnect", "take_control", "settings"] as const) expect(recoveryLabel(recovery)).toBeTruthy();
    expect(recoveryLabel("none")).toBeNull();
  });

  test("the person never manages browsers: Screen lists the agents' displays and nothing else", async () => {
    const list = await Bun.file(new URL("../components/screen/AgentScreens.tsx", import.meta.url)).text();
    expect(list).not.toMatch(/New browser|Open browser|Delete browser|Close browser|action: "(create|start|stop|delete|grant)"/);
    // An agent's browser is a window on its monitor of the agent screen, so there is no row of its own.
    expect(list).not.toMatch(/AgentBrowserCard|browserRequest/);
    expect(list).toContain("listDisplays");
  });
}
