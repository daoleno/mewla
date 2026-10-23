// Tells the daemon what the person did with a push, so Watch can judge whether
// the push was worth sending. Only the notification's id, the action and when
// it happened leave the app.

const MAX_PENDING = 20;
const MAX_AGE_MS = 24 * 60 * 60 * 1000;

export interface NotificationReportTransport {
  isConnected(serverId: string): boolean;
  send(serverId: string, message: object): void;
  on(type: "connected", handler: (data: { serverId: string }) => void): void;
  off(type: "connected", handler: (data: { serverId: string }) => void): void;
}

interface PendingReport {
  message: { type: "notification_response"; notification_id: string; action: string; occurred_at_ms: number };
}

function text(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

/**
 * Reports taps. A tap that opens the app from cold arrives before the socket
 * does, so reports wait for their server's next connection.
 */
export function createNotificationReporter(
  transport: NotificationReportTransport,
  now: () => number = Date.now,
) {
  const pending = new Map<string, PendingReport[]>();

  const flush = (serverId: string) => {
    const queue = pending.get(serverId);
    if (!queue) return;
    pending.delete(serverId);
    for (const report of queue) {
      if (now() - report.message.occurred_at_ms > MAX_AGE_MS) continue;
      try {
        transport.send(serverId, report.message);
      } catch {
        // The connection dropped again; the next connection retries the rest.
        const rest = queue.slice(queue.indexOf(report));
        pending.set(serverId, rest);
        return;
      }
    }
  };

  const onConnected = (data: { serverId: string }) => flush(data.serverId);
  transport.on("connected", onConnected);

  return {
    /** Reports `action` on the push whose payload is `data`; pushes without an id are ignored. */
    report(data: unknown, action: "tapped") {
      if (!data || typeof data !== "object") return;
      const fields = data as Record<string, unknown>;
      const notificationId = text(fields.notification_id);
      const serverId = text(fields.server_id);
      if (!notificationId || !serverId) return;
      const queue = pending.get(serverId) ?? [];
      queue.push({
        message: {
          type: "notification_response",
          notification_id: notificationId,
          action,
          occurred_at_ms: now(),
        },
      });
      pending.set(serverId, queue.slice(-MAX_PENDING));
      if (transport.isConnected(serverId)) flush(serverId);
    },
    dispose() {
      transport.off("connected", onConnected);
    },
  };
}
