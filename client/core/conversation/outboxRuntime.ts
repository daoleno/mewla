import type { KeyValueStore } from "../platform";
import type { Transport } from "../transport";
import { MessageOutbox, type OutboxEntry, type OutboxStore } from "./outbox";
import { sendInput } from "./send";

/** The Expo app's key: messages it queued come over with the upgrade and are sent. */
export const OUTBOX_STORAGE_KEY = "mewla:v1:send_outbox";

/**
 * The app's one send queue, kept in storage and driven by the current computer's
 * connection: `transportFor` answers the live Transport of a server, or null.
 * The session calls `onConnected` / `onDisconnected` as that connection changes.
 */
export function createOutbox(storage: KeyValueStore, transportFor: (serverId: string) => Transport | null): MessageOutbox {
  const store: OutboxStore = {
    async load() {
      try {
        const raw = await storage.getItem(OUTBOX_STORAGE_KEY);
        const parsed = raw ? (JSON.parse(raw) as unknown) : [];
        return Array.isArray(parsed) ? parsed.filter(isOutboxEntry) : [];
      } catch {
        return [];
      }
    },
    async save(entries) {
      try {
        if (entries.length === 0) await storage.removeItem(OUTBOX_STORAGE_KEY);
        else await storage.setItem(OUTBOX_STORAGE_KEY, JSON.stringify(entries));
      } catch {
        // The queue still works in memory; only surviving a restart is lost.
      }
    },
  };
  return new MessageOutbox(
    {
      isConnected: (serverId) => transportFor(serverId)?.state === "connected",
      sendInput: (serverId, workerId, text, options) => {
        const transport = transportFor(serverId);
        if (!transport) return { requestId: options.requestId, outcome: Promise.resolve({ kind: "connection_closed" as const }) };
        return sendInput(transport, { requestId: options.requestId, workerId, text, attachments: options.attachments, conversationScopeKey: options.conversationScopeKey });
      },
      dropConnection: (serverId, reason) => transportFor(serverId)?.drop(reason),
    },
    store,
    {
      now: () => Date.now(),
      setTimer: (run, ms) => setTimeout(run, ms),
      clearTimer: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>),
    },
  );
}

function isOutboxEntry(value: unknown): value is OutboxEntry {
  if (!value || typeof value !== "object") return false;
  const entry = value as Record<string, unknown>;
  return (
    typeof entry.id === "string" &&
    typeof entry.serverId === "string" &&
    typeof entry.workerId === "string" &&
    typeof entry.text === "string" &&
    typeof entry.createdAt === "number" &&
    typeof entry.attempts === "number" &&
    (entry.state === "queued" || entry.state === "uncertain" || entry.state === "undeliverable")
  );
}
