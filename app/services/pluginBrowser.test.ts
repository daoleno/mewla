import { beforeEach, expect, mock, test } from "bun:test";

if (!process.env.MEWLA_PLUGIN_BROWSER_TEST_CHILD) {
  test("plugin browser native compatibility in isolation", () => {
    const result = Bun.spawnSync([process.execPath, "test", import.meta.filename], {
      env: { ...process.env, MEWLA_PLUGIN_BROWSER_TEST_CHILD: "1" },
    });
    if (result.exitCode) {
      throw new Error(new TextDecoder().decode(result.stdout) + new TextDecoder().decode(result.stderr));
    }
    expect(result.exitCode).toBe(0);
  });
} else {

  let nativeAvailable = false;
  let loadCount = 0;
  let opened: string[] = [];
  let authCalls: string[][] = [];
  let browserCalls: string[] = [];
  let openFailure: Error | null = null;
  let nativeResult: { type: "success"; url: string } | { type: "cancel" } = { type: "cancel" };
  mock.module("expo-modules-core", () => ({ requireOptionalNativeModule: () => nativeAvailable ? {} : null }));
  mock.module("react-native", () => ({ Linking: { openURL: async (url: string) => {
    if (openFailure) throw openFailure;
    opened.push(url);
  } } }));
  mock.module("expo-web-browser", () => {
    loadCount++;
    if (!nativeAvailable) throw new Error("Cannot find native module 'ExpoWebBrowser'");
    return {
      openAuthSessionAsync: async (url: string, callback: string) => { authCalls.push([url, callback]); return nativeResult; },
      openBrowserAsync: async (url: string) => { browserCalls.push(url); return { type: "dismiss" }; },
    };
  });
  const { openPluginAuthorization } = await import("./pluginBrowser");
  beforeEach(() => { opened = []; authCalls = []; browserCalls = []; openFailure = null; });

  test("route import and external authorization work without evaluating missing native browser", async () => {
    expect(loadCount).toBe(0);
    const url = "https://mcp.linear.app/authorize?state=bound-state&code_challenge=bound-challenge";
    expect(await openPluginAuthorization(url, false)).toEqual({ type: "external" });
    expect(opened).toEqual([url]);
    expect(loadCount).toBe(0);
  });

  test("external device page stays pending for polling; launch failure remains actionable", async () => {
    expect(await openPluginAuthorization("https://github.com/login/device", true)).toEqual({ type: "external" });
    openFailure = new Error("No browser installed");
    await expect(openPluginAuthorization("https://github.com/login/device", true)).rejects.toThrow("No browser installed");
  });

  test("matching clients retain native auth success, cancellation and exact callback", async () => {
    nativeAvailable = true;
    const url = "https://mcp.notion.com/authorize?state=bound-state";
    nativeResult = { type: "success", url: "mewla://plugins?state=bound-state&code=one" };
    expect(await openPluginAuthorization(url, false)).toEqual(nativeResult);
    expect(authCalls).toEqual([[url, "mewla://plugins"]]);
    nativeResult = { type: "cancel" };
    expect(String((await openPluginAuthorization(url, false)).type)).toBe("cancel");
    expect(await openPluginAuthorization("https://github.com/login/device", true)).toEqual({ type: "external" });
    expect(browserCalls).toEqual(["https://github.com/login/device"]);
    expect(opened).toEqual([]);
  });

  test("a web sign-in that can't return to the web UI opens its own tab and is polled, never an auth popup", async () => {
    nativeAvailable = true;
    authCalls = [];
    const url = "https://accounts.google.com/o/oauth2/v2/auth?state=bound-state";
    expect(await openPluginAuthorization(url, false, "new-tab")).toEqual({ type: "external" });
    expect(opened).toEqual([url]);
    expect(authCalls).toEqual([]);
  });
}
