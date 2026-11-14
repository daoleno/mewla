import { describe, expect, test } from "bun:test";
import { SCREENSHOT_CHAT_ACTIVITY_EVENTS } from "../timeline/chatActivityFixtures";
import type { CodexConversationEvent } from "./conversation";
import { initialInterfaceChatThreadState, type InterfaceChatThreadState, type PendingUserMessage } from "./thread";
import { createChatTimeline, isWorkingRow } from "./timeline";

function state(events: CodexConversationEvent[], extra: Partial<InterfaceChatThreadState> = {}, running = false): InterfaceChatThreadState {
  return {
    ...initialInterfaceChatThreadState("test:timeline"),
    loading: false,
    conversation: { available: true, events, activity: running ? { id: "turn-1", status: "running" } as never : undefined },
    ...extra,
  };
}

const pending = (id: string, lifecycle: PendingUserMessage["lifecycle"]): PendingUserMessage => ({
  id, body: "and then?", sentText: "and then?", attachments: [], createdAt: "2026-06-18T09:31:00.000Z", lifecycle,
  dispatchRequestId: `req-${id}`, dispatchAttemptOrder: 1, createdAfterEventIds: ["activity-assistant"],
});

describe("chat timeline", () => {
  test("a full window slides (the newest event in, the oldest out): every row that stays is the same object", () => {
    // The daemon keeps a conversation's newest 240 events; each new one comes with the oldest deleted.
    const event = (i: number): CodexConversationEvent => ({ id: `m${i}`, seq: i + 1, kind: i % 2 ? "assistant_message" : "user_message", timestamp: new Date(Date.UTC(2026, 10, 14, 1, 0, i)).toISOString(), body: `Message ${i}` }) as never;
    const timeline = createChatTimeline("brain");
    const before = timeline(state(Array.from({ length: 240 }, (_, i) => event(i))));
    const after = timeline(state(Array.from({ length: 240 }, (_, i) => event(i + 1))));
    expect(after.map((item) => item.id)).toEqual([...before.slice(1).map((item) => item.id), "m240"]);
    for (let i = 1; i < before.length; i += 1) expect(after[i - 1]).toBe(before[i]);
  });

  test("a Session's finished tool run folds into one row; messages stay", () => {
    const items = createChatTimeline("session")(state(SCREENSHOT_CHAT_ACTIVITY_EVENTS));
    expect(items[0]).toMatchObject({ type: "message", role: "user" });
    expect(items.some((item) => item.type === "message" && item.role === "assistant")).toBe(true);
    const activities = items.filter((item) => item.type === "activity");
    expect(activities.length).toBeLessThan(SCREENSHOT_CHAT_ACTIVITY_EVENTS.filter((e) => e.kind !== "user_message" && e.kind !== "assistant_message").length);
  });

  test("Working shows while a reply runs, not while a question waits", () => {
    const timeline = createChatTimeline("brain");
    const events = SCREENSHOT_CHAT_ACTIVITY_EVENTS.slice(0, 1);
    expect(timeline(state(events, {}, true)).some(isWorkingRow)).toBe(true);
    const asking: CodexConversationEvent = {
      id: "ask", seq: 2, call_id: "ask", kind: "tool", tool_name: "AskUserQuestion", status: "running", timestamp: "2026-06-18T09:30:01.000Z",
      choice: { call_id: "ask", state: "pending", questions: [{ question: "Color?", options: [{ label: "Blue" }] }] },
    } as never;
    expect(timeline(state([...events, asking], {}, true)).some(isWorkingRow)).toBe(false);
  });

  test("what was sent shows after the reply it follows; a failed one can be sent again", () => {
    const actions: string[] = [];
    const items = createChatTimeline("session")(state(SCREENSHOT_CHAT_ACTIVITY_EVENTS, { pendingUserMessages: [pending("p1", "failed")] }), {
      onPendingAction: (id, action) => actions.push(`${id}:${action}`),
    });
    const at = items.findIndex((item) => item.id === "p1");
    const reply = items.findIndex((item) => item.id === "activity-assistant");
    expect(at).toBeGreaterThan(reply);
    const row = items[at];
    if (row.type !== "message") throw new Error("not a message");
    row.onRetryPending?.();
    expect(actions).toEqual(["p1:retry"]);
  });

  test("unchanged rows keep their objects; a streamed reply replaces only its own", () => {
    const timeline = createChatTimeline("session");
    const first = timeline(state(SCREENSHOT_CHAT_ACTIVITY_EVENTS));
    expect(timeline(state(SCREENSHOT_CHAT_ACTIVITY_EVENTS))).toBe(first);
    const events = SCREENSHOT_CHAT_ACTIVITY_EVENTS.map((e) => (e.id === "activity-assistant" ? { ...e, body: `${e.body} More.` } : e));
    const next = timeline(state(events));
    expect(next).not.toBe(first);
    for (const [index, item] of next.entries()) {
      if (item.id === "activity-assistant") expect(item).not.toBe(first[index]);
      else if (item.type === "message") expect(item).toBe(first.find((f) => f.id === item.id)!);
    }
  });
});

describe("Brain's tool rows", () => {
  // Brain's own calls, as its host Session recorded them on 11-12 (BRAIN-LOOK item 5).
  const M = "M=/home/me/.mewla/bin/mewla;";
  const command = (id: string, seq: number, text: string, status = "completed"): CodexConversationEvent =>
    ({ id, seq, kind: "command", command: text, body: "ok", status, exit_code: status === "failed" ? 1 : 0, timestamp: `2026-11-12T17:28:${10 + seq}.000Z` }) as CodexConversationEvent;
  const said = (id: string, seq: number, kind: "assistant_message" | "update", body: string): CodexConversationEvent =>
    ({ id, seq, kind, role: "assistant", body, timestamp: `2026-11-12T17:28:${10 + seq}.000Z` }) as CodexConversationEvent;
  const activities = (items: ReturnType<ReturnType<typeof createChatTimeline>>) => items.filter((item) => item.type === "activity");

  test("a turn's run folds as a Session's does: what it did, its kind's icon, its files, no steps word", () => {
    const events = [
      command("c1", 1, "cat routing.md | head -60; echo ----; cat policies/delegation.md | head -60"),
      command("c2", 2, `${M} $M brain work create -title "Hello file" | head -c 600`),
      said("a", 3, "assistant_message", "Opened it."),
    ];
    const brain = activities(createChatTimeline("brain")(state(events)));
    const session = activities(createChatTimeline("session")(state(events)));
    expect(brain).toEqual(session);
    expect(brain).toHaveLength(1);
    const run = brain[0]!;
    if (run.type !== "activity") throw new Error("not a tool row");
    expect(run).toMatchObject({ title: "Ran 1 command, read 1 file", detail: "routing.md", icon: "terminal" });
    expect(run.steps?.map((step) => step.id)).toEqual(["explore:c1", "c2"]);
    expect(`${run.title} ${run.detail} ${run.icon}`).not.toMatch(/steps|layers|Worked/);
  });

  test("a single call is its own row and says what it ran, not a bare Read", () => {
    const spawn = `${M} $M worker spawn -name "Hello writer" -work f97f32b9 -prompt "Create hello.txt. Report back with the output of: cat -A hello.txt" | head -c 800`;
    const rows = activities(createChatTimeline("brain")(state([said("a", 1, "assistant_message", "Sonnet at low."), command("c3", 2, spawn)])));
    expect(rows).toHaveLength(1);
    expect(rows[0]!.type === "activity" && rows[0]!.title).toMatch(/^Run \$M worker spawn/);
  });

  test("Brain's words between steps and a running call stay rows of their own", () => {
    const events = [
      command("c1", 1, "cat a.md"), command("c2", 2, "git status"),
      said("u", 3, "update", "Quota is now 256."),
      command("c3", 4, "cat c.md"), command("c4", 5, "go test ./...", "running"),
    ];
    const rows = createChatTimeline("brain")(state(events)).filter((row) => !isWorkingRow(row));
    expect(rows.map((row) => (row.type === "activity" && row.activityKind !== "update" ? row.title : row.id))).toEqual(["Ran 1 command, read 1 file", "u", "Read c.md", expect.stringMatching(/^Run/)]);
  });
});


describe("a row stays the same object while it is the same (owner 11-13: Brain's whole thread flashed)", () => {
  const work: CodexConversationEvent = {
    id: "wr-1", seq: 3, timestamp: "2026-11-13T01:00:00.000Z", kind: "status", title: "Daily digest", body: "Done.", status: "session.done",
    source: "work_result", work_id: "work-1", work_session_id: "w9", session_name: "digest", unread: true, work_review_state: "queued", work_session_state: "open", work_result_current: true,
  } as CodexConversationEvent;
  const events: CodexConversationEvent[] = [
    { id: "u1", seq: 1, kind: "user_message", timestamp: "2026-11-13T00:59:00.000Z", body: "Run the digest" } as CodexConversationEvent,
    { id: "a1", seq: 2, kind: "assistant_message", timestamp: "2026-11-13T00:59:05.000Z", body: "On it." } as CodexConversationEvent,
    work,
  ];

  test("each Brain snapshot projects the thread again with new handlers: the Work card and pending rows are kept", () => {
    const timeline = createChatTimeline("brain");
    const thread = state(events, { pendingUserMessages: [pending("p1", "failed")] });
    // Home makes its options anew each time: a new handler and a new Set of the computer's Sessions.
    const project = () => timeline(thread, { onWorkEvent: () => {}, onPendingAction: () => {}, openSessionIds: new Set(["w9"]) });
    const first = project();
    expect(first.some((item) => item.type === "brain-work-event")).toBe(true);
    expect(project()).toBe(first);
  });

  test("a Work card whose Session goes away is a new row (its press now leads to its Work)", () => {
    const timeline = createChatTimeline("brain");
    const thread = state(events);
    const first = timeline(thread, { onWorkEvent: () => {}, openSessionIds: new Set(["w9"]) });
    const next = timeline(thread, { onWorkEvent: () => {}, openSessionIds: new Set() });
    const card = (items: typeof first) => items.find((item) => item.type === "brain-work-event");
    expect(card(next)).not.toBe(card(first));
    expect(next[0]).toBe(first[0]);
  });
});
