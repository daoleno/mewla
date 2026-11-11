import { describe, expect, test } from "bun:test";
import type { Frame, RequestOptions } from "../transport";
import type { ArmedAlert, NativeAlertAction } from "./alerts";
import { CalendarModel, type AlertHost, type CalendarTransport } from "./calendar";
import type { CalendarItem } from "./model";

class FakeTransport implements CalendarTransport {
  state = "connected";
  sent: Frame[] = [];
  requests: { type: string; body: Record<string, unknown> }[] = [];
  answer: (type: string, body: Record<string, unknown>) => Frame | Error = (type) => ({ type: "ok", calendar_item: { id: "x" } });
  private listeners = new Map<string, Set<(frame: Frame) => void>>();
  on(type: string, listener: (frame: Frame) => void) {
    const set = this.listeners.get(type) ?? new Set();
    this.listeners.set(type, set);
    set.add(listener);
    return () => set.delete(listener);
  }
  emit(frame: Frame) {
    this.listeners.get(frame.type)?.forEach((l) => l(frame));
  }
  send(frame: Frame) {
    if (this.state !== "connected") throw new Error("Daemon is not connected.");
    this.sent.push(frame);
  }
  async request<T>(type: string, body: Record<string, unknown>, _options: RequestOptions): Promise<T> {
    this.requests.push({ type, body });
    const answer = this.answer(type, body);
    if (answer instanceof Error) throw answer;
    return answer as T;
  }
}

class FakeAlerts implements AlertHost {
  armed: ArmedAlert[][] = [];
  cancelled: string[] = [];
  actions: NativeAlertAction[] = [];
  async schedule(alerts: ArmedAlert[]) { this.armed.push(alerts); }
  async cancelItem(id: string) { this.cancelled.push(id); }
  async takeActions() { return this.actions.splice(0); }
}

const tick = () => new Promise((resolve) => setTimeout(resolve, 5));
const NOW = Date.parse("2026-07-14T09:00:00Z");
const item = (id: string, patch: Partial<CalendarItem> = {}): CalendarItem => ({
  id, title: id, kind: "reminder", status: "scheduled", notify_at: "2026-07-14T15:30:00Z", timezone: "UTC", recurrence: "none",
  next_at: "2026-07-14T15:30:00Z", created_at: "", updated_at: "", revision: 1, ...patch,
});

describe("the current computer's Calendar", () => {
  test("asks for the list when connected and on every reconnect; a snapshot replaces it, a change upserts", () => {
    const transport = new FakeTransport();
    const calendar = new CalendarModel("s1", transport, null, () => NOW);
    expect(transport.sent).toEqual([{ type: "list_calendar_items" }]);
    expect(calendar.loaded).toBe(false);
    transport.emit({ type: "calendar_items_snapshot", calendar_items: [item("b", { next_at: "2026-07-15T00:00:00Z" }), item("a")] });
    expect(calendar.loaded).toBe(true);
    expect(calendar.items.map((i) => i.id)).toEqual(["a", "b"]);
    transport.emit({ type: "calendar_item_changed", calendar_item: item("a", { title: "renamed", next_at: "2026-07-16T00:00:00Z" }) });
    expect(calendar.items.map((i) => i.title)).toEqual(["b", "renamed"]);
    transport.emit({ type: "connected" });
    expect(transport.sent).toHaveLength(2);
  });

  test("arms the phone's alerts from each change, once per burst, and stops when closed", async () => {
    const transport = new FakeTransport();
    const alerts = new FakeAlerts();
    const calendar = new CalendarModel("s1", transport, alerts, () => NOW);
    transport.emit({ type: "calendar_items_snapshot", calendar_items: [item("a"), item("owned", { owner: "watch_brief" })] });
    transport.emit({ type: "calendar_item_changed", calendar_item: item("b", { kind: "event", start_at: "2026-07-14T12:00:00Z", next_at: "2026-07-14T12:00:00Z" }) });
    await tick();
    expect(alerts.armed).toHaveLength(1);
    expect(alerts.armed[0].map((a) => [a.itemId, a.serverId, a.body])).toEqual([["b", "s1", "Starts in 10 min"], ["a", "s1", "Reminder"]]);
    calendar.close();
    transport.emit({ type: "calendar_items_snapshot", calendar_items: [] });
    await tick();
    expect(alerts.armed).toHaveLength(1);
  });

  test("sends each action with the item's revision and says what failed in the daemon's words", async () => {
    const transport = new FakeTransport();
    const calendar = new CalendarModel("s1", transport, new FakeAlerts(), () => NOW);
    await calendar.cancel(item("a", { revision: 7 }));
    expect(transport.requests[0]).toEqual({ type: "cancel_calendar_item", body: { id: "a", revision: 7 } });
    await calendar.update(item("a", { revision: 3, runs: [], notify_at: "old" }), { title: "t", kind: "deadline", timezone: "UTC", recurrence: "daily", due_at: "2026-07-20T10:00:00Z" });
    const sent = transport.requests[1].body as { calendar_item: Record<string, unknown>; revision: number };
    expect(sent.revision).toBe(3);
    expect(sent.calendar_item).toMatchObject({ id: "a", kind: "deadline", due_at: "2026-07-20T10:00:00Z" });
    expect("notify_at" in sent.calendar_item || "runs" in sent.calendar_item).toBe(false);
    transport.answer = () => new Error("event end_at must be after start_at");
    await expect(calendar.create({ title: "x", kind: "event", timezone: "UTC", recurrence: "none" })).rejects.toThrow("event end_at must be after start_at");
    transport.answer = () => new Error("create_calendar_item timed out");
    await expect(calendar.runNow("a")).rejects.toThrow("Calendar request timed out.");
  });

  test("Done and Snooze take the alert down on the phone too", async () => {
    const alerts = new FakeAlerts();
    const calendar = new CalendarModel("s1", new FakeTransport(), alerts, () => NOW);
    await calendar.done("a");
    await calendar.snooze("b", 5);
    expect(alerts.cancelled).toEqual(["a", "b"]);
  });

  test("replays what was done on its alerts with no app running, what is left of a snooze, and keeps another computer's", async () => {
    const transport = new FakeTransport();
    const alerts = new FakeAlerts();
    transport.state = "waiting";
    const calendar = new CalendarModel("s1", transport, alerts, () => NOW);
    alerts.actions = [
      { type: "done", itemId: "a", serverId: "s1", minutes: 0, atMs: NOW - 60_000 },
      { type: "snooze", itemId: "b", serverId: "s1", minutes: 15, atMs: NOW - 4 * 60_000 },
      { type: "snooze", itemId: "c", serverId: "s1", minutes: 5, atMs: NOW - 10 * 60_000 },
      { type: "done", itemId: "z", serverId: "s2", minutes: 0, atMs: NOW },
    ];
    await calendar.replayNativeActions();
    expect(transport.requests).toEqual([]);
    transport.state = "connected";
    transport.emit({ type: "connected" });
    await tick();
    expect(transport.requests).toEqual([
      { type: "done_calendar_item", body: { id: "a" } },
      { type: "snooze_calendar_item", body: { id: "b", snooze_minutes: 11 } },
    ]);
    calendar.close();
    const other = new FakeTransport();
    new CalendarModel("s2", other, alerts, () => NOW);
    await tick();
    expect(other.requests).toEqual([{ type: "done_calendar_item", body: { id: "z" } }]);
  });

  // FINAL-VERIFY 6387f88f m3 162–165 / 79: Done on the full-screen alert, then the app asked again.
  test("an alert answered on the phone is not asked again: asking waits for the phone's answers, which count until the computer has them", async () => {
    let release: (actions: NativeAlertAction[]) => void = () => {};
    const alerts = new FakeAlerts();
    alerts.takeActions = () => new Promise<NativeAlertAction[]>((resolve) => (release = resolve));
    const transport = new FakeTransport();
    let reply: (frame: Frame) => void = () => {};
    transport.request = <T,>(type: string, body: Record<string, unknown>) => {
      transport.requests.push({ type, body });
      return new Promise<T>((resolve) => (reply = resolve as (frame: Frame) => void));
    };
    const fired = "2026-07-14T08:55:00Z";
    const calendar = new CalendarModel("s1", transport, alerts, () => NOW);
    transport.emit({ type: "calendar_items_snapshot", calendar_items: [item("a", { alerted_at: fired })] });
    expect(calendar.alertsSettled()).toBe(false);
    release([{ type: "done", itemId: "a", serverId: "s1", minutes: 0, atMs: NOW - 60_000 }]);
    await tick();
    expect(calendar.alertsSettled()).toBe(true);
    expect(transport.requests).toEqual([{ type: "done_calendar_item", body: { id: "a" } }]);
    expect(calendar.answered("a")).toBe(true);
    reply({ type: "calendar_item_updated", calendar_item: item("a", { alerted_at: fired, acked_at: fired, status: "completed", revision: 2 }) });
    await tick();
    expect(calendar.answered("a")).toBe(false);
    expect(calendar.item("a")).toMatchObject({ acked_at: fired, revision: 2 });
  });
});
