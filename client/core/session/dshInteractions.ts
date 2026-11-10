/** From the Expo app's services/dshInteractions.ts. */
import type { Transport } from "../transport";
export type DSHQuestion = { id: string; question: string; detail?: string; header?: string; multiSelect?: boolean; options?: { label: string; description?: string }[] };
export type DSHInteraction = { rpcId: string; payload: { type: "approval/requested" | "question/requested"; sessionId: string; approvalId?: string; toolName?: string; callId?: string; reason?: string; questions?: DSHQuestion[] } };
export type DSHInteractionSnapshot = { epoch: string; connected: boolean; items: DSHInteraction[]; rev?: number };
export type DSHAnswer = { epoch: string; rpcId: string; outcome?: "allowed-once" | "rejected"; cancel?: boolean; answer?: { answers: { id: string; selected: string[]; custom?: string }[] } };

const DSH_RETRY_BASE_MS = 1200;
const DSH_RETRY_MAX_MS = 30000;

/**
 * How long the panel waits before asking for the next snapshot. The daemon
 * holds a request that names the revision it last saw until something changes,
 * so a reply that carries a revision is followed at once. A failure backs off
 * instead of retrying at a fixed rate forever, and a daemon that does not hold
 * the request (no revision in the reply) is asked at the old fixed pace rather
 * than in a tight loop.
 */
export function nextDSHPollDelayMs(outcome: { ok: true; rev?: number } | { ok: false; failures: number }): number {
  if (outcome.ok) return typeof outcome.rev === "number" ? 0 : DSH_RETRY_BASE_MS;
  return Math.min(DSH_RETRY_MAX_MS, DSH_RETRY_BASE_MS * 2 ** Math.max(0, outcome.failures - 1));
}


/** Which run of a DSH Session the requests belong to. */
export type DSHSessionRun = { workerId: string; processId: number; startedAt: number };

/**
 * `dsh_interaction`: without an answer, the pending requests (the daemon holds
 * the call until they change past `afterRev`); with one, whether it was taken.
 */
export async function dshInteraction(transport: Transport, run: DSHSessionRun, answer?: DSHAnswer, afterRev?: number): Promise<DSHInteractionSnapshot | { accepted: boolean }> {
  const reply = await transport.request(
    "dsh_interaction",
    { worker_id: run.workerId, process_id: run.processId, started_at: run.startedAt, ...(answer ? { dsh_answer: answer } : {}), ...(afterRev !== undefined ? { dsh_after_rev: afterRev } : {}) },
    { reply: "dsh_interaction", timeoutMs: 15_000 },
  );
  const result = reply.result as Record<string, unknown> | undefined;
  const valid = answer
    ? result?.accepted === true
    : typeof result?.epoch === "string" && typeof result.connected === "boolean" && Array.isArray(result.items);
  if (!valid) throw new Error("Invalid DSH interaction response");
  return result as DSHInteractionSnapshot | { accepted: boolean };
}

/**
 * Follows a DSH Session's pending requests until `stop()`: each reply that
 * names a revision is followed at once (the daemon holds the next call), a
 * failure backs off. `onSnapshot(null, message)` reports a failure.
 */
export function watchDSHInteractions(transport: Transport, run: DSHSessionRun, onSnapshot: (snapshot: DSHInteractionSnapshot | null, error?: string) => void): () => void {
  let stopped = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let rev: number | undefined;
  let failures = 0;
  const next = async () => {
    if (stopped) return;
    let delay: number;
    try {
      const result = await dshInteraction(transport, run, undefined, rev);
      if (stopped) return;
      if ("items" in result) {
        rev = result.rev;
        onSnapshot(result);
      }
      failures = 0;
      delay = nextDSHPollDelayMs({ ok: true, rev });
    } catch (error) {
      if (stopped) return;
      failures += 1;
      onSnapshot(null, error instanceof Error ? error.message : "Requests unavailable");
      delay = nextDSHPollDelayMs({ ok: false, failures });
    }
    timer = setTimeout(() => void next(), delay);
  };
  void next();
  return () => {
    stopped = true;
    clearTimeout(timer);
  };
}
