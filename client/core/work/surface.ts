import { workProgressMark, type BrainWorkProgress, type ProgressMark } from "./judgedProgress";
import type { CalendarClock, CalendarItem } from "../calendar/model";
import { readableSummary } from "../sessions/activity";
import type { BrainCurrentWork, BrainOpenWork, BrainWorkerRef, BrainWorkShown, WorkWorkerSession } from "./types";

/** How a Work's state reads: a ring, a seal, a check (DESIGN.md's Work states). */
export type WorkStatus = "ready" | "running" | "needs" | "warning" | "failed" | "blocked";

export type BrainWorkLifecycle =
  | "working"
  | "blocked"
  | "ready"
  | "reviewing"
  | "waiting"
  | "done"
  | "cancelled"
  | "needs_you"
  | "uncertain"
  | "failed";

export type BrainWorkLifecyclePresentation = {
  lifecycle: BrainWorkLifecycle;
  label:
    | "Working"
    | "Blocked"
    | "Ready"
    | "Reviewing"
    | "Waiting"
    | "Done"
    | "Cancelled"
    | "Needs you"
    | "Needs review"
    | "Failed";
};

const CANONICAL_SESSION_SUFFIX = /\s*\(mewla-worker-[^()\s]+:@\d+\)\s*$/i;
const PROVIDER_TURN_ID_GLOBAL = /\bturn:[a-z0-9-]+\b/gi;

export function brainWorkTitle(value: string): string {
  const normalized = value
    .replace(CANONICAL_SESSION_SUFFIX, "")
    .replace(PROVIDER_TURN_ID_GLOBAL, "")
    .replace(/\s+/g, " ")
    .trim();
  if (!normalized) {
    return "Delegated Work";
  }
  return !normalized.includes(" ") && normalized.includes("-")
    ? normalized.replace(/-+/g, " ")
    : normalized;
}

const LIFECYCLE_LABEL: Record<BrainWorkLifecycle, BrainWorkLifecyclePresentation["label"]> = {
  working: "Working", blocked: "Blocked", ready: "Ready", reviewing: "Reviewing", waiting: "Waiting", done: "Done",
  cancelled: "Cancelled", needs_you: "Needs you", uncertain: "Needs review", failed: "Failed",
};
function lifecyclePresentation(lifecycle: BrainWorkLifecycle): BrainWorkLifecyclePresentation {
  return { lifecycle, label: LIFECYCLE_LABEL[lifecycle] };
}

/** Where a Work stands, from the daemon's fields alone. */
export function brainCurrentWorkLifecycle(work: BrainCurrentWork): BrainWorkLifecyclePresentation {
  if (work.status === "cancelled") return lifecyclePresentation("cancelled");
  if (work.status === "done") return lifecyclePresentation("done");
  // Only Brain's question asks you something (owner 2026-11-19): a Work parked on a user_input wake with nothing
  // asked is Brain's wait, quiet like any other.
  if (work.question?.trim()) return lifecyclePresentation("needs_you");
  if (work.attention_state === "reviewing") return lifecyclePresentation("reviewing");
  // A Worker's request (the daemon's needs_input) is Brain's to resolve, whether Brain's review is queued or
  // under way: it reaches you only as Brain's question (`work.question`), never by itself, and it is not a
  // result that came back (owner 2026-11-13, shot 11).
  if (work.status === "needs_input") return lifecyclePresentation("reviewing");
  if (work.attention_state === "queued" || work.attention_state === "reserved" || work.progress_mode === "ready") return lifecyclePresentation("ready");
  if (work.status === "waiting" || work.progress_mode === "waiting") return lifecyclePresentation("waiting");
  return lifecyclePresentation("working");
}

export function brainWorkLifecycleStatus(lifecycle: BrainWorkLifecycle): WorkStatus {
  switch (lifecycle) {
    case "ready":
    case "done":
      return "ready";
    case "working":
    case "reviewing":
      return "running";
    case "needs_you":
      return "needs";
    case "uncertain":
      return "warning";
    case "failed":
      return "failed";
    case "blocked":
    case "waiting":
    case "cancelled":
      return "blocked";
  }
}

/**
 * Brain's one Work surface: the current Work, grouped by what it asks of
 * you. The phone shows it as a summary line under the header; wide screens
 * show it as the Work column. Both read the daemon's `current_work`, then its
 * `open_work` (unfinished Work no Worker has yet: Queued, or Waiting), so a
 * Work leaves the surface as soon as the daemon stops listing it.
 */
export type BrainWorkGroup = "needs" | "running" | "back" | "waiting" | "queued";

export const BRAIN_WORK_GROUP_ORDER: readonly BrainWorkGroup[] = [
  "needs",
  "running",
  "back",
  "waiting",
  "queued",
];

export const BRAIN_WORK_GROUP_LABELS: Readonly<Record<BrainWorkGroup, string>> = {
  needs: "Needs you",
  running: "Running",
  back: "Back",
  waiting: "Waiting",
  queued: "Queued",
};

export type BrainWorkSlip = {
  workId: string;
  group: BrainWorkGroup;
  status: WorkStatus;
  statusLabel: string;
  title: string;
  /** "Claude Code · perpetuo" when the Work's Session is live. */
  who?: string;
  /** The live Session's project ("perpetuo"): Work detail's eyebrow reads "Work · perpetuo". */
  project?: string;
  summary?: string;
  updatedAt?: string;
  /** The live Session that owns the Work, when it is still open. */
  sessionId?: string;
  /** A delegated Worker runs its Attempt: the daemon's `stop` cancels the Work and closes that Worker. */
  stoppable?: boolean;
  unread: boolean;
  /** What Brain asks, with one-tap answers. */
  question?: string;
  choices?: readonly string[];
  /** The choice Brain recommends, when it is one of `choices`. */
  recommended?: string;
  /** What the Work waits on, as Brain wrote it. */
  waitFor?: string;
  /** A running Worker's reported phase ("verifying"). */
  phase?: string;
  /** The judged progress (`judgedProgress.ts`), while the Work is open: Work detail lists its outcomes. */
  progress?: BrainWorkProgress;
  /** What its row and card draw of it (`workProgressMark`); none while a person has a part, or before a judgment. */
  mark?: ProgressMark;
  /** Waiting on a decision since then (immutable while it waits). */
  attentionSince?: string;
  waitedMs?: number;
  /** Nothing moves without a decision: outcome unknown, or a result left days without one. */
  stuck?: "outcome_unknown" | "no_decision";
  /** You answered; Brain has it and nothing new has come back yet. */
  replied?: { text?: string; at: string; uncertain: boolean };
  /** The Work has a terminal status but the result is unread. */
  closed?: boolean;
  /** A Calendar run's Work: its schedule and run. */
  calendar?: { itemId: string; runId: string };
  /** The slip's meta: "Oct 27", or "运营：挖稿 · Oct 27" when the titles differ (brainWorkCalendarOrigins). */
  calendarLabel?: string;
  /** "运营：挖稿 · Oct 27", the link back to the schedule. */
  calendarLink?: string;
  /** Every Worker Session that joined it, oldest first (core/work/workers `workWorkerLines`). */
  workerSessions?: readonly WorkWorkerSession[];
  /** Where it stands, as the judge wrote it in the person's language (`shown.line`); absent before a judgment. */
  shownLine?: string;
  /** Where the Work stood when that line was written (`shown.status`): "done" says how it ended. */
  shownStatus?: "active" | "waiting" | "done";
  /** The project the judge or the person put it under (`shown.project`); the tray's chips filter by it. */
  shownProject?: string;
  /** The next step, as the judge wrote it (`shown.next`). */
  shownNext?: string;
  /** The Work the person merged this one into (`shown.merged_into`). */
  mergedInto?: string;
  /** The Works merged into this one whose rows it shows (`foldMergedSlips`). */
  merged?: { workId: string; title: string }[];
  /** When what the person sees of it last changed (`shown.changed_at`, else its last update): the tray's order. */
  changedAt?: string;
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
 * A result stays under Back this long before it counts as a decision nobody took. It stays where it is, never in
 * Needs you: an undecided result is Brain's to settle, and the computer wakes Brain for it (owner 2026-11-19).
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
  openWork?: readonly BrainOpenWork[],
): BrainWorkSurface {
  const workerById = new Map((workers ?? []).map((worker) => [worker.id, worker] as const));
  const counts: Record<BrainWorkGroup, number> = {
    needs: 0,
    running: 0,
    back: 0,
    waiting: 0,
    queued: 0,
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
    } else if (
      !terminal &&
      !work.question &&
      status !== "failed" &&
      waited > BRAIN_WORK_STUCK_AFTER_MS &&
      (group === "back" || replied)
    ) {
      // Back, or answered, for over a day and Brain has not moved it.
      stuck = "no_decision";
    }
    if (work.question) {
      group = "needs";
      status = "needs";
      statusLabel = "Needs you";
    } else if (stuck === "outcome_unknown") {
      // A lost or unconfirmed turn is Brain's to handle (it is woken for it), not the person's (owner 11-13 19:4x).
      group = "running";
      status = "running";
      statusLabel = "Reviewing";
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
      title: shownTitle(work.shown) || brainWorkTitle(work.title),
      who: worker ? workerWho(worker) : undefined,
      project: projectName(worker?.cwd) || undefined,
      summary: work.question
        ? work.question
        : stuck === "outcome_unknown"
          ? "The Worker's Session ended without a result. Ask Brain to check, or close it."
          : replied
            ? `You said “${replied.text ?? ""}”${replied.uncertain ? ". It may not have reached Brain." : ""}`
            : slipSummary(work, group, worker),
      // updated_at moves on every daemon sync; a waiting Work dates from when it began waiting.
      updatedAt: work.attention_since ?? work.updated_at,
      sessionId: worker?.id,
      stoppable: (Boolean(work.attempt_session_id) && work.attempt_delegated === true) || undefined,
      unread: work.unread_result,
      question: work.question,
      choices: work.choices,
      recommended: work.recommended && work.choices?.includes(work.recommended) ? work.recommended : undefined,
      waitFor: work.wait_for,
      phase: group === "running" && worker?.phase && worker.phase !== "working" ? worker.phase : undefined,
      attentionSince: work.attention_since,
      waitedMs: waited > 0 ? waited : undefined,
      stuck,
      replied,
      closed: terminal || undefined,
      calendar: work.calendar ? { itemId: work.calendar.item_id, runId: work.calendar.run_id } : undefined,
      workerSessions: work.worker_sessions,
      progress: terminal ? undefined : work.progress,
      mark: workProgressMark(work),
      ...shownParts(work.shown, work.updated_at),
    });
  }
  const listed = new Set(slips.map((slip) => slip.workId));
  for (const work of openWork ?? []) {
    if (listed.has(work.work_id) || !isSurfacedOpenWork(work)) continue;
    const waiting = work.progress_mode === "waiting" || work.status === "waiting" || work.status === "needs_input";
    const group: BrainWorkGroup = waiting ? "waiting" : "queued";
    counts[group] += 1;
    slips.push({
      workId: work.work_id,
      group,
      // The dashed ring: nothing is working on it yet.
      status: "blocked",
      statusLabel: BRAIN_WORK_GROUP_LABELS[group],
      title: shownTitle(work.shown) || brainWorkTitle(work.title),
      // Brain's wait note is Brain's shorthand, never words for the person (owner 2026-11-19): the judge says it.
      summary: undefined,
      updatedAt: work.updated_at,
      unread: false,
      calendar: work.calendar ? { itemId: work.calendar.item_id, runId: work.calendar.run_id } : undefined,
      workerSessions: work.worker_sessions,
      progress: work.progress,
      mark: workProgressMark({ status: work.status, progress: work.progress }),
      ...shownParts(work.shown, work.updated_at),
    });
  }
  const rank = (group: BrainWorkGroup) => BRAIN_WORK_GROUP_ORDER.indexOf(group);
  // Stable: within a group the daemon's order (review queue, then newest) holds.
  slips.sort((left, right) => rank(left.group) - rank(right.group));
  return { slips, counts };
}

function shownTitle(shown: BrainWorkShown | undefined): string {
  return shown?.title?.trim() ?? "";
}

/** What the judge wrote for the row, beside the slip's own fields. */
function shownParts(shown: BrainWorkShown | undefined, updatedAt: string | undefined): Pick<BrainWorkSlip, "shownLine" | "shownStatus" | "shownProject" | "shownNext" | "mergedInto" | "changedAt"> {
  return {
    shownLine: shown?.line?.trim() || undefined,
    shownStatus: shown?.status,
    shownProject: shown?.project?.trim() || undefined,
    shownNext: shown?.next?.trim() || undefined,
    mergedInto: shown?.merged_into || undefined,
    changedAt: shown?.changed_at ?? updatedAt,
  };
}

/**
 * A Work the person merged into another shows under it (owner's ⋯ › Merge with…): its row goes and its Workers
 * join the other's. Only the list changes. A Work that asks the person something or has a result they have not
 * read keeps its own row, and so does one whose target is not listed or is itself merged.
 */
export function foldMergedSlips(slips: readonly BrainWorkSlip[]): BrainWorkSlip[] {
  const byId = new Map(slips.map((slip) => [slip.workId, slip] as const));
  const under = new Map<string, BrainWorkSlip[]>();
  for (const slip of slips) {
    if (!slip.mergedInto || slip.group === "needs" || slip.question || slip.unread) continue;
    const target = byId.get(slip.mergedInto);
    if (!target || target.mergedInto) continue;
    under.set(target.workId, [...(under.get(target.workId) ?? []), slip]);
  }
  if (!under.size) return [...slips];
  const gone = new Set([...under.values()].flat().map((slip) => slip.workId));
  return slips.filter((slip) => !gone.has(slip.workId)).map((slip) => {
    const merged = under.get(slip.workId);
    if (!merged) return slip;
    return { ...slip, workerSessions: mergedWorkers(slip, merged), merged: merged.map((m) => ({ workId: m.workId, title: m.title })) };
  });
}

/** A Work's Worker Sessions, then those of the Works merged into it, each once. */
export function mergedWorkers(slip: Pick<BrainWorkSlip, "workerSessions">, merged: readonly Pick<BrainWorkSlip, "workerSessions">[]): WorkWorkerSession[] {
  const sessions = [...(slip.workerSessions ?? [])];
  const seen = new Set(sessions.map((worker) => worker.session_id));
  for (const other of merged) {
    for (const worker of other.workerSessions ?? []) {
      if (seen.has(worker.session_id)) continue;
      seen.add(worker.session_id);
      sessions.push(worker);
    }
  }
  return sessions;
}

function isSurfacedOpenWork(work: BrainOpenWork): boolean {
  if (work.work_id.startsWith(RESOURCE_PRESSURE_WORK_PREFIX)) return false;
  // A Calendar occurrence that has not run yet: Calendar owns it.
  return work.wake?.kind !== "calendar_result";
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

/** "Claude Code · perpetuo": a live Session's executor and project. */
export function workerWho(worker: BrainWorkerRef): string | undefined {
  const executor = worker.command ? providerLabel(worker.command) : "";
  const project = projectName(worker.cwd);
  return [executor, project].filter(Boolean).join(" · ") || undefined;
}

/** A working directory's last part ("perpetuo"), or "" with none. */
export function projectName(cwd: string | undefined): string {
  const parts = (cwd ?? "").split("/").filter(Boolean);
  return parts[parts.length - 1] ?? "";
}

/** The runtime's reason code at the head of a line: "worker_stalled: stopped", "lease_expired_escalation". */
const REASON_CODE = /^[a-z]+(?:_[a-z]+)+(?::|$)/;

function slipSummary(
  work: BrainCurrentWork,
  group: BrainWorkGroup,
  worker: BrainWorkerRef | undefined,
): string | undefined {
  // Brain's wait note (`wait_for`) is Brain's shorthand ("11-20 03:17Z retention run; Trigger c57a7c00 pings…"),
  // never words for the person (owner 2026-11-19): what a Work waits on is the judge's line to say.
  // A Session's summary is what it reported only once it reported a phase; before, it is the daemon reading
  // its screen (a pasted command, the provider's footer), as core/sessions/activity says.
  const reported = worker?.phase?.trim() ? worker.summary : undefined;
  const candidates =
    group === "running"
      ? [reported, work.summary]
      : group === "back"
        ? [work.summary, reported]
        : [work.summary];
  // A bare reference ("operator:2985…"), the runtime's reason code ("worker_stalled: stopped") and a terminal's
  // footer chrome are plumbing: none is something to read.
  return candidates
    .map((value) => readableSummary(value))
    .find((value) => Boolean(value && value.includes(" ") && !REASON_CODE.test(value)));
}

/**
 * Names each Calendar run's slip after its schedule ("运营：挖稿 · Oct 27")
 * and, while the run works, gives it the run's Session to open: Calendar,
 * not Brain, started that Worker, so Brain has no Attempt for it.
 */
export function brainWorkCalendarOrigins(
  surface: BrainWorkSurface,
  items: readonly CalendarItem[] | undefined,
  clock: CalendarClock,
): BrainWorkSurface {
  if (!surface.slips.some((slip) => slip.calendar)) return surface;
  const slips = surface.slips.map((slip): BrainWorkSlip => {
      if (!slip.calendar) return slip;
      const item = items?.find((candidate) => candidate.id === slip.calendar!.itemId);
      const run = item?.runs?.find((candidate) => candidate.id === slip.calendar!.runId);
      const origin = originParts(slip.calendar, items ?? [], clock, slip.title);
      const sessionId = run?.status === "running" ? run.worker_session : undefined;
      return {
        ...slip,
        // The slip's title is the schedule's: the meta line adds the date.
        calendarLabel: origin ? (origin.title === slip.title ? origin.date || origin.label : origin.label) : undefined,
        calendarLink: origin?.label,
        sessionId: slip.sessionId ?? sessionId,
        ...(sessionId && slip.group !== "needs" ? { group: "running" as const, status: "running" as const, statusLabel: "Running" } : {}),
      };
  });
  const counts = Object.fromEntries(BRAIN_WORK_GROUP_ORDER.map((group) => [group, 0])) as Record<BrainWorkGroup, number>;
  for (const slip of slips) counts[slip.group] += 1;
  const rank = (group: BrainWorkGroup) => BRAIN_WORK_GROUP_ORDER.indexOf(group);
  slips.sort((left, right) => rank(left.group) - rank(right.group));
  return { slips, counts };
}

/** "Claude Code" for the executor a Session runs. */
export function providerLabel(value?: string): string {
  switch (value?.trim().toLowerCase()) {
    case "codex": return "Codex";
    case "cursor": return "Cursor Agent";
    case "grok": return "Grok";
    case "antigravity": return "Antigravity";
    case "claude": return "Claude Code";
    default: return value?.trim() || "Custom";
  }
}

/** The schedule's name and the run's date of a Calendar run's Work. */
function originParts(origin: { itemId: string; runId: string }, items: readonly CalendarItem[], clock: CalendarClock, fallbackTitle: string) {
  const item = items.find((candidate) => candidate.id === origin.itemId);
  const run = item?.runs?.find((candidate) => candidate.id === origin.runId);
  const title = (item?.title || fallbackTitle || "").trim();
  if (!title) return null;
  const at = run?.scheduled_for;
  const date = at ? clock.format.shortDate(Date.parse(at), item?.timezone ?? clock.zones.device) : "";
  return { title, date, label: date ? `${title} · ${date}` : title };
}
