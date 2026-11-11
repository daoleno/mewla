import { host } from "nsh";
import type { SocketLike } from "../../core/platform";

/**
 * A scripted computer for `nsh test`: the app's real session and Transport talk to it
 * through the platform's socket. It holds Calendar items, Brain's snapshot and Work
 * files, answers the requests the M3 pages make, broadcasts changes as the daemon does,
 * and reports every frame it gets as the host post `app.testFrame` (`app.host.posts()`).
 */
export interface FakeDaemonState {
  calendar_items?: Record<string, unknown>[];
  brain?: Record<string, unknown>;
  work_items?: Record<string, unknown>[];
  live?: Record<string, unknown>;
  /** The Sessions `worker_session_list` sends; `create_session` and `kill_worker` change them. */
  worker_sessions?: Record<string, unknown>[];
  /** Each Session's transcript (by worker id), as `codex_conversation_subscribe` answers it. */
  conversations?: Record<string, Record<string, unknown>[] | { source?: string; available?: boolean; reason?: string; events: Record<string, unknown>[] }>;
  /** A DSH Session's pending requests (`dsh_interaction`). */
  dsh?: { epoch: string; items: Record<string, unknown>[] };
  /** Canned answers by request type (before any built-in one): the reply's `type` and fields, e.g. `{ type: "error", message }`. */
  replies?: Record<string, { type: string } & Record<string, unknown>>;
}

type Frame = { type: string; request_id?: string; [key: string]: unknown };

export class FakeDaemon {
  private sockets = new Set<FakeSocket>();
  private revision = 100;
  private seq = 1;
  private dshRev = 1;
  /** `dsh_interaction` calls held until the requests change, as the daemon holds them. */
  private dshWaiting: Array<() => void> = [];

  constructor(private state: FakeDaemonState) {}

  open(): SocketLike {
    const socket = new FakeSocket(this);
    this.sockets.add(socket);
    setTimeout(() => {
      socket.readyState = 1;
      socket.onopen?.({});
      // What the daemon sends unasked on every connection.
      this.to(socket, { type: "calendar_items_snapshot", calendar_items: this.state.calendar_items ?? [] });
      this.to(socket, { type: "work_items_snapshot", work_items: this.state.work_items ?? [] });
      this.to(socket, { type: "brain_snapshot", brain: this.state.brain ?? {} });
      this.to(socket, { type: "worker_session_list", seq: this.seq, worker_sessions: this.state.worker_sessions ?? [] });
    }, 5);
    return socket;
  }

  closed(socket: FakeSocket): void {
    this.sockets.delete(socket);
  }

  private to(socket: FakeSocket, frame: Frame): void {
    setTimeout(() => socket.onmessage?.({ data: JSON.stringify(frame) }), 1);
  }

  /** Sends `frame` to every connection, as the computer does with its news (a `seq` is added when it has none). */
  push(frame: Frame): void {
    this.broadcast(frame.type.startsWith("worker_session_") && frame.seq === undefined ? { ...frame, seq: ++this.seq } : frame);
  }

  private broadcast(frame: Frame): void {
    this.sockets.forEach((socket) => this.to(socket, frame));
  }

  private item(id: unknown) {
    return (this.state.calendar_items ?? []).find((i) => i.id === id);
  }

  private upsert(item: Record<string, unknown>): Record<string, unknown> {
    const items = (this.state.calendar_items ?? []).filter((i) => i.id !== item.id);
    const next = { ...item, revision: ++this.revision, updated_at: new Date().toISOString() };
    this.state.calendar_items = [...items, next];
    this.broadcast({ type: "calendar_item_changed", calendar_item: next });
    return next;
  }

  receive(socket: FakeSocket, frame: Frame): void {
    host.post("app.testFrame", frame);
    const reply = (type: string, body: Record<string, unknown>) => this.to(socket, { type, request_id: frame.request_id, ...body });
    const fail = (message: string) => reply("error", { message });
    // A canned answer wins over the built-in one: a test makes any request fail this way.
    const canned = this.state.replies?.[frame.type];
    if (canned) return reply(canned.type, canned);
    switch (frame.type) {
      case "brain_snapshot":
        return reply("brain_snapshot", { brain: this.state.brain ?? {} });
      case "list_calendar_items":
        return reply("calendar_items_snapshot", { calendar_items: this.state.calendar_items ?? [] });
      case "get_calendar_item": {
        const item = this.item(frame.id);
        return item ? reply("calendar_item", { calendar_item: item }) : fail("calendar item not found");
      }
      case "create_calendar_item": {
        const draft = frame.calendar_item as Record<string, unknown>;
        if (!draft?.title) return fail("calendar item title is required");
        const at = draft.start_at ?? draft.notify_at ?? draft.due_at;
        const created = this.upsert({ ...draft, id: `new-${this.revision}`, status: "scheduled", next_at: at, created_at: new Date().toISOString() });
        return reply("calendar_item_created", { calendar_item: created });
      }
      case "update_calendar_item": {
        const draft = frame.calendar_item as Record<string, unknown>;
        const existing = this.item(draft?.id);
        if (!existing) return fail("calendar item not found");
        if (existing.revision !== frame.revision) return fail("calendar item changed");
        const at = draft.start_at ?? draft.notify_at ?? draft.due_at;
        return reply("calendar_item_updated", { calendar_item: this.upsert({ ...draft, status: "scheduled", next_at: at }) });
      }
      case "cancel_calendar_item": {
        const existing = this.item(frame.id);
        if (!existing) return fail("calendar item not found");
        return reply("calendar_item_cancelled", { calendar_item: this.upsert({ ...existing, status: "cancelled" }) });
      }
      case "run_calendar_item": {
        const existing = this.item(frame.id);
        if (!existing) return fail("calendar item not found");
        return reply("calendar_item_running", { calendar_item: this.upsert({ ...existing, status: "running" }) });
      }
      case "done_calendar_item":
      case "snooze_calendar_item": {
        const existing = this.item(frame.id);
        if (!existing) return fail("calendar item not found");
        const snoozed = frame.type === "snooze_calendar_item" ? { snoozed_until: new Date(Date.now() + Number(frame.snooze_minutes) * 60_000).toISOString() } : { status: existing.kind === "event" ? existing.status : "completed" };
        return reply("calendar_item_updated", { calendar_item: this.upsert({ ...existing, ...snoozed, acked_at: existing.alerted_at }) });
      }
      case "brain_work_action": {
        const brain = (this.state.brain ?? {}) as { current_work?: Record<string, unknown>[] };
        const work = brain.current_work?.find((w) => w.work_id === frame.id);
        if (!work) return reply("brain_work_action_failed", { message: "This Work no longer exists." });
        const action = frame.action as string;
        if (action === "close" || action === "dismiss" || action === "stop") work.status = action === "close" ? "done" : "cancelled";
        if (action === "reply") Object.assign(work, { question: undefined, choices: undefined, user_action: { kind: "reply", text: frame.text, at: new Date().toISOString(), admission: "accepted" } });
        if (action === "snooze") work.snoozed_until = frame.snooze_until;
        work.unread_result = false;
        reply("brain_work_action", { work_id: frame.id, status: work.status, admission: "accepted" });
        return this.broadcast({ type: "brain_snapshot", brain: this.state.brain ?? {} });
      }
      case "list_work_items":
        return reply("work_items_snapshot", { work_items: this.state.work_items ?? [] });
      case "write_work_item": {
        const items = this.state.work_items ?? [];
        const existing = items.find((w) => w.id === frame.id);
        if (existing && existing.mtime !== frame.base_mtime && new Date(String(existing.mtime)).toISOString() !== frame.base_mtime) return fail("work item changed on disk");
        const written = { ...existing, id: frame.id, project: frame.project, path: frame.path, body: frame.body, frontmatter: frame.frontmatter, mtime: new Date(Date.now() + this.revision++).toISOString() };
        this.state.work_items = [...items.filter((w) => w.id !== frame.id), written];
        return reply("work_item_written", { work_item: written });
      }
      case "delete_work_item":
        this.state.work_items = (this.state.work_items ?? []).filter((w) => w.id !== frame.id);
        return reply("work_item_deleted_ack", { id: frame.id });
      case "live_state_get":
        return reply("live_state", { live: this.state.live ?? null });
      case "widget_token_issue":
        return reply("widget_token", { token: "widget-token", id: "w1" });
      case "codex_conversation_subscribe":
      {
        const entry = this.state.conversations?.[String(frame.worker_id)] ?? [];
        const conversation = Array.isArray(entry) ? { available: true, events: entry } : { available: true, ...entry };
        return reply("codex_conversation_snapshot", { worker_id: frame.worker_id, revision: 1, conversation });
      }
      case "answer_prompt": {
        const answer = frame.prompt_answer as { prompt_id?: string } | undefined;
        reply("prompt_answered", { prompt_id: answer?.prompt_id });
        const answered = (this.state.worker_sessions ?? []).find((w) => w.id === frame.worker_id);
        if (!answered) return;
        const next: Record<string, unknown> = { ...answered, status: "running", prompt: undefined };
        this.state.worker_sessions = (this.state.worker_sessions ?? []).map((w) => (w.id === next.id ? next : w));
        return this.broadcast({ type: "worker_session_updated", seq: ++this.seq, worker_session: next });
      }
      case "dsh_interaction": {
        const dsh = this.state.dsh ?? { epoch: "e0", items: [] };
        const answer = frame.dsh_answer as { rpcId?: string } | undefined;
        if (answer) {
          dsh.items = dsh.items.filter((item) => item.rpcId !== answer.rpcId);
          this.state.dsh = dsh;
          this.dshRev += 1;
          this.dshWaiting.splice(0).forEach((flush) => flush());
          return reply("dsh_interaction", { result: { accepted: true } });
        }
        const send = () => reply("dsh_interaction", { result: { epoch: dsh.epoch, connected: true, items: this.state.dsh?.items ?? [], rev: this.dshRev } });
        if (frame.dsh_after_rev === this.dshRev) return void this.dshWaiting.push(send);
        return send();
      }
      case "list_worker_sessions":
        return this.to(socket, { type: "worker_session_list", seq: this.seq, worker_sessions: this.state.worker_sessions ?? [] });
      case "create_session": {
        const id = `w${++this.revision}`;
        const session = { id, name: String(frame.name || ""), status: "running", cwd: String(frame.cwd || "/home/me"), command: String(frame.command || ""), started_at: Date.now(), updated_at: Date.now() };
        this.state.worker_sessions = [...(this.state.worker_sessions ?? []), session];
        reply("session_created", { worker_id: id, worker_session: session });
        return this.broadcast({ type: "worker_session_created", seq: ++this.seq, worker_session: session });
      }
      case "kill_worker": {
        this.state.worker_sessions = (this.state.worker_sessions ?? []).filter((w) => w.id !== frame.worker_id);
        if (frame.request_id) reply("worker_killed", { worker_id: frame.worker_id });
        return this.broadcast({ type: "worker_session_archived", seq: ++this.seq, worker_session: { id: frame.worker_id } });
      }
      case "live_answer":
        return reply("live_answer", { kind: "brain", text: "ok", duplicate: false });
      default:
        return;
    }
  }
}

class FakeSocket implements SocketLike {
  readyState = 0;
  onopen: SocketLike["onopen"] = null;
  onmessage: SocketLike["onmessage"] = null;
  onclose: SocketLike["onclose"] = null;
  onerror: SocketLike["onerror"] = null;

  constructor(private readonly daemon: FakeDaemon) {}

  send(data: string): void {
    this.daemon.receive(this, JSON.parse(data));
  }

  close(code = 1000, reason = ""): void {
    if (this.readyState === 3) return;
    this.readyState = 3;
    this.daemon.closed(this);
    setTimeout(() => this.onclose?.({ code, reason }), 1);
  }
}
