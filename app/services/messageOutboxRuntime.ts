import AsyncStorage from "@react-native-async-storage/async-storage";
import {
  MessageOutbox,
  type OutboxEntry,
  type OutboxStore,
  type OutboxTransport,
} from "./messageOutbox";
import { wsClient } from "./websocket";

const STORAGE_KEY = "mewla:v1:send_outbox";

const asyncStorageOutbox: OutboxStore = {
  async load() {
    try {
      const raw = await AsyncStorage.getItem(STORAGE_KEY);
      const parsed = raw ? (JSON.parse(raw) as unknown) : [];
      return Array.isArray(parsed) ? parsed.filter(isOutboxEntry) : [];
    } catch {
      return [];
    }
  },
  async save(entries) {
    try {
      if (entries.length === 0) {
        await AsyncStorage.removeItem(STORAGE_KEY);
        return;
      }
      await AsyncStorage.setItem(STORAGE_KEY, JSON.stringify(entries));
    } catch {
      // The queue still works in memory; only surviving a restart is lost.
    }
  },
};

const wsTransport: OutboxTransport = {
  isConnected: (serverId) => wsClient.isConnected(serverId),
  sendInput: (serverId, workerId, text, options) =>
    wsClient.sendInput(serverId, workerId, text, options),
  dropConnection: (serverId, reason) => wsClient.dropConnection(serverId, reason),
};

/** The app's one send queue, driven by the live connection. */
export const messageOutbox = new MessageOutbox(
  wsTransport,
  asyncStorageOutbox,
  {
    now: () => Date.now(),
    setTimer: (run, ms) => setTimeout(run, ms),
    clearTimer: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>),
  },
);

wsClient.on("connected", (payload: { serverId?: string }) => {
  if (payload.serverId) messageOutbox.onConnected(payload.serverId);
});
wsClient.on("disconnected", (payload: { serverId?: string }) => {
  if (payload.serverId) messageOutbox.onDisconnected(payload.serverId);
});
void messageOutbox.hydrate();

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
    (entry.state === "queued" ||
      entry.state === "uncertain" ||
      entry.state === "undeliverable")
  );
}
