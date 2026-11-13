import { brainWorkSlipStale, type BrainWorkSlip } from "./surface";
import type { BrainWorkUserActionKind } from "./types";

/**
 * What you can do with a Work, by where it stands. One table, read by the
 * slip (the first, primary actions inline) and by its detail sheet (all of
 * them), so a state never offers a dead end.
 *
 * - answer: send a choice to Brain as the Work's reply
 * - reply: open the reply box for free text
 * - ask: open Brain with the Work quoted in the composer
 * - open: open the Worker Session
 * - close / dismiss / stop: close the Work as the user (done / not needed / stop the Worker)
 * - snooze: hide it from Needs you for an hour
 * - read: mark a closed Work's result read so it leaves
 */
export type BrainWorkActionKind =
  | "answer"
  | "reply"
  | "ask"
  | "open"
  | "close"
  | "dismiss"
  | "stop"
  | "snooze"
  | "read";

export type BrainWorkAction = {
  kind: BrainWorkActionKind;
  label: string;
  /** For answer: the choice sent as the reply. */
  text?: string;
  primary?: boolean;
  /** For answer: the choice Brain recommends ("Brain's pick"). */
  pick?: boolean;
  /** Ask before doing it; the confirm says what will happen. */
  confirm?: string;
};

export function brainWorkActions(slip: BrainWorkSlip): BrainWorkAction[] {
  if (slip.closed) {
    return [
      { kind: "read", label: "Mark reviewed", primary: true },
      { kind: "ask", label: "Ask Brain about this" },
    ];
  }
  if (slip.question) {
    // Brain's pick leads when there is one, else the first choice, as before.
    const choices = (slip.choices ?? []).map<BrainWorkAction>((choice, index) => ({
      kind: "answer",
      label: choice,
      text: choice,
      primary: slip.recommended ? choice === slip.recommended : index === 0,
      pick: choice === slip.recommended || undefined,
    }));
    return [
      ...choices,
      { kind: "reply", label: choices.length ? "Something else…" : "Reply", primary: choices.length === 0 },
      { kind: "snooze", label: "Snooze 1 hour" },
      { kind: "dismiss", label: "Not needed anymore", confirm: "Brain stops this Work and won't bring it up again." },
    ];
  }
  if (slip.stuck === "outcome_unknown") {
    return [
      { kind: "ask", label: "Ask Brain to check", primary: true },
      { kind: "close", label: "Close it", confirm: "Close this Work as done. Brain won't check it." },
      { kind: "dismiss", label: "Not needed anymore", confirm: "Brain stops this Work and won't bring it up again." },
    ];
  }
  if (slip.status === "failed") {
    return [
      { kind: "ask", label: "Retry with Brain", primary: true },
      { kind: "dismiss", label: "Dismiss", confirm: "Brain stops this Work and won't retry it." },
    ];
  }
  if (slip.stuck === "no_decision" || slip.group === "back") {
    return [
      { kind: "close", label: "Accept", primary: true },
      { kind: "ask", label: "Ask Brain about this" },
      { kind: "dismiss", label: "Not needed anymore", confirm: "Brain stops this Work and won't bring it up again." },
    ];
  }
  if (slip.group === "needs") {
    return [
      { kind: "reply", label: "Reply", primary: true },
      { kind: "snooze", label: "Snooze 1 hour" },
      { kind: "dismiss", label: "Not needed anymore", confirm: "Brain stops this Work and won't bring it up again." },
    ];
  }
  if (slip.group === "running") {
    const open: BrainWorkAction[] = slip.sessionId ? [{ kind: "open", label: "Open Worker", primary: true }] : [];
    // The daemon stops only a Work whose Attempt runs in a delegated Worker. Calendar, not Brain, runs a
    // Calendar run's Worker: Stop would cancel Brain's Work and leave the run going.
    const stop: BrainWorkAction[] = slip.stoppable && !slip.calendar
      ? [{ kind: "stop", label: "Stop", confirm: "The Worker is closed and its Work cancelled. Unsaved work in it is lost." }]
      : [];
    // Snoozed, it stays out of Needs you for the hour if it comes to need you (SPEC §11 Work detail).
    return [...open, { kind: "ask", label: "Ask Brain about this" }, { kind: "snooze", label: "Snooze 1 hour" }, ...stop];
  }
  if (slip.group === "queued" || slip.group === "waiting") {
    // Nothing here asks for you, so the slip stays quiet; its sheet has these.
    return [
      { kind: "ask", label: "Ask Brain about this" },
      { kind: "snooze", label: "Snooze 1 hour" },
      { kind: "dismiss", label: "Not needed anymore", confirm: "Brain stops this Work and won't bring it up again." },
    ];
  }
  return [
    { kind: "ask", label: "Ask Brain about this", primary: true },
    { kind: "dismiss", label: "Not needed anymore", confirm: "Brain stops this Work and won't bring it up again." },
  ];
}

/** The daemon action behind a slip action, when there is one. */
export function brainWorkUserAction(kind: BrainWorkActionKind): BrainWorkUserActionKind | null {
  switch (kind) {
    case "answer":
    case "reply":
      return "reply";
    case "close":
    case "dismiss":
    case "stop":
    case "snooze":
      return kind;
    default:
      return null;
  }
}

/** Snooze's length: the computer ends it on its own clock (SPEC "Snooze 1 hour"). */
export const BRAIN_WORK_SNOOZE_SECONDS = 3600;

/**
 * The actions a slip carries in the list, beside the full set on its page: the
 * answers (or the reply), the way out beside a reply left for days, or the one
 * primary step; nothing destructive otherwise.
 */
export function brainWorkSlipActions(slip: BrainWorkSlip): BrainWorkAction[] {
  const all = brainWorkActions(slip);
  if (slip.question) return all.filter((action) => action.kind === "answer" || action.kind === "reply").slice(0, 3);
  if (brainWorkSlipStale(slip)) return all.filter((action) => action.primary || action.kind === "dismiss").slice(0, 2);
  return all.filter((action) => action.primary).slice(0, 1);
}

/** "Waiting for: …" on a Work's page, when it says something the page does not already. */
export function brainWorkWaitForLine(slip: Pick<BrainWorkSlip, "waitFor" | "calendar" | "summary" | "question">): string | undefined {
  const wait = slip.waitFor?.trim();
  if (!wait || slip.calendar || slip.question || wait === slip.summary || !wait.includes(" ")) return undefined;
  return `Waiting for: ${wait}`;
}

/** The composer text for "Ask Brain about this". */
export function brainWorkAskDraft(slip: Pick<BrainWorkSlip, "title" | "workId" | "summary">): string {
  const quote = slip.summary ? `\n> ${slip.summary}` : "";
  return `Re: ${slip.title} (work ${slip.workId})${quote}\n`;
}
