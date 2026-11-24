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
  /** The computer's Triggers (`triggers_snapshot`); pause, resume, check and delete change them and broadcast. */
  triggers?: Record<string, unknown>[];
  brain?: Record<string, unknown>;
  work_items?: Record<string, unknown>[];
  live?: Record<string, unknown>;
  /** The Sessions `worker_session_list` sends; `create_session` and `kill_worker` change them. */
  worker_sessions?: Record<string, unknown>[];
  /** Agent programs this computer does not have, each with the product name and install command `create_session`'s refusal carries (the daemon's agent_not_installed). */
  missing_agents?: Record<string, { agent: string; install?: string }>;
  /** Each Session's transcript (by worker id), as `codex_conversation_subscribe` answers it. */
  conversations?: Record<string, Record<string, unknown>[] | { source?: string; available?: boolean; reason?: string; events: Record<string, unknown>[] }>;
  /** A DSH Session's pending requests (`dsh_interaction`). */
  dsh?: { epoch: string; items: Record<string, unknown>[] };
  /** The `computers` message (WIRE §5) without its type: the other computers as this one sees them. */
  computers?: Record<string, unknown>;
  /** Canned answers by request type (before any built-in one): the reply's `type` and fields, e.g. `{ type: "error", message }`. */
  replies?: Record<string, { type: string } & Record<string, unknown>>;
  /** Pictures by path, as `codex_asset` answers them (`data_url`); a path not here falls to `replies`. */
  assets?: Record<string, string>;
  /** The first this many `codex_asset` asks are lost with the link: it drops instead of answering (a relay that went away mid-picture). */
  assetsLost?: number;
  /**
   * `send_input` is admitted as the daemon admits it: once per request id, echoed to the conversation as a
   * user message whose id is that request id, then `input_sent`. loseFirstAck: the first one is admitted and
   * echoed, but the link drops before its answer (a slow phone), so the app sends it again under the same id.
   */
  admitsInput?: { loseFirstAck?: boolean; /** The computer's answer to the message: an assistant message in the same conversation, right after the echo. */ reply?: string };
  /**
   * What's New's record as the daemon keeps it (daemon/whatsnew): `whats_new_get` answers it (with `releases`, the
   * computer's notes, when given), `whats_new_seen` adds ids and ends a replay, `whats_new_note` adds an update.
   * Without it the computer is one from before What's New and answers nothing.
   */
  whats_new?: { baseline: string; seen?: string[]; updates?: { version: string; at: number }[]; replay?: boolean; releases?: unknown };
}

type Frame = { type: string; request_id?: string; [key: string]: unknown };

/** How many older events one delta carries after a first page (the daemon's codexConversationOlderPageEvents). */
const OLDER_PAGE_EVENTS = 40;

export class FakeDaemon {
  private sockets = new Set<FakeSocket>();
  private revision = 100;
  private seq = 1;
  private dshRev = 1;
  /** `dsh_interaction` calls held until the requests change, as the daemon holds them. */
  private dshWaiting: Array<() => void> = [];
  /** Request ids of the `send_input`s admitted so far: a resend under one is answered, never stored again. */
  private admitted = new Set<string>();
  private ackLost = false;

  constructor(private state: FakeDaemonState) {}

  /** The computer went away (`goDown`): every connection closes and new ones fail. */
  private down = false;

  goDown(): void {
    this.down = true;
    for (const socket of [...this.sockets]) socket.close(1006, "gone");
  }

  /** A connection refused before it opens: nothing listens at that address (FakeDaemonApp's `refuseSockets`). */
  refused(): SocketLike {
    const socket = new FakeSocket(this);
    setTimeout(() => socket.close(1006, "Connection refused"), 3);
    return socket;
  }

  open(): SocketLike {
    const socket = new FakeSocket(this);
    if (this.down) {
      setTimeout(() => socket.close(1006, "gone"), 5);
      return socket;
    }
    this.sockets.add(socket);
    setTimeout(() => {
      socket.readyState = 1;
      socket.onopen?.({});
      // What the daemon sends unasked on every connection.
      this.to(socket, { type: "calendar_items_snapshot", calendar_items: this.state.calendar_items ?? [] });
      this.to(socket, { type: "triggers_snapshot", triggers: this.state.triggers ?? [] });
      this.to(socket, { type: "work_items_snapshot", work_items: this.state.work_items ?? [] });
      this.to(socket, { type: "brain_snapshot", brain: this.state.brain ?? {} });
      this.to(socket, { type: "worker_session_list", seq: this.seq, worker_sessions: this.state.worker_sessions ?? [] });
    }, 5);
    return socket;
  }

  closed(socket: FakeSocket): void {
    this.sockets.delete(socket);
    this.subscriptions = this.subscriptions.filter((s) => s.socket !== socket);
  }

  /** While set, what `to` would send is kept here instead (`newsThenDrop`). */
  private held: Array<[FakeSocket, Frame]> | null = null;

  private to(socket: FakeSocket, frame: Frame): void {
    if (this.held) return void this.held.push([socket, frame]);
    setTimeout(() => socket.onmessage?.({ data: JSON.stringify(frame) }), 1);
  }

  /**
   * The computer's own news, as it sends it to every connection: a Session's update (a `seq` is added
   * when it has none), or a change made on the computer itself (a Work file edited or removed there).
   */
  push(frame: Frame): void {
    if (frame.type === "work_item_deleted") this.state.work_items = (this.state.work_items ?? []).filter((w) => w.id !== frame.id);
    if (frame.type === "work_item_changed") this.state.work_items = [...(this.state.work_items ?? []).filter((w) => w.id !== frame.id), frame.work_item as never];
    this.broadcast(frame.type.startsWith("worker_session_") && frame.seq === undefined ? { ...frame, seq: ++this.seq } : frame);
  }

  /** Who follows which conversation: deltas and re-sent snapshots go to them under their subscription's id. */
  private subscriptions: { socket: FakeSocket; worker: string; requestId: string }[] = [];
  private revisions = new Map<string, number>();

  private revisionOf(worker: string): number {
    return this.revisions.get(worker) ?? 1;
  }

  private conversationOf(worker: string): Record<string, unknown> {
    const entry = this.state.conversations?.[worker] ?? [];
    return Array.isArray(entry) ? { available: true, events: entry } : { available: true, ...entry };
  }

  private events(worker: string): Record<string, unknown>[] {
    return (this.conversationOf(worker).events as Record<string, unknown>[]) ?? [];
  }

  /**
   * A conversation's news, as the daemon sends it to its followers: `upserts` (new or changed events, by id),
   * `activity` (the running turn, null when it ended), or `snapshot: true` to send the whole conversation again.
   */
  conversation(update: { worker: string; upserts?: Record<string, unknown>[]; deletes?: string[]; activity?: Record<string, unknown> | null; snapshot?: boolean }): void {
    const { worker } = update;
    const base = this.revisionOf(worker);
    if (update.upserts?.length || update.deletes?.length) {
      const byId = new Map(this.events(worker).map((e) => [e.id, e] as const));
      for (const event of update.upserts ?? []) byId.set(event.id, event);
      for (const id of update.deletes ?? []) byId.delete(id);
      const entry = this.state.conversations?.[worker];
      const events = [...byId.values()];
      this.state.conversations = { ...this.state.conversations, [worker]: Array.isArray(entry) || !entry ? events : { ...entry, events } };
    }
    const revision = update.snapshot ? base : base + 1;
    this.revisions.set(worker, revision);
    for (const s of this.subscriptions.filter((s) => s.worker === worker)) {
      if (update.snapshot) this.to(s.socket, { type: "codex_conversation_snapshot", request_id: s.requestId, worker_id: worker, revision, conversation: this.conversationOf(worker) });
      else this.to(s.socket, {
        type: "codex_conversation_delta", request_id: s.requestId, worker_id: worker, revision, base_revision: base,
        upserts: update.upserts ?? [], deletes: update.deletes ?? [], ...(update.activity !== undefined ? { activity: update.activity } : {}),
      });
    }
  }

  /** Every connection drops and the app connects again (a network change): the daemon forgets the subscriptions. */
  drop(): void {
    for (const socket of [...this.sockets]) socket.close(1006, "network changed");
    this.subscriptions = [];
  }

  /**
   * A conversation's news, then every connection drops, in one call into the app: a host hands JS what came in
   * together, the socket's end with it (a phone back from the background gets the frames queued behind the freeze
   * and the close of a socket the computer already dropped).
   */
  newsThenDrop(update: Parameters<FakeDaemon["conversation"]>[0] & { busyMs?: number }): void {
    const held: Array<[FakeSocket, Frame]> = [];
    this.held = held;
    try {
      this.conversation(update);
      if (update.busyMs) this.broadcast({ type: "test_busy", ms: update.busyMs });
    } finally {
      this.held = null;
    }
    setTimeout(() => {
      const sockets = [...this.sockets];
      this.subscriptions = [];
      for (const [socket, frame] of held) socket.onmessage?.({ data: JSON.stringify(frame) });
      for (const socket of sockets) {
        socket.readyState = 3;
        this.closed(socket);
        socket.onclose?.({ code: 1006, reason: "network changed" });
      }
    }, 1);
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
    if (frame.type === "codex_asset" && (this.state.assetsLost ?? 0) > 0) {
      this.state.assetsLost = this.state.assetsLost! - 1;
      setTimeout(() => this.drop(), 0);
      return;
    }
    const asset = frame.type === "codex_asset" ? this.state.assets?.[String(frame.path)] : undefined;
    if (asset) return reply("codex_asset", { data_url: asset });
    // A canned answer wins over the built-in one: a test makes any request fail this way.
    const canned = this.state.replies?.[frame.type];
    if (canned) return reply(canned.type, canned);
    switch (frame.type) {
      case "brain_snapshot":
        return reply("brain_snapshot", { brain: this.state.brain ?? {} });
      case "computers":
        return reply("computers", this.state.computers ?? { computers: [], open_requests: 0 });
      case "list_triggers":
        return reply("triggers_snapshot", { triggers: this.state.triggers ?? [] });
      case "pause_trigger":
      case "resume_trigger":
      case "check_trigger": {
        const existing = (this.state.triggers ?? []).find((t) => t.id === frame.id);
        if (!existing) return fail("trigger not found");
        const now = new Date().toISOString();
        const patch: Record<string, unknown> =
          frame.type === "pause_trigger" ? { paused: true, pause_reason: undefined, state: "paused" }
          : frame.type === "resume_trigger" ? { paused: false, pause_reason: undefined, state: "watching", resumed_at: now }
          : { last_checked_at: now, failing: false, failures: 0, state: "watching" };
        const next: Record<string, unknown> = { ...existing, ...patch, revision: ++this.revision, updated_at: now };
        this.state.triggers = (this.state.triggers ?? []).map((t) => (t.id === next.id ? next : t));
        this.broadcast({ type: "trigger_changed", trigger: next });
        return reply("trigger_updated", { trigger: next });
      }
      case "delete_trigger": {
        if (!(this.state.triggers ?? []).some((t) => t.id === frame.id)) return fail("trigger not found");
        this.state.triggers = (this.state.triggers ?? []).filter((t) => t.id !== frame.id);
        this.broadcast({ type: "trigger_removed", trigger_id: frame.id });
        return reply("trigger_removed", { trigger_id: frame.id });
      }
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
        if (action === "snooze") work.snoozed_until = new Date(Date.now() + Number(frame.snooze_seconds) * 1000).toISOString();
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
        // The daemon's file watcher tells every connection, often before the writer's own answer.
        this.broadcast({ type: "work_item_changed", id: frame.id, path: frame.path, work_item: written });
        return reply("work_item_written", { work_item: written });
      }
      case "delete_work_item":
        this.state.work_items = (this.state.work_items ?? []).filter((w) => w.id !== frame.id);
        this.broadcast({ type: "work_item_deleted", id: frame.id });
        return reply("work_item_deleted_ack", { id: frame.id });
      case "live_state_get":
        return reply("live_state", { live: this.state.live ?? null });
      case "widget_token_issue":
        return reply("widget_token", { token: "widget-token", id: "w1" });
      case "codex_conversation_subscribe":
      {
        // A Brain thread of its own is `<worker>#<scope>` (`brain-host#brain-thread:t-old`); else the worker's.
        const scoped = `${String(frame.worker_id)}#${String(frame.conversation_scope_key ?? "")}`;
        const worker = this.state.conversations?.[scoped] ? scoped : String(frame.worker_id);
        this.subscriptions = [...this.subscriptions.filter((s) => s.socket !== socket || s.worker !== worker), { socket, worker, requestId: String(frame.request_id) }];
        // first_events: the newest that many first, then the older ones as the next revisions, 40 a page and the
        // newest page first (the daemon's split, codexConversationOlderPages).
        const conversation = this.conversationOf(worker);
        const events = (conversation.events as Record<string, unknown>[]) ?? [];
        const first = Number(frame.first_events ?? 0);
        let revision = this.revisionOf(worker);
        if (!(first > 0 && events.length > first)) return reply("codex_conversation_snapshot", { worker_id: frame.worker_id, revision, conversation });
        reply("codex_conversation_snapshot", { worker_id: frame.worker_id, revision, conversation: { ...conversation, events: events.slice(-first) } });
        for (let end = events.length - first; end > 0; end -= OLDER_PAGE_EVENTS) {
          reply("codex_conversation_delta", { worker_id: frame.worker_id, revision: revision + 1, base_revision: revision, upserts: events.slice(Math.max(0, end - OLDER_PAGE_EVENTS), end), deletes: [] });
          revision += 1;
        }
        this.revisions.set(worker, revision);
        return;
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
        const program = String(frame.command ?? "").trim().split(/\s+/)[0]?.split("/").pop() ?? "";
        const refusal = this.state.missing_agents?.[program];
        if (refusal) return reply("error", { code: "agent_not_installed", message: `${refusal.agent} (${program}) is not installed on this computer.`, agent: refusal.agent, program, ...(refusal.install ? { install: refusal.install } : {}) });
        const id = `w${++this.revision}`;
        const session = { id, name: String(frame.name || ""), status: "running", cwd: String(frame.cwd || "/home/me"), command: String(frame.command || ""), started_at: Date.now(), updated_at: Date.now() };
        this.state.worker_sessions = [...(this.state.worker_sessions ?? []), session];
        const holds = typeof frame.text === "string" && frame.text.trim() !== "";
        reply("session_created", { worker_id: id, worker_session: session, ...(holds ? { first_message: "pending" } : {}) });
        return this.broadcast({ type: "worker_session_created", seq: ++this.seq, worker_session: session });
      }
      case "kill_worker": {
        this.state.worker_sessions = (this.state.worker_sessions ?? []).filter((w) => w.id !== frame.worker_id);
        if (frame.request_id) reply("worker_killed", { worker_id: frame.worker_id });
        return this.broadcast({ type: "worker_session_archived", seq: ++this.seq, worker_session: { id: frame.worker_id } });
      }
      case "live_answer":
        return reply("live_answer", { kind: "brain", text: "ok", duplicate: false });
      case "send_input": {
        const admits = this.state.admitsInput;
        if (!admits) return;
        const id = String(frame.request_id);
        if (!this.admitted.has(id)) {
          this.admitted.add(id);
          const echo = { id, seq: ++this.revision, timestamp: new Date().toISOString(), kind: "user_message", role: "user", body: String(frame.text ?? ""), ...(frame.attachments ? { attachments: frame.attachments } : {}) };
          this.conversation({ worker: String(frame.worker_id), upserts: [echo] });
          if (admits.reply !== undefined) {
            const answer = { id: `${id}:reply`, seq: ++this.revision, timestamp: new Date().toISOString(), kind: "assistant_message", role: "assistant", body: admits.reply };
            this.conversation({ worker: String(frame.worker_id), upserts: [answer] });
          }
          if (admits.loseFirstAck && !this.ackLost) {
            this.ackLost = true;
            return void setTimeout(() => this.drop(), 0);
          }
        }
        return reply("input_sent", {});
      }
      case "whats_new_get":
      case "whats_new_seen":
      case "whats_new_note": {
        const record = this.state.whats_new;
        if (!record) return;
        record.seen ??= [];
        record.updates ??= [];
        if (frame.type === "whats_new_seen") {
          record.replay = false;
          for (const id of (frame.ids as string[] | undefined) ?? []) if (!record.seen.includes(id)) record.seen.push(id);
        }
        if (frame.type === "whats_new_note" && typeof frame.version === "string" && !record.updates.some((u) => u.version === frame.version)) {
          record.updates.push({ version: frame.version, at: Date.now() });
        }
        const { releases, ...rest } = record;
        return reply("whats_new", { record: { ...rest, seen: [...record.seen], updates: [...record.updates] }, ...(frame.type === "whats_new_get" && releases ? { releases } : {}) });
      }
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
