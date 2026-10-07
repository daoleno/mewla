import type { BrainWorkUserActionKind } from "../../store/brain";
import type { BrainWorkSlip } from "./brainWorkSurface";

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
 * - snooze: hide it from Needs you until tomorrow morning
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
    const choices = (slip.choices ?? []).map<BrainWorkAction>((choice, index) => ({
      kind: "answer",
      label: choice,
      text: choice,
      primary: index === 0,
    }));
    return [
      ...choices,
      { kind: "reply", label: choices.length ? "Something else…" : "Reply", primary: choices.length === 0 },
      { kind: "snooze", label: "Snooze" },
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
      { kind: "snooze", label: "Snooze" },
      { kind: "dismiss", label: "Not needed anymore", confirm: "Brain stops this Work and won't bring it up again." },
    ];
  }
  if (slip.group === "running") {
    const open: BrainWorkAction[] = slip.sessionId ? [{ kind: "open", label: "Open Worker", primary: true }] : [];
    const stop: BrainWorkAction[] = slip.sessionId
      ? [{ kind: "stop", label: "Stop", confirm: "The Worker is closed and its Work cancelled. Unsaved work in it is lost." }]
      : [];
    return [...open, { kind: "ask", label: "Ask Brain about this" }, ...stop];
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

/** Tomorrow at 09:00 local, the snooze target. */
export function brainWorkSnoozeUntil(now: Date = new Date()): Date {
  const next = new Date(now);
  next.setDate(next.getDate() + 1);
  next.setHours(9, 0, 0, 0);
  return next;
}

/** The composer text for "Ask Brain about this". */
export function brainWorkAskDraft(slip: Pick<BrainWorkSlip, "title" | "workId" | "summary">): string {
  const quote = slip.summary ? `\n> ${slip.summary}` : "";
  return `Re: ${slip.title} (work ${slip.workId})${quote}\n`;
}
