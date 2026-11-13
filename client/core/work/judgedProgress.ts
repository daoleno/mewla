/**
 * A Work's progress as the daemon's judge keeps it (`daemon/brain/progress.go`): a short checklist of the
 * Work's outcomes, marked from evidence, and the percentage computed from the marks. The client never
 * computes or guesses one: no judgment, no number.
 *
 * What a row shows (SPEC §11, Progress):
 * - `moving`: the percentage, in the progress colour, rounded to 5: the judge gets the order of things
 *   right but is about 15 points off on average (a replay of 78 finished Works), so "42 %" would claim a
 *   precision it does not have;
 * - `back`: it went down because an outcome was reopened: the percentage in amber. The row's line stays
 *   what the Worker says it does; the judge's reason is on Work detail;
 * - `last`: every outcome is done and the Work is in its last stretch (reporting, Brain reading): a closed
 *   ring with a check, never "100 %";
 * - no number when the Work has no finish line, when it waits on a person (its own state says so), when
 *   it is closed, or before its first judgment.
 *
 * A Work whose Worker has reported is `waiting` while Brain reads the result: that is the last stretch and
 * keeps its mark. Only a person's part hides it: a question, a needs-you wake or state, or Brain parking
 * it on something it names (`wait_for`).
 */
import type { BrainCurrentWork } from "./types";

export type JudgedOutcomeState = "todo" | "doing" | "done";

export type JudgedOutcome = { id: string; text: string; weight: number; state: JudgedOutcomeState };

export type JudgedProgressStage = "moving" | "back" | "last" | "unmeasured";

/** The wire shape (`progress` on current_work[] and brain_work_detail). */
export type BrainWorkProgress = {
  stage: JudgedProgressStage;
  percent: number;
  outcomes?: JudgedOutcome[];
  back?: string;
  judged_at?: string;
  judgments?: number;
};

/** What a row draws: a fraction with its tone, or the last stretch. */
export type ProgressMark =
  | { kind: "percent"; percent: number; tone: "moving" | "back" }
  | { kind: "last" };

/**
 * The mark a running Work's row carries, or undefined for none. `waiting` is the Work's own fact that it
 * waits on a person (its question, its waiting group): then there is no number.
 */
export function progressMark(progress: BrainWorkProgress | undefined | null, options: { running: boolean; waiting?: boolean }): ProgressMark | undefined {
  if (!progress || !options.running || options.waiting) return undefined;
  if (progress.stage === "last") return { kind: "last" };
  if (progress.stage !== "moving" && progress.stage !== "back") return undefined;
  const percent = Math.max(0, Math.min(95, Math.round(progress.percent / 5) * 5));
  return { kind: "percent", percent, tone: progress.stage === "back" ? "back" : "moving" };
}

/** The mark a Work carries (`progressMark`), from the Work's own fields. */
export function workProgressMark(work: Pick<BrainCurrentWork, "status" | "progress" | "question" | "wake" | "wait_for">): ProgressMark | undefined {
  const open = work.status !== "done" && work.status !== "cancelled";
  const waiting = Boolean(work.question?.trim())
    || work.status === "needs_input"
    || work.wake?.kind === "user_input"
    || (work.status === "waiting" && Boolean(work.wait_for?.trim()));
  return progressMark(work.progress, { running: open, waiting });
}

/** The words a screen reader says for a mark ("42 percent", "Last stretch"). */
export function progressMarkLabel(mark: ProgressMark): string {
  return mark.kind === "last" ? "Last stretch" : `${mark.percent} percent${mark.tone === "back" ? ", sent back" : ""}`;
}

/** Why a sent-back Work went down, for the row's line; empty otherwise. */
export function progressBackLine(progress: BrainWorkProgress | undefined | null): string {
  return progress?.stage === "back" ? (progress.back ?? "").trim() : "";
}

/** The outcomes Work detail lists, in the judge's order. */
export function progressOutcomes(progress: BrainWorkProgress | undefined | null): JudgedOutcome[] {
  return (progress?.outcomes ?? []).filter((outcome) => outcome.text.trim() !== "");
}

/** Two progress values draw the same (the timeline keeps its item when nothing visible changed). */
export function sameProgress(left: BrainWorkProgress | undefined, right: BrainWorkProgress | undefined): boolean {
  if (left === right) return true;
  if (!left || !right) return false;
  if (left.stage !== right.stage || left.percent !== right.percent || (left.back ?? "") !== (right.back ?? "")) return false;
  const a = left.outcomes ?? [];
  const b = right.outcomes ?? [];
  return a.length === b.length && a.every((outcome, index) => outcome.id === b[index].id && outcome.state === b[index].state && outcome.text === b[index].text);
}
