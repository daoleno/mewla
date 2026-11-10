import { describe, expect, test } from "bun:test";
import { brainSurface } from "./brain";

describe("brainSurface", () => {
  const host = { id: "h1", name: "Brain", status: "running" };
  test("chat once the computer reports a host and a thread, even offline (the transcript stays)", () => {
    expect(brainSurface({ hasServer: true, connected: true, snapshot: { host_worker: host, chat_thread_id: "t1" } })).toBe("chat");
    expect(brainSurface({ hasServer: true, connected: false, snapshot: { host_worker: host, chat_thread_id: "t1" } })).toBe("chat");
  });
  test("waking while connected without a host; offline before any snapshot", () => {
    expect(brainSurface({ hasServer: true, connected: true, snapshot: null })).toBe("waking");
    expect(brainSurface({ hasServer: true, connected: true, snapshot: { chat_thread_id: "t1" } })).toBe("waking");
    expect(brainSurface({ hasServer: true, connected: false, snapshot: null })).toBe("offline");
  });
  test("a host problem outranks a recorded host", () => {
    const host_problem = { code: "not_signed_in", executor: "claude", detail: "Sign in to Claude Code.", installed_executors: ["claude"] };
    expect(brainSurface({ hasServer: true, connected: true, snapshot: { host_worker: host, chat_thread_id: "t1", host_problem } })).toBe("host_down");
  });
  test("no computer", () => {
    expect(brainSurface({ hasServer: false, connected: false, snapshot: null })).toBe("no_computer");
  });
});
