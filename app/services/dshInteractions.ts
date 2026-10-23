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
