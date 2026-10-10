import { afterEach, describe, expect, test } from "bun:test";
import { resolveBrowserPairingURL } from "./browserPairingOrigin.web";

const DAEMON_ID = "a".repeat(64);
const DAEMON_KEY = "b".repeat(64);
const OTHER_KEY = "c".repeat(64);
const originalFetch = globalThis.fetch;
const fetched: string[] = [];

function servePageHealth(body: unknown, status = 200) {
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    fetched.push(String(input));
    return new Response(typeof body === "string" ? body : JSON.stringify(body), { status });
  }) as typeof fetch;
}

afterEach(() => {
  globalThis.fetch = originalFetch;
  fetched.length = 0;
});

describe("resolveBrowserPairingURL", () => {
  const link = {
    url: "wss://zen.daoleno.com/ws",
    daemonId: DAEMON_ID,
    daemonPublicKey: DAEMON_KEY,
  };

  test("keeps a link for the page origin without probing", async () => {
    servePageHealth({});
    expect(await resolveBrowserPairingURL(link, "https://zen.daoleno.com")).toBe(link.url);
    expect(fetched).toEqual([]);
  });

  test("pairs a same-daemon link through the page origin", async () => {
    servePageHealth({ daemon_id: DAEMON_ID, daemon_public_key: DAEMON_KEY.toUpperCase() });
    expect(await resolveBrowserPairingURL(link, "https://manjaro.tail7e23.ts.net")).toBe(
      "wss://manjaro.tail7e23.ts.net/ws",
    );
    expect(fetched).toEqual(["https://manjaro.tail7e23.ts.net/health"]);
  });

  test("uses ws for a loopback http page", async () => {
    servePageHealth({ daemon_id: DAEMON_ID, daemon_public_key: DAEMON_KEY });
    expect(await resolveBrowserPairingURL(link, "http://127.0.0.1:9876")).toBe("ws://127.0.0.1:9876/ws");
  });

  test("rejects a link for a different daemon with browser guidance", async () => {
    servePageHealth({ daemon_id: DAEMON_ID, daemon_public_key: OTHER_KEY });
    const result = resolveBrowserPairingURL(link, "https://manjaro.tail7e23.ts.net");
    await expect(result).rejects.toThrow("zen.daoleno.com, a different Mewla daemon");
    await expect(result).rejects.toThrow("Open https://zen.daoleno.com/");
    await expect(result).rejects.toThrow("`mewla pair`");
  });

  test("rejects a matching key with a different daemon id", async () => {
    servePageHealth({ daemon_id: "d".repeat(64), daemon_public_key: DAEMON_KEY });
    await expect(resolveBrowserPairingURL(link, "https://manjaro.tail7e23.ts.net")).rejects.toThrow(
      "different Mewla daemon",
    );
  });

  test("leaves the link unchanged when the page is not a daemon", async () => {
    servePageHealth("<!doctype html>");
    expect(await resolveBrowserPairingURL(link, "http://localhost:8081")).toBe(link.url);
  });
});
