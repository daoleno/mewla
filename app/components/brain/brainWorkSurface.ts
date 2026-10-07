import type { BrainCurrentWork, BrainWorkerRef } from "../../store/brain";
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
export function brainWorkSurface(
  currentWork: readonly BrainCurrentWork[] | undefined,
  workers: readonly BrainWorkerRef[] | undefined,
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
    const status = brainWorkLifecycleStatus(lifecycle.lifecycle);
    const group = brainWorkGroup(work, status);
    const worker = work.attempt_session_id
      ? workerById.get(work.attempt_session_id)
      : undefined;
    counts[group] += 1;
    slips.push({
      workId: work.work_id,
      group,
      status,
      statusLabel: lifecycle.label,
      title: brainWorkTitle(work.title),
      who: worker ? workerWho(worker) : undefined,
      summary: slipSummary(work, group, worker),
      updatedAt: work.updated_at,
      sessionId: worker?.id,
      unread: work.unread_result,
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
