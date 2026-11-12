import type { Transport } from "../transport";
import { chatCacheKey } from "./draft";
import type { MessageOutbox, OutboxEntry, OutboxEvent } from "./outbox";
import { UNCERTAIN_SEND_ERROR, UNDELIVERED_SEND_ERROR } from "./pendingUserMessages";
import { subscribeConversation, type ConversationTarget } from "./subscription";
import {
  initialInterfaceChatThreadState,
  interfaceChatThreadReducer,
  interfaceConversationStreamErrorMessage,
  rememberInterfaceConversation,
  type InterfaceChatThreadAction,
  type InterfaceChatThreadState,
  type ComposerAttachment,
  type PendingUserMessageInput,
} from "./thread";
import type { SendAttachment } from "./send";

export { chatCacheKey };

/** What the daemon is told about each file: where it keeps it (the phone's own URI stays here). */
function sendAttachments(files: readonly ComposerAttachment[]): SendAttachment[] {
  return files.map((file) => ({ name: file.name, path: file.path, ...(file.mimeType ? { content_type: file.mimeType } : {}) }));
}

export interface ChatOptions {
  serverId: string;
  transport: Transport;
  target: ConversationTarget;
  outbox: MessageOutbox;
}

/**
 * One open chat (Brain's thread or a Session): the transcript the daemon streams,
 * and the person's messages it has not echoed yet. It follows the
 * conversation whenever the transport is connected and again after every
 * reconnect or stream gap; sends go only through the outbox, under ids the
 * daemon dedupes on. Nothing here throws into the caller. `close()` when the
 * computer, the thread or the screen goes away.
 */
export class Chat {
  readonly cacheKey: string;
  private state: InterfaceChatThreadState;
  private readonly listeners = new Set<(state: InterfaceChatThreadState) => void>();
  private readonly rowByRequest = new Map<string, string>();
  private readonly stops: Array<() => void> = [];
  private unsubscribe: (() => void) | null = null;
  private generation = 0;
  private resyncToken = 0;
  private closed = false;

  constructor(private readonly options: ChatOptions) {
    const { serverId, target, transport, outbox } = options;
    this.cacheKey = chatCacheKey(serverId, target.targetId, target.conversationScopeKey);
    this.state = initialInterfaceChatThreadState(this.cacheKey);
    this.stops.push(transport.on("connected", () => this.follow()));
    this.stops.push(transport.on("disconnected", () => this.stopFollowing()));
    this.stops.push(outbox.subscribe((event) => this.onOutbox(event)));
    this.follow();
    this.showQueued();
    void outbox.hydrate().then(() => this.showQueued());
  }

  get current(): InterfaceChatThreadState {
    return this.state;
  }

  onChange(listener: (state: InterfaceChatThreadState) => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  /** Queues the message: the row shows at once, delivery (now or after a reconnect) is the outbox's. */
  send(text: string, attachments: ComposerAttachment[] = []): void {
    const body = text.trim();
    if ((!body && attachments.length === 0) || this.closed) return;
    const { outbox, serverId, target } = this.options;
    const requestId = outbox.mintId();
    const rowId = this.addPending({ body, sentText: text, attachments, lifecycle: "pending", dispatchRequestId: requestId });
    this.rowByRequest.set(requestId, rowId);
    outbox.enqueue({ id: requestId, serverId, workerId: target.targetId, conversationScopeKey: target.conversationScopeKey, text, attachments: sendAttachments(attachments) });
  }

  /** "Send again" or "Delete" on a message that could not be delivered. */
  actOnFailed(rowId: string, action: "retry" | "discard"): void {
    const message = this.state.pendingUserMessages.find((m) => m.id === rowId && m.lifecycle === "failed");
    if (!message) return;
    const { outbox, serverId, target } = this.options;
    if (action === "discard") {
      outbox.discard(message.dispatchRequestId);
      this.rowByRequest.delete(message.dispatchRequestId);
      this.dispatch({ type: "discard_pending_user_message", id: message.id });
      return;
    }
    if (outbox.resend(message.dispatchRequestId)) return;
    const requestId = outbox.mintId();
    this.rowByRequest.set(requestId, message.id);
    this.dispatch({ type: "begin_pending_user_message_attempt", id: message.id, requestId });
    outbox.enqueue({ id: requestId, serverId, workerId: target.targetId, conversationScopeKey: target.conversationScopeKey, text: message.sentText, attachments: sendAttachments(message.attachments) });
  }

  /** Reads the conversation again ("Try again" on a chat that did not load); a no-op while disconnected. */
  reload(): void {
    this.follow();
  }

  close(): void {
    if (this.closed) return;
    this.closed = true;
    this.stopFollowing();
    this.stops.splice(0).forEach((stop) => stop());
    this.listeners.clear();
  }

  private follow(): void {
    if (this.closed || this.options.transport.state !== "connected") return;
    this.stopFollowing();
    const generation = ++this.generation;
    this.dispatch({ type: "stream_start", generation });
    this.unsubscribe = subscribeConversation(this.options.transport, this.options.target, {
      onSnapshot: (payload) => this.dispatch({ type: "snapshot", payload, generation }),
      onDelta: (delta) => this.dispatch({ type: "delta", delta, generation }),
      onSyncStatus: (status) => this.dispatch({ type: "sync_status", status, generation }),
      onError: (error) => this.dispatch({ type: "stream_error", error: interfaceConversationStreamErrorMessage(error), generation }),
    });
  }

  private stopFollowing(): void {
    this.unsubscribe?.();
    this.unsubscribe = null;
  }

  private dispatch(action: InterfaceChatThreadAction): void {
    if (this.closed) return;
    const next = interfaceChatThreadReducer(this.state, action);
    if (next === this.state) return;
    const previous = this.state;
    this.state = next;
    if (next.conversation !== previous.conversation) rememberInterfaceConversation(this.cacheKey, next.conversation);
    this.listeners.forEach((listener) => listener(next));
    // A revision gap: the reducer asks for the stream again from a fresh snapshot.
    if (next.resyncToken !== this.resyncToken) {
      this.resyncToken = next.resyncToken;
      this.follow();
    }
  }

  private addPending(message: PendingUserMessageInput): string {
    const id = `pending:${Date.now().toString(36)}:${Math.random().toString(36).slice(2, 8)}`;
    this.dispatch({ type: "add_pending_user_message", message: { ...message, id, createdAt: new Date().toISOString() } });
    return id;
  }

  private mine(entry: OutboxEntry): boolean {
    const { serverId, target } = this.options;
    return entry.serverId === serverId && entry.workerId === target.targetId && (entry.conversationScopeKey ?? "") === (target.conversationScopeKey ?? "");
  }

  private rowFor(requestId: string): string | undefined {
    return this.rowByRequest.get(requestId) ?? this.state.pendingUserMessages.find((m) => m.dispatchRequestId === requestId)?.id;
  }

  /** Messages queued before this chat opened (or before the app restarted, or by the Expo app) get their row back. */
  private showQueued(): void {
    const { outbox, serverId, target } = this.options;
    for (const entry of outbox.entriesFor(serverId, target.targetId, target.conversationScopeKey)) {
      if (this.rowFor(entry.id)) continue;
      const failed = entry.state === "undeliverable";
      const rowId = this.addPending({
        body: entry.text.trim(),
        sentText: entry.text.trim(),
        attachments: (entry.attachments ?? []).map((file) => ({ name: file.name, path: file.path, mimeType: file.content_type })),
        lifecycle: failed ? "failed" : "pending",
        dispatchRequestId: entry.id,
        ...(failed ? { failureCode: entry.failureCode, failureMessage: UNDELIVERED_SEND_ERROR } : {}),
      });
      this.rowByRequest.set(entry.id, rowId);
    }
  }

  private onOutbox(event: OutboxEvent): void {
    if (event.kind === "changed") {
      if (event.serverId === this.options.serverId) this.showQueued();
      return;
    }
    const entry = event.entry;
    if (!this.mine(entry)) return;
    if (event.kind === "reidentified") {
      const rowId = this.rowFor(event.previousId);
      if (!rowId) return;
      this.rowByRequest.delete(event.previousId);
      this.rowByRequest.set(entry.id, rowId);
      this.dispatch({ type: "begin_pending_user_message_attempt", id: rowId, requestId: entry.id });
      return;
    }
    const rowId = this.rowFor(entry.id);
    if (!rowId) return;
    if (event.kind === "accepted") {
      this.dispatch({ type: "settle_pending_user_message", id: rowId, settlement: { kind: "acknowledge", requestId: entry.id } });
    } else if (event.kind === "uncertain") {
      this.dispatch({
        type: "settle_pending_user_message",
        id: rowId,
        settlement: { kind: "uncertain", requestId: entry.id, code: "input_uncertain", message: UNCERTAIN_SEND_ERROR },
      });
    } else if (event.kind === "undeliverable") {
      this.dispatch({
        type: "settle_pending_user_message",
        id: rowId,
        settlement: { kind: "reject", requestId: entry.id, code: entry.failureCode ?? "undeliverable", message: UNDELIVERED_SEND_ERROR },
      });
    }
  }
}
