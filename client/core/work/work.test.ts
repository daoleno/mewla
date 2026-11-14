import { describe, expect, test } from "bun:test";
import type { Frame, RequestOptions } from "../transport";
import { markWorkRead, objectiveLine, workAction, workOfSnapshot } from "./brainWork";
import { readableBody, WorkItemConflict, WorkItems, workItemStatus } from "./workItems";

class FakeTransport {
  state = "connected";
  sent: Frame[] = [];
  requests: { type: string; body: Record<string, unknown>; reply: string }[] = [];
  answer: (type: string) => Frame | Error = (type) => ({ type });
  private listeners = new Map<string, Set<(frame: Frame) => void>>();
  on(type: string, listener: (frame: Frame) => void) {
    const set = this.listeners.get(type) ?? new Set();
    this.listeners.set(type, set);
    set.add(listener);
    return () => set.delete(listener);
  }
  emit(frame: Frame) { this.listeners.get(frame.type)?.forEach((l) => l(frame)); }
  send(frame: Frame) { this.sent.push(frame); }
  async request<T>(type: string, body: Record<string, unknown>, options: RequestOptions): Promise<T> {
    this.requests.push({ type, body, reply: options.reply });
    const a = this.answer(type);
    if (a instanceof Error) throw a;
    return a as T;
  }
}

describe("Brain's Work in the snapshot", () => {
  test("keeps well-formed Work, drops finished open Work, caps the objective's count", () => {
    const work = workOfSnapshot({
      current_work: [{ work_id: "w1", title: "a" }, { title: "no id" }],
      open_work: [{ work_id: "o1", status: "waiting" }, { work_id: "o2", status: "done" }],
      objective: { title: "Ship the beta", total: 5, back: 7 },
    });
    expect(work.current.map((w) => w.work_id)).toEqual(["w1"]);
    expect(work.open.map((w) => w.work_id)).toEqual(["o1"]);
    expect(objectiveLine(work.objective)).toBe("Ship the beta · 5 of 5 back");
    expect(workOfSnapshot(null)).toEqual({ current: [], open: [], objective: null });
  });

  test("an action says what it is, a snooze how long, and a timeout that it may have reached Brain", async () => {
    const transport = new FakeTransport();
    transport.answer = () => ({ type: "brain_work_action", status: "running", admission: "uncertain" });
    expect(await workAction(transport, "w1", "reply", { text: "Keep both" })).toEqual({ status: "running", admission: "uncertain" });
    await workAction(transport, "w1", "snooze", { snoozeSeconds: 3600 });
    expect(transport.requests.map((r) => r.body)).toEqual([
      { id: "w1", action: "reply", text: "Keep both" },
      { id: "w1", action: "snooze", snooze_seconds: 3600 },
    ]);
    transport.answer = () => new Error("brain_work_action timed out");
    await expect(workAction(transport, "w1", "close")).rejects.toThrow("Timed out. The action may still have reached Brain.");
    transport.answer = () => new Error("This Work just changed. Look again and retry.");
    await expect(workAction(transport, "w1", "close")).rejects.toThrow("This Work just changed. Look again and retry.");
    markWorkRead(transport, "w1");
    expect(transport.sent).toEqual([{ type: "brain_work_read", id: "w1" }]);
  });
});

const raw = (patch: Record<string, unknown> = {}) => ({
  id: "run-1", project: "calendar", title: "Current scheduled Work", body: "# Current scheduled Work\n\nDo it.\n\n## Calendar metadata\n- item: x\n",
  frontmatter: { id: "run-1", kind: "calendar_action", created: "2026-07-17T03:00:00Z", status: "running", worker_session: "agent-1", extra: { legacy_note: "inert" } },
  mtime: "2026-07-17T03:01:00Z", ...patch,
});

describe("the Markdown Work files", () => {
  test("follow the snapshot and the broadcasts, keeping the front matter and the Session they belong to", () => {
    const transport = new FakeTransport();
    const items = new WorkItems(transport);
    expect(transport.sent).toEqual([{ type: "list_work_items" }]);
    transport.emit({ type: "work_items_snapshot", work_items: [raw()] });
    const item = items.items.get("run-1")!;
    expect(item.frontmatter).toMatchObject({ kind: "calendar_action", worker_session: "agent-1", extra: { legacy_note: "inert" } });
    expect(items.linkedTo("agent-1")).toBe(item);
    expect(workItemStatus(item)).toBe("running");
    expect(readableBody(item.body)).toBe("Do it.");
    transport.emit({ type: "work_item_deleted", path: item.path, id: "run-1" });
    expect(items.items.size).toBe(0);
  });

  test("status: an explicit one, else done, else started, else queued", () => {
    expect(workItemStatus({ frontmatter: { id: "", created: "", done: "x" } })).toBe("done");
    expect(workItemStatus({ frontmatter: { id: "", created: "", started: "x" } })).toBe("running");
    expect(workItemStatus({ frontmatter: { id: "", created: "" } })).toBe("queued");
  });

  test("a save names the version it edited; a newer one on the computer is a conflict, not an overwrite", async () => {
    const transport = new FakeTransport();
    const items = new WorkItems(transport);
    transport.emit({ type: "work_items_snapshot", work_items: [raw()] });
    const item = items.items.get("run-1")!;
    transport.answer = () => ({ type: "work_item_written", work_item: raw({ body: "new", mtime: "2026-07-17T04:00:00Z" }) });
    const saved = await items.save(item, { body: "new" });
    expect(transport.requests[0].body).toMatchObject({ id: "run-1", body: "new", base_mtime: "2026-07-17T03:01:00.000Z" });
    expect(saved.mtime).toBe("2026-07-17T04:00:00.000Z");
    transport.answer = () => new Error("work item changed on disk");
    await expect(items.save(item, { body: "stale" })).rejects.toBeInstanceOf(WorkItemConflict);
    transport.answer = () => ({ type: "work_item_written", work_item: raw() });
    await items.setDone(saved, true, new Date("2026-07-18T00:00:00Z"));
    expect((transport.requests[2].body.frontmatter as Record<string, unknown>).done).toBe("2026-07-18T00:00:00.000Z");
    expect("status" in (transport.requests[2].body.frontmatter as Record<string, unknown>)).toBe(false);
  });
});
