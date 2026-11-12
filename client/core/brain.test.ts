import { describe, expect, test } from "bun:test";
import { brainChatState, brainChatUnavailable, brainComposerBlock, brainSurface } from "./brain";

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
  test("Brain's chat is unavailable on an executor without structured chat events", () => {
    const plain = { id: "plain", name: "plain", provider: "custom", capabilities: { interactive_tty: true, structured_events: false } };
    const claude = { id: "claude", name: "claude", provider: "claude", capabilities: { interactive_tty: true, structured_events: true } };
    expect(brainChatUnavailable({ host_worker: host, chat_thread_id: "t1", host_executor: plain })).toBe(true);
    expect(brainChatUnavailable({ host_worker: host, chat_thread_id: "t1", host_executor: claude })).toBe(false);
    expect(brainChatUnavailable({ host_worker: host, chat_thread_id: "t1" })).toBe(false);
  });
  test("no computer", () => {
    expect(brainSurface({ hasServer: false, connected: false, snapshot: null })).toBe("no_computer");
  });
});

describe("brainChatState (soft-final/SPEC.md §1.3)", () => {
  test("loading, failed (a load error or a transcript the computer cannot read), empty, content", () => {
    expect(brainChatState({ itemCount: 0, loading: true })).toBe("loading");
    expect(brainChatState({ itemCount: 0, loading: false, error: "timed out" })).toBe("failed");
    expect(brainChatState({ itemCount: 0, loading: false, conversation: { available: false, reason: "transcript_malformed" } })).toBe("failed");
    expect(brainChatState({ itemCount: 0, loading: false, conversation: { available: false, reason: "session_not_ready" } })).toBe("loading");
    expect(brainChatState({ itemCount: 0, loading: false, conversation: { available: false, reason: "transcript_not_found" } })).toBe("empty");
    expect(brainChatState({ itemCount: 0, loading: false, conversation: { available: true } })).toBe("empty");
    expect(brainChatState({ itemCount: 3, loading: false, error: "late" })).toBe("content");
  });
  test("the composer says why it cannot send; offline still sends (they queue)", () => {
    const plain = { host_executor: { id: "plain", name: "plain", capabilities: { structured_events: false } } };
    expect(brainComposerBlock({ surface: "chat", snapshot: plain, chat: "empty" })).toBe("Chat isn't available");
    expect(brainComposerBlock({ surface: "chat", snapshot: null, chat: "failed" })).toBe("Chat didn't load");
    expect(brainComposerBlock({ surface: "host_down", snapshot: null, chat: "empty" })).toBe("Brain can't start");
    expect(brainComposerBlock({ surface: "offline", snapshot: null, chat: "empty" })).toBeNull();
    expect(brainComposerBlock({ surface: "chat", snapshot: null, chat: "content" })).toBeNull();
  });
});
