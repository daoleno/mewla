import type { Frame, ListenOptions, RequestOptions } from "../transport";
import { sortTriggers, type Trigger } from "./model";

/** The part of the Transport Triggers use (a test gives a fake). */
export interface TriggersTransport {
  readonly state: string;
  on(type: string, listener: (frame: Frame) => void, options?: ListenOptions): () => void;
  send(frame: Frame): void;
  request<T = Frame>(type: string, body: Record<string, unknown>, options: RequestOptions): Promise<T>;
}

const TIMEOUT_MS = 15_000;
/** A check runs up to its timeout (at most ten minutes) before the computer answers. */
const CHECK_TIMEOUT_MS = 11 * 60_000;

/**
 * The current computer's Triggers as it last sent them, and what the person can do with one: pause, resume,
 * check now, delete. One per computer: a switch closes it. Every change also arrives as `trigger_changed` /
 * `trigger_removed`, so a reply only applies what the broadcast may not have brought yet.
 */
export class TriggersModel {
  triggers: Trigger[] = [];
  loaded = false;
  private listeners = new Set<() => void>();
  private stops: Array<() => void> = [];
  private closed = false;

  constructor(readonly serverId: string, private readonly transport: TriggersTransport) {
    this.stops.push(
      transport.on("triggers_snapshot", (frame) => {
        const list = frame.triggers;
        this.triggers = sortTriggers(Array.isArray(list) ? (list as Trigger[]) : []);
        this.loaded = true;
        this.changed();
      }),
      transport.on("trigger_changed", (frame) => this.apply(frame.trigger as Trigger | undefined)),
      transport.on("trigger_removed", (frame) => this.drop(String(frame.trigger_id ?? ""))),
    );
    if (transport.state === "connected") this.refresh();
  }

  onChange(listener: () => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  close(): void {
    this.closed = true;
    this.stops.splice(0).forEach((stop) => stop());
    this.listeners.clear();
  }

  trigger(id: string): Trigger | undefined {
    return this.triggers.find((trigger) => trigger.id === id);
  }

  refresh(): void {
    try {
      this.transport.send({ type: "list_triggers" });
    } catch {
      // Not connected: the next connection sends the snapshot.
    }
  }

  pause(id: string): Promise<Trigger> {
    return this.ask("pause_trigger", id, "Failed to pause the Trigger.");
  }

  resume(id: string): Promise<Trigger> {
    return this.ask("resume_trigger", id, "Failed to resume the Trigger.");
  }

  checkNow(id: string): Promise<Trigger> {
    return this.ask("check_trigger", id, "The check did not run.", CHECK_TIMEOUT_MS);
  }

  /** Deletes it on the computer, then here (the broadcast may already have taken it). */
  async delete(id: string): Promise<void> {
    try {
      await this.transport.request<Frame>("delete_trigger", { id }, { reply: "trigger_removed", timeoutMs: TIMEOUT_MS });
    } catch (error) {
      throw new Error(messageOf(error, "Failed to delete the Trigger."));
    }
    this.drop(id);
  }

  private async ask(type: string, id: string, fallback: string, timeoutMs = TIMEOUT_MS): Promise<Trigger> {
    try {
      const frame = await this.transport.request<Frame>(type, { id }, { reply: "trigger_updated", timeoutMs });
      const trigger = frame.trigger as Trigger;
      this.apply(trigger);
      return trigger;
    } catch (error) {
      throw new Error(messageOf(error, fallback));
    }
  }

  /** A newer Trigger in place of the one listed; an older reply never replaces a newer broadcast. */
  private apply(trigger: Trigger | undefined): void {
    if (!trigger?.id) return;
    const existing = this.trigger(trigger.id);
    if (existing && Date.parse(existing.updated_at) > Date.parse(trigger.updated_at)) return;
    this.triggers = sortTriggers([...this.triggers.filter((t) => t.id !== trigger.id), trigger]);
    this.changed();
  }

  private drop(id: string): void {
    if (!id || !this.trigger(id)) return;
    this.triggers = this.triggers.filter((trigger) => trigger.id !== id);
    this.changed();
  }

  private changed(): void {
    if (this.closed) return;
    this.listeners.forEach((listener) => listener());
  }
}

function messageOf(error: unknown, fallback: string): string {
  const message = error instanceof Error ? error.message : "";
  return /timed out/.test(message) ? "The computer did not answer in time." : message || fallback;
}
