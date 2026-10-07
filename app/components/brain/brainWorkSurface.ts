import type { BrainCurrentWork, BrainWorkerRef } from "../../store/brain";

export type { BrainCurrentWork, BrainObjective, BrainWorkerRef } from "../../store/brain";
import type { WorkStatus } from "../ui/workStatus";
import { brainProviderLabel } from "./brainPresentation";
import {
  brainCurrentWorkLifecycle,
  brainWorkLifecycleStatus,
  brainWorkTitle,
} from "./brainWorkEventPresentation";

/**
 * Brain's one Work surface: the current Work, grouped by what it asks of
 * you. The phone shows it as a summary line under the header; wide screens
 * show it as the Work column. Both read the daemon's `current_work`, so a
 * Work leaves the surface as soon as the daemon stops listing it.
 */
export type BrainWorkGroup = "needs" | "running" | "back" | "waiting";

export const BRAIN_WORK_GROUP_ORDER: readonly BrainWorkGroup[] = [
  "needs",
  "running",
  "back",
  "waiting",
];

export const BRAIN_WORK_GROUP_LABELS: Readonly<Record<BrainWorkGroup, string>> = {
  needs: "Needs you",
  running: "Running",
  back: "Back",
  waiting: "Waiting",
};

export type BrainWorkSlip = {
  workId: string;
  group: BrainWorkGroup;
  status: WorkStatus;
  statusLabel: string;
  title: string;
  /** "Claude Code · perpetuo" when the Work's Session is live. */
  who?: string;
  summary?: string;
  updatedAt?: string;
  /** The live Session that owns the Work, when it is still open. */
  sessionId?: string;
  unread: boolean;
  /** What Brain asks, with one-tap answers. */
  question?: string;
  choices?: readonly string[];
  /** A running Worker's reported phase ("verifying"). */
  phase?: string;
  /** Waiting on a decision since then (immutable while it waits). */
  attentionSince?: string;
  waitedMs?: number;
  /** Nothing moves without a decision: outcome unknown, or a result left days without one. */
  stuck?: "outcome_unknown" | "no_decision";
  /** You answered; Brain has it and nothing new has come back yet. */
  replied?: { text?: string; at: string; uncertain: boolean };
  /** The Work has a terminal status but the result is unread. */
  closed?: boolean;
};

export type BrainWorkSurface = {
  slips: BrainWorkSlip[];
  counts: Record<BrainWorkGroup, number>;
};

/** Brain's own machine telemetry rides the Work queue but is not your Work. */
const RESOURCE_PRESSURE_WORK_PREFIX = "resource-pressure:";

/**
 * Current Work as slips, needs-you first. Closed Work, Calendar occurrences
 * that have not run yet (Calendar owns those) and Brain's resource telemetry
 * are left out.
 */
/**
 * A result stays under Back this long. After that, if Brain still has not
 * decided, it is no longer news but a decision nobody took, and it moves to
 * Needs you with Accept / Ask Brain on it.
 */
export const BRAIN_WORK_STUCK_AFTER_MS = 24 * 60 * 60 * 1000;

const OUTCOME_UNKNOWN_REASONS = new Set([
  "turn_lost",
  "lease_expired",
  "submission_ambiguous",
  "submission_failed",
]);

export function brainWorkSurface(
  currentWork: readonly BrainCurrentWork[] | undefined,
  workers: readonly BrainWorkerRef[] | undefined,
  now: number = Date.now(),
): BrainWorkSurface {
  const workerById = new Map((workers ?? []).map((worker) => [worker.id, worker] as const));
  const counts: Record<BrainWorkGroup, number> = {
    needs: 0,
    running: 0,
    back: 0,
    waiting: 0,
  };
  const slips: BrainWorkSlip[] = [];
  for (const work of currentWork ?? []) {
    if (!isSurfacedWork(work)) continue;
    const lifecycle = brainCurrentWorkLifecycle(work);
    let status = brainWorkLifecycleStatus(lifecycle.lifecycle);
    let statusLabel: string = lifecycle.label;
    let group = brainWorkGroup(work, status);
    const worker = work.attempt_session_id
      ? workerById.get(work.attempt_session_id)
      : undefined;
    const since = work.attention_since ? Date.parse(work.attention_since) : NaN;
    const waited = Number.isFinite(since) ? now - since : 0;
    const terminal = work.status === "done" || work.status === "cancelled";
    const replied =
      work.user_action?.kind === "reply" && !work.question && !terminal
        ? {
            text: work.user_action.text,
            at: work.user_action.at,
            uncertain: work.user_action.admission === "uncertain",
          }
        : undefined;
    let stuck: BrainWorkSlip["stuck"];
    if (!terminal && !work.attempt_session_id && work.attention_reason === "turn_failed") {
      // The Worker failed and nobody has retried: a Failed slip, not "Back".
      status = "failed";
      statusLabel = "Failed";
    }
    if (!terminal && !work.attempt_session_id && OUTCOME_UNKNOWN_REASONS.has(work.attention_reason ?? "")) {
      stuck = "outcome_unknown";
    } else if (!terminal && !replied && !work.question && group === "back" && status !== "failed" && waited > BRAIN_WORK_STUCK_AFTER_MS) {
      stuck = "no_decision";
    }
    if (work.question) {
      group = "needs";
      status = "needs";
      statusLabel = "Needs you";
    } else if (stuck) {
      group = "needs";
      status = "warning";
      statusLabel = stuck === "outcome_unknown" ? "Outcome unknown" : "No decision";
    } else if (replied) {
      group = "running";
      status = "running";
      statusLabel = "With Brain";
    }
    const snoozed = work.snoozed_until ? Date.parse(work.snoozed_until) > now : false;
    if (snoozed && group === "needs") {
      group = "waiting";
      statusLabel = "Snoozed";
    }
    counts[group] += 1;
    slips.push({
      workId: work.work_id,
      group,
      status,
      statusLabel,
      title: brainWorkTitle(work.title),
      who: worker ? workerWho(worker) : undefined,
      summary: work.question
        ? work.question
        : stuck
          ? stuckSummary(stuck, waited)
          : replied
            ? `You said “${replied.text ?? ""}”${replied.uncertain ? ". It may not have reached Brain." : ""}`
            : slipSummary(work, group, worker),
      updatedAt: work.updated_at,
      sessionId: worker?.id,
      unread: work.unread_result,
      question: work.question,
      choices: work.choices,
      phase: group === "running" && worker?.phase && worker.phase !== "working" ? worker.phase : undefined,
      attentionSince: work.attention_since,
      waitedMs: waited > 0 ? waited : undefined,
      stuck,
      replied,
      closed: terminal || undefined,
    });
  }
  const rank = (group: BrainWorkGroup) => BRAIN_WORK_GROUP_ORDER.indexOf(group);
  // Stable: within a group the daemon's order (review queue, then newest) holds.
  slips.sort((left, right) => rank(left.group) - rank(right.group));
  return { slips, counts };
}

function isSurfacedWork(work: BrainCurrentWork): boolean {
  // Closed Work leaves at once, unless it brought back a result you haven't read.
  if (work.status === "cancelled") return false;
  if (work.status === "done") return work.unread_result;
  if (work.work_id.startsWith(RESOURCE_PRESSURE_WORK_PREFIX)) return false;
  return !(work.status === "open" && work.wake?.kind === "calendar_result");
}

/**
 * What the Work asks of you right now, from the same lifecycle the slips in
 * the conversation show, so the column and the conversation agree.
 */
export function brainWorkGroup(
  work: BrainCurrentWork,
  status: WorkStatus = brainWorkLifecycleStatus(brainCurrentWorkLifecycle(work).lifecycle),
): BrainWorkGroup {
  if (status === "needs") return "needs";
  if (work.status === "running" || status === "running") return "running";
  if (status === "blocked") return "waiting";
  return "back";
}

/** "3 running · 1 needs you", or null when nothing is current. */
export function brainWorkSummaryLine(
  counts: Record<BrainWorkGroup, number>,
): string | null {
  const parts: string[] = [];
  if (counts.needs) parts.push(`${counts.needs} ${counts.needs === 1 ? "needs" : "need"} you`);
  if (counts.running) parts.push(`${counts.running} running`);
  if (counts.back) parts.push(`${counts.back} back`);
  if (counts.waiting) parts.push(`${counts.waiting} waiting`);
  return parts.length ? parts.join(" · ") : null;
}

/** "Claude Code · perpetuo": a live Session's executor and project. */
export function workerWho(worker: BrainWorkerRef): string | undefined {
  const executor = worker.command ? brainProviderLabel(worker.command) : "";
  const project = projectName(worker.cwd);
  return [executor, project].filter(Boolean).join(" · ") || undefined;
}

function projectName(cwd: string | undefined): string {
  const parts = (cwd ?? "").split("/").filter(Boolean);
  return parts[parts.length - 1] ?? "";
}

/** "3 days" from a span in milliseconds; whole hours below a day. */
export function brainWorkAge(ms: number): string {
  const hours = Math.max(1, Math.round(ms / 3_600_000));
  if (hours < 24) return hours === 1 ? "an hour" : `${hours} hours`;
  const days = Math.round(hours / 24);
  return days === 1 ? "a day" : `${days} days`;
}

function stuckSummary(stuck: NonNullable<BrainWorkSlip["stuck"]>, waited: number): string {
  if (stuck === "outcome_unknown") {
    return "The Worker's Session ended without a result. Ask Brain to check, or close it.";
  }
  return `Back for ${brainWorkAge(waited)} with no decision. Close it, or ask Brain.`;
}

function slipSummary(
  work: BrainCurrentWork,
  group: BrainWorkGroup,
  worker: BrainWorkerRef | undefined,
): string | undefined {
  // A wait owned by a Session ("Session %90") says nothing the slip doesn't.
  const waitFor = work.wait_for && !work.wake && work.attempt_session_id
    ? undefined
    : work.wait_for;
  const candidates =
    group === "running"
      ? [worker?.summary, work.summary]
      : group === "back"
        ? [work.summary, worker?.summary, waitFor]
        : [waitFor, work.summary];
  // A bare reference ("operator:2985…") is plumbing, not something to read.
  return candidates
    .map((value) => value?.replace(/\s+/g, " ").trim())
    .find((value) => Boolean(value && value.includes(" ")));
}
