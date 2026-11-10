import type { Frame, Transport } from "../transport";
import { normalizeCodexConversation } from "./conversation";
import type {
  CodexConversationDeltaPayload,
  CodexConversationSnapshotPayload,
  CodexConversationSyncStatusPayload,
} from "./thread";

/** What to follow: a Session (worker) or Brain's host, in one conversation scope (`brain-thread:<id>`). */
export interface ConversationTarget {
  targetId: string;
  workerId?: string;
  cwd?: string;
  command?: string;
  name?: string;
  startedAt?: number;
  processId?: number;
  conversationScopeKey?: string;
}

export interface ConversationHandlers {
  onSnapshot(payload: CodexConversationSnapshotPayload): void;
  onDelta(payload: CodexConversationDeltaPayload): void;
  onSyncStatus(payload: CodexConversationSyncStatusPayload): void;
  onError(error: Error): void;
}

let nextId = 0;

/**
 * Follows one conversation on the current connection: `codex_conversation_subscribe`,
 * then its snapshot and revisions, matched by the subscription's own request id.
 * Call it once the transport is connected and again after every reconnect (the
 * daemon forgets subscriptions with the socket). Never throws: on a connection
 * that is not open it does nothing and returns a no-op. The returned function
 * unsubscribes.
 */
export function subscribeConversation(transport: Transport, target: ConversationTarget, handlers: ConversationHandlers): () => void {
  if (transport.state !== "connected") return () => {};
  const requestId = `sub${Date.now().toString(36)}${(nextId++).toString(36)}`;
  const mine = (frame: Frame) => frame.request_id === requestId;
  const stops = [
    transport.on("codex_conversation_snapshot", (frame) => mine(frame) && handlers.onSnapshot(snapshotPayload(frame))),
    transport.on("codex_conversation_delta", (frame) => mine(frame) && handlers.onDelta(deltaPayload(frame))),
    transport.on("codex_conversation_sync_status", (frame) => mine(frame) && handlers.onSyncStatus(syncStatusPayload(frame))),
    transport.on("error", (frame) => mine(frame) && handlers.onError(new Error(String(frame.message ?? "")))),
  ];
  const stop = () => stops.splice(0).forEach((off) => off());
  try {
    transport.send({
      type: "codex_conversation_subscribe",
      request_id: requestId,
      target_id: target.targetId,
      worker_id: target.workerId,
      cwd: target.cwd,
      command: target.command,
      name: target.name,
      started_at: target.startedAt,
      process_id: target.processId,
      conversation_scope_key: target.conversationScopeKey,
    });
  } catch {
    stop();
    return () => {};
  }
  return () => {
    if (stops.length === 0) return;
    stop();
    try {
      transport.send({ type: "codex_conversation_unsubscribe", request_id: requestId, target_id: target.targetId, worker_id: target.workerId });
    } catch {
      // The socket is gone, and the daemon dropped the subscription with it.
    }
  };
}

const text = (value: unknown) => (typeof value === "string" ? value : undefined);
const count = (value: unknown) => (typeof value === "number" && Number.isFinite(value) ? value : 0);

export function snapshotPayload(payload: Frame): CodexConversationSnapshotPayload {
  return {
    request_id: text(payload.request_id),
    worker_id: text(payload.worker_id),
    conversation_id: text(payload.conversation_id),
    revision: count(payload.revision),
    generation: text(payload.generation),
    conversation: normalizeCodexConversation(payload.conversation),
  };
}

export function deltaPayload(payload: Frame): CodexConversationDeltaPayload {
  const delta = normalizeCodexConversation({ available: true, updated_at: payload.updated_at, activity: payload.activity, events: payload.upserts });
  return {
    request_id: text(payload.request_id),
    worker_id: text(payload.worker_id),
    conversation_id: text(payload.conversation_id),
    revision: count(payload.revision),
    base_revision: count(payload.base_revision),
    generation: text(payload.generation),
    available: typeof payload.available === "boolean" ? payload.available : undefined,
    reason: text(payload.reason),
    source: text(payload.source),
    path: text(payload.path),
    session_id: text(payload.session_id),
    cwd: text(payload.cwd),
    updated_at: text(payload.updated_at),
    // Present only when it changed; null when it was cleared.
    activity: Object.prototype.hasOwnProperty.call(payload, "activity") ? (delta.activity ?? null) : undefined,
    upserts: delta.events,
    deletes: Array.isArray(payload.deletes) ? payload.deletes.filter((id): id is string => typeof id === "string") : [],
  };
}

export function syncStatusPayload(payload: Frame): CodexConversationSyncStatusPayload {
  return {
    request_id: text(payload.request_id),
    worker_id: text(payload.worker_id),
    conversation_id: text(payload.conversation_id),
    revision: count(payload.revision),
    generation: text(payload.generation),
    state: text(payload.state) ?? "syncing",
    reason: text(payload.reason),
  };
}
