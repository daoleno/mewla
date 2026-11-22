import { describe, expect, test } from "bun:test";
import type { Frame, RequestOptions } from "../transport";
import { askDetail, attentionWords, gapLine, NOT_HANDLED_AFTER_MS, triggerAttention, triggerGroups, triggerLine, triggersNeedingYou, type Trigger } from "./model";
import { TriggersModel, type TriggersTransport } from "./triggers";

class FakeTransport implements TriggersTransport {
  state = "connected";
  sent: Frame[] = [];
  requests: { type: string; body: Record<string, unknown>; options: RequestOptions }[] = [];
  answer: (type: string, body: Record<string, unknown>) => Frame | Error = () => ({ type: "ok" });
  private listeners = new Map<string, Set<(frame: Frame) => void>>();
  on(type: string, listener: (frame: Frame) => void) {
    const set = this.listeners.get(type) ?? new Set();
    this.listeners.set(type, set);
    set.add(listener);
    return () => set.delete(listener);
  }
  emit(frame: Frame) {
    this.listeners.get(frame.type)?.forEach((listener) => listener(frame));
  }
  send(frame: Frame) {
    this.sent.push(frame);
  }
  async request<T>(type: string, body: Record<string, unknown>, options: RequestOptions): Promise<T> {
    this.requests.push({ type, body, options });
    const answer = this.answer(type, body);
    if (answer instanceof Error) throw answer;
    return answer as T;
  }
}

const NOW = Date.parse("2026-11-18T12:00:00Z");
const ago = (ms: number) => new Date(NOW - ms).toISOString();
const MIN = 60_000;
const HOUR = 3_600_000;

function trigger(id: string, patch: Partial<Trigger> = {}): Trigger {
  return {
    id, title: id, check: "./check.sh", interval_seconds: 600, timeout_seconds: 60, work_id: "w",
    created_at: ago(10 * HOUR), updated_at: ago(MIN), revision: 1, state: "watching", ...patch,
  };
}

describe("a Trigger's row", () => {
  test("says how often it checks and when it last did, before anything fired", () => {
    expect(triggerLine(trigger("a"), NOW)).toBe("Every 10 min · not checked yet");
    expect(triggerLine(trigger("a", { last_checked_at: ago(2 * MIN) }), NOW)).toBe("Every 10 min · checked 2 min ago");
  });

  test("its newest firing: with Brain, then not handled after three hours; handled; waiting to reach Brain; lost", () => {
    const fired = (patch: Partial<NonNullable<Trigger["firings"]>[number]>, at = ago(5 * MIN)) =>
      trigger("a", { last_checked_at: ago(MIN), firings: [{ seq: 1, at, outputs: 1, payload: "/p/1.txt", event_id: "e1", ...patch }] });
    expect(triggerLine(fired({}), NOW)).toBe("Every 10 min · fired 5 min ago · with Brain");
    expect(triggerAttention(fired({}), NOW)).toBeNull();
    const late = fired({}, ago(NOT_HANDLED_AFTER_MS));
    expect(triggerLine(late, NOW)).toBe("Every 10 min · fired 3 h ago · not handled");
    expect(triggerAttention(late, NOW)).toBe("not_handled");
    expect(attentionWords(late, NOW)).toBe("It fired 3 h ago and Brain has not handled it.");
    expect(triggerLine(fired({ handled_at: ago(MIN) }, ago(4 * HOUR)), NOW)).toBe("Every 10 min · fired 4 h ago · handled");
    expect(triggerAttention(fired({ handled_at: ago(MIN) }, ago(4 * HOUR)), NOW)).toBeNull();
    expect(triggerLine(fired({ event_id: undefined }), NOW)).toBe("Every 10 min · fired 5 min ago · waiting to reach Brain");
    expect(triggerAttention(fired({ lost: true }), NOW)).toBe("not_handled");
  });

  test("failing and the runaway pause lead the row and need you", () => {
    const failing = trigger("a", { failing: true, failures: 4, last_failure: "exit status 1\ngh: HTTP 502", state: "failing", last_checked_at: ago(MIN) });
    expect(triggerLine(failing, NOW)).toBe("Failing: gh: HTTP 502 · checked 1 min ago");
    expect(triggerAttention(failing, NOW)).toBe("failing");
    expect(attentionWords(failing, NOW)).toBe("Its check keeps failing (4 in a row): gh: HTTP 502");
    const runaway = trigger("b", { paused: true, pause_reason: "runaway", state: "paused" });
    expect(triggerLine(runaway, NOW)).toBe("Paused: woke Brain 6 times in an hour");
    expect(triggerAttention(runaway, NOW)).toBe("runaway");
    const paused = trigger("c", { paused: true, state: "paused" });
    expect(triggerLine(paused, NOW)).toBe("Paused");
    expect(triggerAttention(paused, NOW)).toBeNull();
    expect(triggerAttention({ ...failing, paused: true, state: "paused" }, NOW)).toBeNull();
    expect(triggersNeedingYou([failing, runaway, paused], NOW)).toEqual([
      { id: "a", title: "a", ask: "Check keeps failing" },
      { id: "b", title: "b", ask: "Paused itself" },
    ]);
  });

  test("a gap shows for a day after it ended", () => {
    const gap = trigger("a", { gap_from: ago(3 * HOUR), gap_to: ago(HOUR) });
    expect(gapLine(gap, NOW)).toBe("Not checked for 2 h: Mewla was not running");
    expect(gapLine(trigger("a", { gap_from: ago(30 * HOUR), gap_to: ago(25 * HOUR) }), NOW)).toBe("");
  });
});

describe("the current computer's Triggers", () => {
  test("asks for the list on a link already up; the snapshot replaces it, a change upserts, a removal drops", () => {
    const transport = new FakeTransport();
    const model = new TriggersModel("s1", transport);
    expect(transport.sent).toEqual([{ type: "list_triggers" }]);
    let changes = 0;
    model.onChange(() => changes++);
    transport.emit({ type: "triggers_snapshot", triggers: [trigger("b", { created_at: ago(HOUR) }), trigger("a")] });
    expect(model.loaded).toBe(true);
    expect(model.triggers.map((t) => t.id)).toEqual(["a", "b"]);
    transport.emit({ type: "trigger_changed", trigger: trigger("a", { title: "renamed", updated_at: ago(0) }) });
    expect(model.trigger("a")?.title).toBe("renamed");
    // An older copy never replaces a newer one.
    transport.emit({ type: "trigger_changed", trigger: trigger("a", { title: "stale", updated_at: ago(HOUR) }) });
    expect(model.trigger("a")?.title).toBe("renamed");
    transport.emit({ type: "trigger_removed", trigger_id: "b" });
    expect(model.triggers.map((t) => t.id)).toEqual(["a"]);
    expect(changes).toBe(3);
  });

  test("pause, resume and check apply their reply; a check may take its whole timeout; delete drops it", async () => {
    const transport = new FakeTransport();
    const model = new TriggersModel("s1", transport);
    transport.emit({ type: "triggers_snapshot", triggers: [trigger("a")] });
    transport.answer = (type) => (type === "delete_trigger" ? { type: "trigger_removed", trigger_id: "a" } : { type: "trigger_updated", trigger: trigger("a", { paused: type === "pause_trigger", state: type === "pause_trigger" ? "paused" : "watching", updated_at: ago(0) }) });
    await model.pause("a");
    expect(model.trigger("a")?.paused).toBe(true);
    await model.resume("a");
    expect(model.trigger("a")?.paused).toBe(false);
    await model.checkNow("a");
    expect(transport.requests.map((r) => [r.type, r.body.id, r.options.reply])).toEqual([
      ["pause_trigger", "a", "trigger_updated"], ["resume_trigger", "a", "trigger_updated"], ["check_trigger", "a", "trigger_updated"],
    ]);
    expect(transport.requests[2]!.options.timeoutMs).toBeGreaterThanOrEqual(10 * 60_000);
    await model.delete("a");
    expect(model.triggers).toEqual([]);
  });

  test("a refused request says the computer's reason", async () => {
    const transport = new FakeTransport();
    const model = new TriggersModel("s1", transport);
    transport.answer = () => new Error("invalid trigger: the Trigger is paused; resume it first");
    await expect(model.checkNow("a")).rejects.toThrow("the Trigger is paused; resume it first");
    transport.answer = () => new Error("Request timed out");
    await expect(model.pause("a")).rejects.toThrow("The computer did not answer in time.");
  });
});

describe("the Triggers page", () => {
  const day = (at: number) => new Date(at).toISOString().slice(0, 10);
  const shortDate = (at: number) => new Date(at).toISOString().slice(5, 10);
  const firing = (seq: number, at: string) => ({ seq, at, outputs: 1, payload: "", event_id: `e${seq}`, handled_at: at });

  test("groups what needs you, what noticed something today (newest first, how often) and what is quiet", () => {
    const list = [
      trigger("quiet-old", { firings: [firing(1, "2026-11-16T09:00:00Z")] }),
      trigger("never"),
      trigger("failing", { failing: true, failures: 3, last_failure: "the disk did not answer" }),
      trigger("busy", { firings: [firing(1, "2026-11-17T09:00:00Z"), firing(2, "2026-11-18T08:00:00Z"), firing(3, "2026-11-18T11:30:00Z")] }),
      trigger("once", { firings: [firing(1, "2026-11-18T11:50:00Z")] }),
      trigger("runaway", { paused: true, pause_reason: "runaway" }),
      trigger("paused", { paused: true }),
      trigger("quiet-new", { firings: [firing(1, "2026-11-17T09:00:00Z")] }),
    ];
    const groups = triggerGroups(list, NOW, day, shortDate);
    expect(groups.needs.map((row) => [row.trigger.id, row.line])).toEqual([["failing", "Its check keeps failing"], ["runaway", "It paused itself"]]);
    expect(groups.today.map((row) => [row.trigger.id, row.line])).toEqual([["once", "Noticed something today"], ["busy", "Noticed something twice today"]]);
    expect(groups.quiet.map((row) => [row.trigger.id, row.line])).toEqual([["quiet-new", "Last noticed 11-17"], ["quiet-old", "Last noticed 11-16"], ["never", "Nothing noticed yet"], ["paused", "Paused"]]);
    expect(groups.watching).toBe(6);
  });

  test("a firing Brain has not handled is Brain's: it never puts the Trigger with what needs you", () => {
    const open = trigger("open", { firings: [{ seq: 1, at: ago(4 * HOUR), outputs: 1, payload: "", event_id: "e" }] });
    expect(triggerAttention(open, NOW)).toBe("not_handled");
    expect(triggerGroups([open], NOW, day, shortDate).needs).toEqual([]);
  });

  test("the ask card says how long and why", () => {
    const failing = trigger("f", { failing: true, failures: 3, failing_since: "2026-11-18T11:46:00Z", last_failure: "the disk did not answer" });
    expect(askDetail(failing, (at) => new Date(at).toISOString().slice(11, 16))).toBe("3 times in a row since 11:46: the disk did not answer");
    // The daemon's failure is its reason ("exit status 1", "timed out after 30s"), then what the check printed: the app
    // shows the check's own words, never an exit code (gate 0.4.21).
    const time = (at: number) => new Date(at).toISOString().slice(11, 16);
    const said = trigger("s", { failing: true, failures: 6, failing_since: "2026-11-18T15:02:00Z", last_failure: "exit status 1\nthe disk did not answer" });
    expect(askDetail(said, time)).toBe("6 times in a row since 15:02: the disk did not answer");
    const silent = trigger("q", { failing: true, failures: 6, failing_since: "2026-11-18T15:02:00Z", last_failure: "exit status 2" });
    expect(askDetail(silent, time)).toBe("6 times in a row since 15:02: it stopped without saying why");
    expect(attentionWords(silent, NOW)).toBe("Its check keeps failing (6 in a row): it stopped without saying why");
    expect(triggerLine({ ...silent, last_checked_at: undefined }, NOW)).toMatch(/^Failing: it stopped without saying why/);
    const slow = trigger("t", { failing: true, failures: 3, failing_since: "2026-11-18T15:02:00Z", last_failure: "timed out after 30s" });
    expect(askDetail(slow, time)).toBe("3 times in a row since 15:02: timed out after 30s");
    expect(askDetail(trigger("r", { paused: true, pause_reason: "runaway" }), () => "")).toBe("It woke Brain 6 times in an hour. Its latest change waits until you resume it.");
  });
});
