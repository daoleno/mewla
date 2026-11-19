/**
 * A Work in Brain's thread (soft-final SPEC §1.2, §11 Work card, Work result card): one card per
 * Work, the newest. While it runs it is a live card; when it came back it is a result card while
 * new, then its quiet line; one that waits on a decision is a request (the tray's Needs you leads
 * to Work detail), so its card says so and opens Work detail.
 */
import type { BrainWorkEventKind } from "./brainWorkEvent";
import type { BrainWorkEventTimelineItem } from "./types";
import type { ResultAction } from "../work/types";
import { workProgressMark, type ProgressMark } from "../work/judgedProgress";
import { workerName } from "../work/workers";

export type WorkCardKind = "live" | "needs" | "result" | "line";

export interface WorkCardModel {
  kind: WorkCardKind;
  workId: string;
  title: string;
  /** The result's words, or what the live Work is doing. */
  summary: string;
  /** "Done", "Failed", "Running", "Needs you". */
  state: string;
  /** When it came back (ms), for "Agent · Done 11:20". */
  at: number | null;
  /** The Session that ran it: the header opens it, its logo is the card's. */
  sessionId?: string;
  sessionName?: string;
  unread: boolean;
  /** A result's next steps, as the computer found them truthfully offered (WIRE §4). */
  actions: readonly ResultAction[];
  /** A live Work's judged progress, as its tray row draws it (`workProgressMark`). */
  mark?: ProgressMark;
  /**
   * The judge's name and line for it in the person's language (`shown`), when it has written them: a card that
   * draws its Session's row names the Work with them, not the Session's code name and its Worker's last words.
   */
  shownTitle?: string;
  shownLine?: string;
}

const RESULT_STATE: Record<BrainWorkEventKind, string> = {
  "session.done": "Done",
  "session.failed": "Failed",
  "session.needs_input": "Asked Brain",
  "session.stale": "Stalled",
  "session.uncertain": "Unclear",
  "session.ownership_lost": "Lost",
};

export function workCardModel(item: Pick<BrainWorkEventTimelineItem, "event" | "currentWork">): WorkCardModel {
  const event = item.event;
  const work = item.currentWork;
  const status = work?.status;
  const at = Date.parse(event.occurred_at);
  const base = {
    workId: event.work_id,
    // The judge's name for it in the person's language; Brain's code name ("XcodeLogsViewerHang") only before.
    title: work?.shown?.title?.trim() || work?.title || event.work_title || "Work",
    at: Number.isFinite(at) && at > 0 ? at : null,
    sessionId: work?.attempt_session_id || event.session_id,
    sessionName: workerName(event.session_name) || undefined,
    actions: event.actions ?? [],
    shownTitle: work?.shown?.title?.trim() || undefined,
  };
  // Only Brain's question asks you; a Worker's request without one is Brain's to resolve (owner 2026-11-13).
  if (work?.question && status !== "done" && status !== "cancelled") return { ...base, kind: "needs", summary: work.question, state: "Needs you", unread: false };
  // A lost, stalled or uncertain turn, and a Worker's request, are Brain's to handle: never a result for the person,
  // and never the daemon's words ("Delegated Session ended; outcome is unknown", owner 11-13 19:4x).
  const brains = BRAINS_EVENTS.has(event.kind);
  // Where it stands as the judge wrote it in the person's language. Brain's wait note (`wait_for`) is Brain's
  // shorthand and never the card's words: a parked Work asks nothing of the person (owner 2026-11-19).
  const judged = work?.shown?.line?.trim() || "";
  if (status === "running" || status === "open" || status === "waiting" || status === "needs_input") {
    const state = status === "running" ? "Running" : status === "needs_input" ? "Reviewing" : "Waiting";
    const fallback = status === "waiting" ? "Nothing needed from you" : (brains ? work?.summary : work?.summary || event.summary) || "";
    return { ...base, kind: "live", summary: judged || fallback, state, unread: false, mark: work ? workProgressMark(work) : undefined, shownLine: judged || (status === "waiting" ? fallback : undefined) };
  }
  // A closed Work's line is the judge's once it read how the Work ended; until then what came back.
  const ended = work?.shown?.status === "done" ? judged : "";
  if (brains) return { ...base, kind: "line", summary: work?.status === "done" ? ended || work.summary || "" : "", state: work?.status === "done" ? "Done" : "Stopped", unread: false };
  const unread = work ? work.unread_result : event.unread;
  return { ...base, kind: unread ? "result" : "line", summary: ended || work?.summary || event.summary, state: RESULT_STATE[event.kind] ?? "Done", unread };
}

/** The events Brain handles itself: the Work's next step is Brain's, so the thread shows the Work, not the event. */
const BRAINS_EVENTS = new Set<BrainWorkEventKind>(["session.needs_input", "session.stale", "session.uncertain", "session.ownership_lost"]);
