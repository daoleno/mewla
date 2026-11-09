// @ts-nocheck
import { beforeEach, describe, expect, mock, test } from "bun:test";

let handler: (request: { action: string; enabled?: boolean }, signal?: AbortSignal) => Promise<unknown>;
mock.module("../services/browser", () => ({
  browserRequest: (_server: unknown, request: { action: string; enabled?: boolean }, signal?: AbortSignal) => handler(request, signal),
  BrowserRequestError: class extends Error {
    constructor(message: string, readonly status: number, readonly detail: string) { super(message); }
  },
}));
mock.module("./currentServer", () => ({ useCurrentServer: () => ({}) }));

const { readVirtualScreenLabs, writeVirtualScreenLabs, resetVirtualScreenLabs, setVirtualScreenTimeout, setVirtualScreenErrorMs, virtualScreenSnapshot } = await import("./virtualScreenLabs");
const { BrowserRequestError } = await import("../services/browser");

const server = { id: "s1", name: "manjaro" };

beforeEach(() => resetVirtualScreenLabs());

describe("virtual screen labs", () => {
  test("a read gives the computer's answer", async () => {
    handler = async () => ({ virtual_screen: true });
    await readVirtualScreenLabs(server);
    expect(virtualScreenSnapshot()).toMatchObject({ server: "s1", enabled: true, error: null });
  });

  test("a computer with no virtual screen says so, and an older one is taken as having one", async () => {
    handler = async () => ({ virtual_screen: false, virtual_screen_unsupported: true });
    await readVirtualScreenLabs(server);
    expect(virtualScreenSnapshot()).toMatchObject({ enabled: false, unsupported: true });
    handler = async () => { throw new BrowserRequestError("Browser request failed", 502, "down"); };
    await readVirtualScreenLabs(server);
    expect(virtualScreenSnapshot()).toMatchObject({ unsupported: true });
    resetVirtualScreenLabs();
    handler = async () => ({ virtual_screen: false });
    await readVirtualScreenLabs(server);
    expect(virtualScreenSnapshot()).toMatchObject({ enabled: false, unsupported: false });
  });

  test("a failed read is an error, not an off switch", async () => {
    handler = async () => { throw new BrowserRequestError("Browser request failed", 404, "not found"); };
    await readVirtualScreenLabs(server);
    expect(virtualScreenSnapshot().enabled).toBeNull();
    expect(virtualScreenSnapshot().error).toBe("Browser isn't on this server yet");
  });

  test("a computer that never answers ends in an error", async () => {
    handler = (_request, signal) => new Promise((_resolve, reject) => signal?.addEventListener("abort", () => reject(new Error("aborted"))));
    setVirtualScreenTimeout(50);
    await readVirtualScreenLabs(server);
    setVirtualScreenTimeout(12_000);
    expect(virtualScreenSnapshot().error).toBe("Can't reach manjaro");
  });

  test("a write turns it on and a failed write says why and keeps the old answer", async () => {
    handler = async (request) => ({ virtual_screen: request.enabled ?? false });
    await readVirtualScreenLabs(server);
    await writeVirtualScreenLabs(server, true, () => true);
    expect(virtualScreenSnapshot()).toMatchObject({ enabled: true, error: null, busy: false });
    handler = async () => { throw new BrowserRequestError("x", 401, "unauthorized"); };
    await writeVirtualScreenLabs(server, false, () => true);
    expect(virtualScreenSnapshot()).toMatchObject({ enabled: true, error: "This phone needs to pair again", busy: false });
  });

  test("a write is pending at once, with what it turns to, until the computer answers", async () => {
    let answer: (value: { virtual_screen: boolean }) => void = () => {};
    handler = async () => ({ virtual_screen: false });
    await readVirtualScreenLabs(server);
    handler = () => new Promise((resolve) => { answer = resolve; });
    const writing = writeVirtualScreenLabs(server, true, () => true);
    expect(virtualScreenSnapshot()).toMatchObject({ enabled: false, busy: true, target: true });
    answer({ virtual_screen: true });
    await writing;
    expect(virtualScreenSnapshot()).toMatchObject({ enabled: true, busy: false, target: null });
  });

  test("a failed write's message is let go after a moment", async () => {
    handler = async () => ({ virtual_screen: false });
    await readVirtualScreenLabs(server);
    handler = async () => { throw new BrowserRequestError("x", 401, "unauthorized"); };
    setVirtualScreenErrorMs(20);
    await writeVirtualScreenLabs(server, true, () => true);
    setVirtualScreenErrorMs(6_000);
    expect(virtualScreenSnapshot()).toMatchObject({ enabled: false, busy: false, error: "This phone needs to pair again" });
    await new Promise((resolve) => setTimeout(resolve, 60));
    expect(virtualScreenSnapshot()).toMatchObject({ enabled: false, error: null });
  });

  test("an answer from a computer that is no longer current is dropped", async () => {
    handler = async () => ({ virtual_screen: true });
    await writeVirtualScreenLabs(server, true, () => false);
    expect(virtualScreenSnapshot().enabled).toBeNull();
  });
});
