import { describe, expect, mock, test } from "bun:test";
import { interfaceTimelinePhase } from "./interfaceTimelinePhase";

mock.module("react-native", () => ({ Platform: { OS: "web" } }));
mock.module("../../services/auth", () => ({
  buildAuthorizationHeader: async () => "test-authorization",
}));
mock.module("../../services/connectionIssue", () => ({
  diagnoseConnectionIssue: async () => null,
}));

const { interfaceChatThreadReducer } = await import("./InterfaceChatSession");
type ThreadState = Parameters<typeof interfaceChatThreadReducer>[0];

const quiet = { error: null, suppressed: false, unavailable: false, syncing: false };

/** The phase the timeline shows for this thread state, as the chat derives it. */
function phaseOf(thread: ThreadState) {
  return interfaceTimelinePhase({
    ...quiet,
    itemCount: thread.conversation?.events.length ?? 0,
    loading: thread.loading,
    error: thread.error,
  });
}

function freshThread(): ThreadState {
  // A new cache key starts the thread from its initial state.
  return interfaceChatThreadReducer(
    {
      cacheKey: "previous",
      conversation: null,
      loading: false,
      error: null,
      pendingUserMessages: [],
      turnFocusAnchorAliases: new Map(),
      streamCursor: { revision: 0, generation: 0 },
      awaitingSnapshot: false,
      resyncToken: 0,
    },
    { type: "cache_key_changed", cacheKey: "server-a:agent:brain" },
  );
}

function snapshot(thread: ThreadState, events: { id: string; seq: number; body: string }[]) {
  return interfaceChatThreadReducer(thread, {
    type: "snapshot",
    generation: 1,
    payload: {
      request_id: "stream-a",
      conversation_id: "thread-a",
      revision: 1,
      conversation: {
        available: true,
        session_id: "thread-a",
        events: events.map((event) => ({ ...event, kind: "assistant_message" })),
      },
    },
  });
}

describe("an empty timeline is loading until history has loaded", () => {
  test("a fresh thread and an opened stream are loading, never empty", () => {
    const fresh = freshThread();
    expect(fresh.loading).toBe(true);
    expect(phaseOf(fresh)).toBe("loading");

    const streaming = interfaceChatThreadReducer(fresh, { type: "stream_start", generation: 1 });
    expect(streaming.loading).toBe(true);
    expect(phaseOf(streaming)).toBe("loading");
  });

  test("a syncing transcript stays loading", () => {
    const streaming = interfaceChatThreadReducer(freshThread(), { type: "stream_start", generation: 1 });
    const syncing = interfaceChatThreadReducer(streaming, {
      type: "sync_status",
      generation: 1,
      status: { request_id: "stream-a", revision: 0, state: "syncing" },
    });
    expect(syncing.loading).toBe(true);
    expect(phaseOf(syncing)).toBe("loading");
    // A sync that reports syncing without the loading flag is still loading.
    expect(interfaceTimelinePhase({ ...quiet, itemCount: 0, loading: false, syncing: true })).toBe("loading");
  });

  test("history that arrives shows; a loaded empty history is the empty state", () => {
    const streaming = interfaceChatThreadReducer(freshThread(), { type: "stream_start", generation: 1 });
    const withHistory = snapshot(streaming, [{ id: "a", seq: 1, body: "Here is what I found." }]);
    expect(withHistory.loading).toBe(false);
    expect(phaseOf(withHistory)).toBe("content");

    const emptyHistory = snapshot(
      interfaceChatThreadReducer(freshThread(), { type: "stream_start", generation: 1 }),
      [],
    );
    expect(emptyHistory.loading).toBe(false);
    expect(phaseOf(emptyHistory)).toBe("empty");
  });

  test("a stream that fails is an error, not an empty conversation", () => {
    const streaming = interfaceChatThreadReducer(freshThread(), { type: "stream_start", generation: 1 });
    const failed = interfaceChatThreadReducer(streaming, {
      type: "stream_error",
      generation: 1,
      error: "Could not stream this conversation.",
    });
    expect(phaseOf(failed)).toBe("error");
  });

  test("rows always win, and an open command menu hides the empty canvas", () => {
    expect(interfaceTimelinePhase({ ...quiet, itemCount: 3, loading: true })).toBe("content");
    expect(interfaceTimelinePhase({ ...quiet, itemCount: 0, loading: true, suppressed: true })).toBe("hidden");
    expect(interfaceTimelinePhase({ ...quiet, itemCount: 0, loading: false, unavailable: true })).toBe("unavailable");
  });
});
