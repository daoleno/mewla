/**
 * Tells the computer what the person did with its push, so Watch can judge whether the
 * push was worth sending. Only the notification's id, the action and when it happened
 * leave the phone. A tap that starts the app arrives before the connection does, so a
 * report waits for its computer's next connection (a day at most, twenty per computer).
 */
const MAX_PENDING = 20;
const MAX_AGE_MS = 24 * 60 * 60 * 1000;

export interface NotificationReport {
  type: "notification_response";
  notification_id: string;
  action: "tapped";
  occurred_at_ms: number;
}

export class NotificationReporter {
  private pending = new Map<string, NotificationReport[]>();

  constructor(private readonly now: () => number = Date.now) {}

  /** Notes a tap on the push whose data is `data`; pushes without an id or computer are not reported. */
  tapped(data: unknown): string | null {
    if (!data || typeof data !== "object") return null;
    const fields = data as Record<string, unknown>;
    const id = typeof fields.notification_id === "string" ? fields.notification_id : "";
    const server = typeof fields.server_id === "string" ? fields.server_id : "";
    if (!id || !server) return null;
    const queue = this.pending.get(server) ?? [];
    queue.push({ type: "notification_response", notification_id: id, action: "tapped", occurred_at_ms: this.now() });
    this.pending.set(server, queue.slice(-MAX_PENDING));
    return server;
  }

  /** Sends what waits for `serverId`; what `send` refuses (the connection dropped) waits for the next one. */
  flush(serverId: string, send: (report: NotificationReport) => void): void {
    const queue = this.pending.get(serverId);
    if (!queue) return;
    this.pending.delete(serverId);
    for (let i = 0; i < queue.length; i++) {
      if (this.now() - queue[i].occurred_at_ms > MAX_AGE_MS) continue;
      try {
        send(queue[i]);
      } catch {
        this.pending.set(serverId, queue.slice(i));
        return;
      }
    }
  }
}
