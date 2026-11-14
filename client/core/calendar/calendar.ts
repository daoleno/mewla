import type { Frame, RequestOptions } from "../transport";
import { alertPlan, remainingSnoozeMinutes, type ArmedAlert, type NativeAlertAction } from "./alerts";
import type { CalendarItem } from "./model";

/** The part of the Transport the Calendar uses (a test gives a fake). */
export interface CalendarTransport {
  readonly state: string;
  on(type: string, listener: (frame: Frame) => void): () => void;
  send(frame: Frame): void;
  request<T = Frame>(type: string, body: Record<string, unknown>, options: RequestOptions): Promise<T>;
}

/** What the host does with the phone's own alerts: Android's exact alarms, iOS's local notifications. */
export interface AlertHost {
  /** Replaces every armed alert with these; alerts that already fired stay fired. */
  schedule(alerts: ArmedAlert[]): Promise<void>;
  /** Cancels an item's armed alerts and takes its posted notification down. */
  cancelItem(itemId: string): Promise<void>;
  /** Done and Snooze taken on a notification or the lock-screen alert while no app ran, then forgotten. */
  takeActions(): Promise<NativeAlertAction[]>;
}

/** A new or edited item as the editor gives it: the daemon fills status, next_at, revision and the rest. */
export type CalendarDraft = Pick<CalendarItem, "title" | "kind" | "timezone" | "recurrence"> &
  Partial<Pick<CalendarItem, "start_at" | "end_at" | "notify_at" | "due_at" | "notes" | "summary" | "action_instruction" | "source_thread_id" | "alert_lead_minutes">>;

const TIMEOUT_MS = 15_000;

/** Alert actions taken from the host that wait for their computer: shared, so a switch does not lose them. */
const waitingActions: NativeAlertAction[] = [];

/**
 * The current computer's Calendar: its items as the daemon last sent them, the actions on
 * them, and the phone's alerts armed from them. One per computer: a switch closes it, and
 * the next computer's Calendar arms its own alerts in place of these. Every change is
 * applied from the `calendar_item_changed` broadcast that follows it; an answer to an alert
 * (Done, Snooze) also applies its reply, so the alert is closed here the moment it is answered.
 */
export class CalendarModel {
  items: CalendarItem[] = [];
  /** A snapshot arrived: until then "no items" is not "nothing planned". */
  loaded = false;
  private listeners = new Set<() => void>();
  private stops: Array<() => void> = [];
  private armTimer: ReturnType<typeof setTimeout> | null = null;
  private closed = false;
  /** Items whose Done or Snooze is on its way to the computer. */
  private answering = new Set<string>();
  /** Taking the host's alert actions: until then an open alert may already be answered on the phone. */
  private taking = 0;
  private took: boolean;

  constructor(
    readonly serverId: string,
    private readonly transport: CalendarTransport,
    private readonly alertHost: AlertHost | null = null,
    private readonly now: () => number = Date.now,
  ) {
    this.took = alertHost === null;
    this.stops.push(
      // Each connection starts with the computer's calendar_items_snapshot (asking again only sent it twice);
      // what was done on alerts while the app was closed still goes to the computer.
      transport.on("connected", () => void this.replayNativeActions()),
      transport.on("calendar_items_snapshot", (frame) => {
        const list = frame.calendar_items;
        if (!Array.isArray(list)) return;
        this.items = sortItems(list as CalendarItem[]);
        this.loaded = true;
        this.changed();
      }),
      transport.on("calendar_item_changed", (frame) => {
        const item = frame.calendar_item as CalendarItem | undefined;
        if (!item?.id) return;
        this.items = sortItems([...this.items.filter((i) => i.id !== item.id), item]);
        this.changed();
      }),
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
    if (this.armTimer) clearTimeout(this.armTimer);
    this.listeners.clear();
  }

  /**
   * Whether the app may ask about an open alert: not while the phone's own answers (Done or
   * Snooze on the notification or the lock-screen alert) are still being taken from the host.
   */
  alertsSettled(): boolean {
    return this.took && this.taking === 0;
  }

  /** The item's alert was answered on this phone and the answer has not reached the computer yet. */
  answered(id: string): boolean {
    return this.answering.has(id) || waitingActions.some((action) => action.serverId === this.serverId && action.itemId === id);
  }

  item(id: string): CalendarItem | undefined {
    return this.items.find((item) => item.id === id);
  }

  /** Asks for the list again, and replays what was done on alerts while the app was closed. */
  refresh(): void {
    try {
      this.transport.send({ type: "list_calendar_items" });
    } catch {
      // Not connected: the next connection asks.
    }
    void this.replayNativeActions();
  }

  create(draft: CalendarDraft): Promise<CalendarItem> {
    return this.ask("create_calendar_item", { calendar_item: draft }, "calendar_item_created", "Failed to create calendar item.");
  }

  update(item: CalendarItem, draft: CalendarDraft): Promise<CalendarItem> {
    const { runs: _runs, start_at: _s, end_at: _e, notify_at: _n, due_at: _d, ...rest } = item;
    return this.ask("update_calendar_item", { calendar_item: { ...rest, ...draft }, revision: item.revision }, "calendar_item_updated", "Failed to update calendar item.");
  }

  cancel(item: CalendarItem): Promise<CalendarItem> {
    return this.ask("cancel_calendar_item", { id: item.id, revision: item.revision }, "calendar_item_cancelled", "Failed to cancel calendar item.");
  }

  runNow(id: string): Promise<CalendarItem> {
    return this.ask("run_calendar_item", { id }, "calendar_item_running", "Failed to run calendar item.");
  }

  snooze(id: string, minutes: number): Promise<CalendarItem> {
    return this.answer(id, () => this.ask("snooze_calendar_item", { id, snooze_minutes: minutes }, "calendar_item_updated", "Failed to snooze calendar item."));
  }

  done(id: string): Promise<CalendarItem> {
    return this.answer(id, () => this.ask("done_calendar_item", { id }, "calendar_item_updated", "Failed to mark calendar item done."));
  }

  /** An answer to an item's alert: answered while it travels, its reply applied when it lands. */
  private async answer(id: string, send: () => Promise<CalendarItem>): Promise<CalendarItem> {
    this.answering.add(id);
    this.changed();
    try {
      const item = await send();
      this.apply(item);
      await this.alertHost?.cancelItem(id).catch(() => {});
      return item;
    } finally {
      this.answering.delete(id);
      this.changed();
    }
  }

  /** A reply's item in place of the one it answered, unless the broadcast already brought a newer one. */
  private apply(item: CalendarItem | undefined): void {
    if (!item?.id) return;
    const existing = this.item(item.id);
    if (!existing || (existing.revision ?? 0) > (item.revision ?? 0)) return;
    this.items = sortItems([...this.items.filter((i) => i.id !== item.id), item]);
  }

  /** The item with every run's full result (lists carry one line of each). */
  full(id: string): Promise<CalendarItem> {
    return this.ask("get_calendar_item", { id }, "calendar_item", "Failed to load calendar item.");
  }

  private async ask(type: string, body: Record<string, unknown>, reply: string, fallback: string): Promise<CalendarItem> {
    try {
      const frame = await this.transport.request<Frame>(type, body, { reply, timeoutMs: TIMEOUT_MS });
      return frame.calendar_item as CalendarItem;
    } catch (error) {
      const message = error instanceof Error ? error.message : "";
      throw new Error(/timed out/.test(message) ? "Calendar request timed out." : message || fallback);
    }
  }

  /**
   * Tells the computer what was done on its alerts while no app ran (Done, Snooze on the
   * notification or the lock-screen alert), so its other phones close them too. A snooze
   * replayed late says what is left of it; one that ran out already alerted again.
   */
  async replayNativeActions(): Promise<void> {
    if (!this.alertHost) return;
    this.taking++;
    let taken: NativeAlertAction[] = [];
    try {
      taken = await this.alertHost.takeActions().catch(() => [] as NativeAlertAction[]);
    } finally {
      this.taking--;
    }
    waitingActions.push(...taken);
    this.took = true;
    // Alerts gated on this can open now; before the list came there are none.
    if (this.loaded) this.changed();
    if (this.transport.state !== "connected") return;
    const mine = waitingActions.filter((a) => a.serverId === this.serverId);
    const others = waitingActions.filter((a) => a.serverId !== this.serverId);
    waitingActions.splice(0, waitingActions.length, ...others);
    // Answered until each reaches the computer, not only the one on its way.
    mine.forEach((action) => this.answering.add(action.itemId));
    for (const action of mine) {
      try {
        if (action.type === "done") await this.done(action.itemId);
        else {
          const minutes = remainingSnoozeMinutes(action, this.now());
          if (minutes >= 1) await this.snooze(action.itemId, minutes);
        }
      } catch {
        // Gone, or already answered on another phone.
      } finally {
        this.answering.delete(action.itemId);
        this.changed();
      }
    }
  }

  private changed(): void {
    if (this.closed) return;
    this.listeners.forEach((listener) => listener());
    this.armSoon();
  }

  /** Re-arms the phone's alerts from the items, once per burst of changes. */
  private armSoon(): void {
    if (!this.alertHost || this.armTimer) return;
    this.armTimer = setTimeout(() => {
      this.armTimer = null;
      if (this.closed) return;
      void this.alertHost?.schedule(alertPlan(this.serverId, this.items, this.now())).catch(() => {});
    }, 0);
  }
}

function sortItems(items: CalendarItem[]): CalendarItem[] {
  return [...items].sort((a, b) => Date.parse(a.next_at) - Date.parse(b.next_at));
}

/** What the editor needs to know about a time it is given: its instant, or why it has none. */
export function draftTimeField(kind: CalendarItem["kind"]): "start_at" | "notify_at" | "due_at" {
  return kind === "event" ? "start_at" : kind === "reminder" ? "notify_at" : "due_at";
}
