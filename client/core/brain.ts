import type { Transport } from "./transport";
import { isConversationSyncingReason } from "./session/timelinePhase";

/** A Session as Brain lists it; also Brain's own hidden host (`host_worker`). */
export type BrainWorkerRef = {
  id: string;
  name: string;
  status: string;
  summary?: string;
  phase?: string;
  cwd?: string;
  command?: string;
  /** Adapter family of the executor, as the daemon names it. */
  provider?: string;
  started_at?: number;
  process_id?: number;
  updated_at?: string;
  delegated?: boolean;
  /** Daemon-authoritative capabilities; never inferred from the name or command. */
  capabilities?: Record<string, unknown>;
  /** The blocking prompt open in the host's terminal, when it is blocked on one. */
  prompt?: unknown;
};

export type BrainExecutorRef = {
  id: string;
  name: string;
  provider?: string;
  command?: string;
  runtime?: string;
  capabilities?: { interactive_tty?: boolean; structured_events?: boolean };
  host?: boolean;
};

export type BrainExecutorState = { installed: boolean; signed_in: boolean; account?: string };

/** Why Brain has no running host, said by the computer (daemon brain.HostProblem). */
export type BrainHostProblem = {
  /** not_installed, not_signed_in, not_started or not_ready; a newer computer may say more. */
  code: string;
  executor: string;
  detail: string;
  suggest_executor?: string;
  installed_executors: string[];
};

/**
 * `brain_snapshot.brain`: Brain's state, not its conversation. The conversation
 * is the host's, followed with `codex_conversation_subscribe` in the scope of
 * `chat_thread_id`. Work (current_work, open_work, …) is read by the Work screens.
 */
export type BrainSnapshot = {
  workers?: BrainWorkerRef[];
  host_worker?: BrainWorkerRef | null;
  host_executor?: BrainExecutorRef | null;
  host_problem?: BrainHostProblem;
  executors?: BrainExecutorRef[];
  executor_states?: Record<string, BrainExecutorState>;
  chat_thread_id?: string;
  current_work?: unknown[];
  open_work?: unknown[];
  workspace?: string;
  generated_at?: string;
};

/** The conversation scope of a Brain chat thread; drafts and the subscription use it. */
export function brainThreadScopeKey(threadId: string): string {
  return `brain-thread:${threadId}`;
}

/** Asks for Brain's state; the answer arrives as a `brain_snapshot` like every broadcast. Never throws. */
export function requestBrainSnapshot(transport: Transport): void {
  try {
    transport.send({ type: "brain_snapshot" });
  } catch {
    // Not connected: the daemon sends one on the next connection.
  }
}

/** Starts a new Brain chat; settles with the snapshot that carries the new thread. */
export function newBrainChat(transport: Pick<Transport, "request">): Promise<BrainSnapshot> {
  return transport
    .request<{ brain?: BrainSnapshot }>("brain_chat_new", {}, { reply: "brain_snapshot" })
    .then((frame) => frame.brain ?? {});
}

/**
 * What Brain's screen shows. `waking` until the computer reports a running host
 * (the composer keeps the draft meanwhile); `host_down` when the computer says
 * why there is none, which waiting will not fix.
 */
export type BrainSurface = "no_computer" | "offline" | "waking" | "host_down" | "chat";

export function brainSurface(input: { hasServer: boolean; connected: boolean; snapshot: BrainSnapshot | null }): BrainSurface {
  if (!input.hasServer) return "no_computer";
  const { snapshot } = input;
  // The computer states a problem only while the host is not running, so it outranks a recorded host id.
  if (snapshot?.host_problem) return input.connected ? "host_down" : "offline";
  if (snapshot?.host_worker?.id && snapshot.chat_thread_id) return "chat";
  return input.connected ? "waking" : "offline";
}

/**
 * Brain runs on an agent without structured chat events: its chat cannot be read,
 * only its terminal ("Chat isn't available for {agent}", soft-final/SPEC.md §1.3).
 * The computer always states `structured_events`; a snapshot without it reads as a chat.
 */
export function brainChatUnavailable(snapshot: BrainSnapshot | null | undefined): boolean {
  return snapshot?.host_executor?.capabilities?.structured_events === false;
}

/**
 * What Brain's chat area shows when it has no rows (soft-final/SPEC.md §1.3):
 * "loading", "failed" ("Couldn't load the chat": a load error, or a transcript the
 * computer cannot read), or "empty". A transcript still syncing reads as loading.
 */
export type BrainChatState = "content" | "loading" | "failed" | "empty";

export function brainChatState(input: {
  itemCount: number;
  loading: boolean;
  error?: string | null;
  conversation?: { available?: boolean; reason?: string } | null;
}): BrainChatState {
  if (input.itemCount > 0) return "content";
  if (input.loading) return "loading";
  if (input.error) return "failed";
  const conversation = input.conversation;
  if (conversation && conversation.available === false) {
    // No transcript yet is a chat with nothing in it (a fresh thread), as a Session reads it.
    if (conversation.reason === "transcript_not_found") return "empty";
    return isConversationSyncingReason(conversation.reason) ? "loading" : "failed";
  }
  return "empty";
}

/**
 * Why Brain's composer cannot send now, for its placeholder (soft-final/SPEC.md
 * §1.3); null when it can. Offline is not a reason: sends queue.
 */
export function brainComposerBlock(input: { surface: BrainSurface; snapshot: BrainSnapshot | null | undefined; chat: BrainChatState }): string | null {
  if (input.surface === "host_down") return "Brain can't start";
  if (input.surface !== "chat") return null;
  if (brainChatUnavailable(input.snapshot)) return "Chat isn't available";
  return input.chat === "failed" ? "Chat didn't load" : null;
}
