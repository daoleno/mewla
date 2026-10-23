import { describe, expect, test } from "bun:test";
import { createNotificationReporter } from "./notificationReport";

function fakeTransport(connected: string[]) {
  const sent: Array<{ serverId: string; message: any }> = [];
  const handlers = new Set<(data: { serverId: string }) => void>();
  return {
    sent,
    connect(serverId: string) {
      connected.push(serverId);
      handlers.forEach((handler) => handler({ serverId }));
    },
    isConnected: (serverId: string) => connected.includes(serverId),
    send(serverId: string, message: object) {
      if (!connected.includes(serverId)) throw new Error("not connected");
      sent.push({ serverId, message });
    },
    on: (_: "connected", handler: (data: { serverId: string }) => void) => handlers.add(handler),
    off: (_: "connected", handler: (data: { serverId: string }) => void) => handlers.delete(handler),
  };
}

describe("notification tap reports", () => {
  test("sends only the id, the action and the time when connected", () => {
    const transport = fakeTransport(["server-1"]);
    const reporter = createNotificationReporter(transport, () => 1000);
    reporter.report(
      { notification_id: "n_1", server_id: "server-1", screen: "brain", brain_thread_id: "t", worker_id: "w" },
      "tapped",
    );
    expect(transport.sent).toEqual([
      {
        serverId: "server-1",
        message: { type: "notification_response", notification_id: "n_1", action: "tapped", occurred_at_ms: 1000 },
      },
    ]);
  });

  test("a cold-start tap waits for its server to connect", () => {
    const transport = fakeTransport([]);
    let clock = 1000;
    const reporter = createNotificationReporter(transport, () => clock);
    reporter.report({ notification_id: "n_2", server_id: "server-1" }, "tapped");
    expect(transport.sent).toHaveLength(0);
    clock = 5000;
    transport.connect("server-2");
    expect(transport.sent).toHaveLength(0);
    transport.connect("server-1");
    expect(transport.sent).toHaveLength(1);
    // The time is when the person tapped, not when the socket came up.
    expect(transport.sent[0].message.occurred_at_ms).toBe(1000);
  });

  test("ignores pushes with no id (local Calendar reminders) or no server", () => {
    const transport = fakeTransport(["server-1"]);
    const reporter = createNotificationReporter(transport);
    reporter.report({ screen: "calendar", server_id: "server-1" }, "tapped");
    reporter.report({ notification_id: "n_3" }, "tapped");
    reporter.report(null, "tapped");
    expect(transport.sent).toHaveLength(0);
  });

  test("drops reports older than a day", () => {
    const transport = fakeTransport([]);
    let clock = 0;
    const reporter = createNotificationReporter(transport, () => clock);
    reporter.report({ notification_id: "n_4", server_id: "server-1" }, "tapped");
    clock = 25 * 60 * 60 * 1000;
    transport.connect("server-1");
    expect(transport.sent).toHaveLength(0);
  });
});
