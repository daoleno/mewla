import type { CalendarTransport } from "../calendar/calendar";
import type { Frame } from "../transport";

/**
 * The Markdown Work files of the current computer (`~/.mewla/work`), as far as the daemon
 * lists them: the Work a Calendar run wrote, while it is unfinished, linked from its item
 * or tied to a live Session. Editable: the body saves against the version it was loaded
 * from, so an edit never overwrites a change made on the computer meanwhile.
 */
export type Frontmatter = {
  id: string;
  kind?: string;
  created: string;
  done?: string | null;
  started?: string | null;
  status?: string;
  title?: string;
  worker_session?: string;
  extra?: Record<string, unknown>;
  [key: string]: unknown;
};

export type WorkItem = {
  id: string;
  path: string;
  project: string;
  title: string;
  body: string;
  frontmatter: Frontmatter;
  /** The file's version: a save says which one it edited. */
  mtime: string;
};

export type WorkItemStatus = "queued" | "running" | "blocked" | "done" | "failed" | "removed" | "unknown";

export const workItemStatusLabel: Record<WorkItemStatus, string> = {
  queued: "Queued", running: "Running", blocked: "Blocked", done: "Done", failed: "Failed", removed: "Removed", unknown: "Unknown",
};

export function workItemStatus(item: Pick<WorkItem, "frontmatter">): WorkItemStatus {
  const explicit = item.frontmatter.status;
  if (explicit === "failed" || explicit === "blocked" || explicit === "done" || explicit === "removed" || explicit === "running" || explicit === "unknown") return explicit;
  if (item.frontmatter.done) return "done";
  if (item.frontmatter.started) return "running";
  return "queued";
}

function timestamp(value: unknown): string {
  if (typeof value === "string" && value.trim()) {
    const parsed = Date.parse(value);
    return Number.isNaN(parsed) ? value : new Date(parsed).toISOString();
  }
  if (typeof value === "number" && Number.isFinite(value)) return new Date(value > 10_000_000_000 ? value : value * 1000).toISOString();
  return new Date(0).toISOString();
}

export function normalizeWorkItem(raw: Record<string, unknown>): WorkItem {
  const id = String(raw.id || "");
  const front = (raw.frontmatter && typeof raw.frontmatter === "object" ? raw.frontmatter : {}) as Partial<Frontmatter>;
  return {
    id,
    path: String(raw.path || ""),
    project: String(raw.project || "inbox"),
    title: String(raw.title || front.title || ""),
    body: String(raw.body || ""),
    frontmatter: { ...front, id: String(front.id || id), created: typeof front.created === "string" ? front.created : new Date(0).toISOString() },
    mtime: timestamp(raw.mtime),
  };
}

/** The body as it reads: without its leading title and the daemon's Calendar metadata block. */
export function readableBody(body: string): string {
  return body.replace(/^\s*# [^\n]*\n+/, "").replace(/\n## Calendar metadata\n[\s\S]*$/, "").trim();
}

export class WorkItemConflict extends Error {
  constructor(readonly current: WorkItem | null) {
    super("This Work changed on your computer.");
  }
}

export class WorkItems {
  items = new Map<string, WorkItem>();
  loaded = false;
  private listeners = new Set<() => void>();
  private stops: Array<() => void>;

  constructor(private readonly transport: CalendarTransport) {
    const take = (raw: unknown) => {
      if (!raw || typeof raw !== "object") return;
      const item = normalizeWorkItem(raw as Record<string, unknown>);
      if (item.id) this.items.set(item.id, item);
    };
    this.stops = [
      transport.on("connected", () => this.refresh()),
      transport.on("work_items_snapshot", (frame) => {
        if (!Array.isArray(frame.work_items)) return;
        this.items = new Map();
        frame.work_items.forEach(take);
        this.loaded = true;
        this.changed();
      }),
      transport.on("work_item_changed", (frame) => {
        take(frame.work_item);
        this.changed();
      }),
      transport.on("work_item_deleted", (frame) => {
        const id = typeof frame.id === "string" ? frame.id : [...this.items.values()].find((i) => i.path === frame.path)?.id;
        if (id && this.items.delete(id)) this.changed();
      }),
    ];
    if (transport.state === "connected") this.refresh();
  }

  onChange(listener: () => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  close(): void {
    this.stops.forEach((stop) => stop());
    this.listeners.clear();
  }

  refresh(): void {
    try {
      this.transport.send({ type: "list_work_items" });
    } catch {
      // The next connection asks.
    }
  }

  /** The Work a Session was started for (a Calendar run's), if any. */
  linkedTo(sessionId: string): WorkItem | undefined {
    return [...this.items.values()].find((item) => item.frontmatter.worker_session === sessionId);
  }

  /** Saves the body (and front matter) over the version `base` was; a newer version on the computer is a conflict. */
  async save(base: WorkItem, changes: { body?: string; frontmatter?: Frontmatter }): Promise<WorkItem> {
    try {
      const frame = await this.transport.request<Frame>("write_work_item", {
        id: base.id, project: base.project, path: base.path,
        body: changes.body ?? base.body, frontmatter: changes.frontmatter ?? base.frontmatter, base_mtime: base.mtime,
      }, { reply: "work_item_written", errors: ["error"], timeoutMs: 15_000 });
      const saved = normalizeWorkItem(frame.work_item as Record<string, unknown>);
      this.items.set(saved.id, saved);
      this.changed();
      return saved;
    } catch (error) {
      if (/changed on disk|conflict/i.test(error instanceof Error ? error.message : "")) throw new WorkItemConflict(null);
      throw error;
    }
  }

  /** Done marks it done now; Reopen clears it. */
  setDone(item: WorkItem, done: boolean, now = new Date()): Promise<WorkItem> {
    const { status: _status, ...rest } = item.frontmatter;
    return this.save(item, { frontmatter: { ...rest, done: done ? now.toISOString() : null } });
  }

  async remove(item: WorkItem): Promise<void> {
    await this.transport.request("delete_work_item", { id: item.id }, { reply: "work_item_deleted_ack", timeoutMs: 15_000 });
    this.items.delete(item.id);
    this.changed();
  }

  private changed(): void {
    this.listeners.forEach((listener) => listener());
  }
}
