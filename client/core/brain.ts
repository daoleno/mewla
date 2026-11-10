import type { Transport } from "./transport";

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
export function newBrainChat(transport: Transport): Promise<BrainSnapshot> {
  return transport
    .request<{ brain?: BrainSnapshot }>("brain_chat_new", {}, { reply: "brain_snapshot" })
    .then((frame) => frame.brain ?? {});
}
