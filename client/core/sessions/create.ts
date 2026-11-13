import type { Frame, Transport } from "../transport";
import { reconcileCreateSessionFailure, reconcileCreateSessionSuccess, type CreateSessionReconciliation } from "./createSessionReconcile";
import { invalidProviderReply, providerErrorFromPayload } from "./providerErrors";
import { classifyMutationPersistence, parseOptionalMutationPersistence } from "./providerPersistence";

export type CreateSessionOptions = {
  cwd?: string;
  command?: string;
  name?: string;
  /** The Session a "New Terminal" is opened from: the new window joins its tmux session. */
  targetId?: string;
  /** The Provider connection and model the new Session launches with (New Terminal carries its Provider's). */
  connectionId?: string;
  modelId?: string;
  /** The Session's first message (New session's "What should it do?"): the computer holds it until the agent is ready. */
  text?: string;
};

/** `firstMessage: "pending"`: the computer holds `text` (WIRE §11); absent, a computer from before it ignored the text. */
export type CreatedSession = CreateSessionReconciliation & { workerSession?: Record<string, unknown>; firstMessage?: "pending" };

/**
 * `create_session`, settled as the Expo app settled it: the new Session's id
 * (and its row, when the computer sends it) on success; "ambiguous" when the
 * request went out and no clear answer came back (a timeout, a dropped
 * socket, an unclear write), after which no new Session may be created until
 * a fresh list shows what happened; "failed" when nothing was created.
 */
export async function createSession(transport: Transport, options: CreateSessionOptions): Promise<CreatedSession> {
  if (transport.state !== "connected") return reconcileCreateSessionFailure(new Error("Your computer is offline."), false);
  let reply: Frame;
  try {
    reply = await transport.request("create_session", {
      ...(options.targetId ? { target_id: options.targetId } : {}),
      cwd: options.cwd ?? "",
      command: options.command ?? "",
      name: options.name ?? "",
      ...(options.connectionId?.trim() ? { connection_id: options.connectionId.trim() } : {}),
      ...(options.modelId?.trim() ? { model_id: options.modelId.trim() } : {}),
      ...(options.text?.trim() ? { text: options.text.trim() } : {}),
    }, { reply: "session_created", timeoutMs: 10_000 });
  } catch (error) {
    const code = (error as { code?: unknown }).code;
    const known = typeof code === "string" && code ? providerErrorFromPayload({ code, message: (error as Error).message }) : error;
    return reconcileCreateSessionFailure(known, true);
  }
  const workerSession = reply.worker_session && typeof reply.worker_session === "object" ? (reply.worker_session as Record<string, unknown>) : undefined;
  if (typeof reply.worker_id !== "string" || !reply.worker_id) {
    return { ...reconcileCreateSessionFailure(invalidProviderReply("Your computer returned an invalid session id."), true), workerSession };
  }
  const persistence = parseOptionalMutationPersistence(reply);
  if (persistence && classifyMutationPersistence(persistence) === "not_applied" && reply.code) {
    return { ...reconcileCreateSessionFailure(providerErrorFromPayload({ code: String(reply.code), message: String(reply.persistence_warning || reply.message || "Session was not created.") }), false), workerSession };
  }
  return { ...reconcileCreateSessionSuccess({ workerId: reply.worker_id, persistence }), workerSession, ...(reply.first_message === "pending" ? { firstMessage: "pending" as const } : {}) };
}
