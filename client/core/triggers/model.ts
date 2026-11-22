/**
 * A Trigger as the computer sends it (daemon/trigger View, trimmed by server/triggers.go): a check command on an
 * interval that wakes Brain when its output changes. What its row says is worked out here with the phone's clock,
 * so "checked 2 min ago" and "not handled" stay true between the computer's updates.
 */
export interface TriggerFiring {
  seq: number;
  at: string;
  outputs: number;
  payload: string;
  /** The event that woke Brain; absent while the firing waits to reach Brain. */
  event_id?: string;
  handled_at?: string;
  /** The event is gone from its Work: it can never be handled. */
  lost?: boolean;
}

export interface Trigger {
  id: string;
  title: string;
  check: string;
  cwd?: string;
  interval_seconds: number;
  timeout_seconds: number;
  instruction?: string;
  work_id: string;
  paused?: boolean;
  /** "runaway": it paused itself for waking Brain too often. */
  pause_reason?: string;
  created_at: string;
  updated_at: string;
  revision: number;
  last_checked_at?: string;
  resumed_at?: string;
  failures?: number;
  failing_since?: string;
  last_failure?: string;
  failing?: boolean;
  gap_from?: string;
  gap_to?: string;
  fire_seq?: number;
  /** The newest firings, oldest first. */
  firings?: TriggerFiring[];
  pending_changes?: number;
  state: "watching" | "paused" | "failing";
  next_check_at?: string;
}

/** Why a Trigger needs the person; null when it does not. */
export type TriggerAttention = "failing" | "runaway" | "not_handled" | null;

/** A firing Brain has not picked up after this long needs the person (daemon/trigger NotHandledAfter). */
export const NOT_HANDLED_AFTER_MS = 3 * 3_600_000;
/** The runaway pause's threshold (daemon/trigger WakesPerHour). */
export const WAKES_PER_HOUR = 6;

export function latestFiring(trigger: Pick<Trigger, "firings">): TriggerFiring | undefined {
  const firings = trigger.firings ?? [];
  return firings[firings.length - 1];
}

function open(firing: TriggerFiring | undefined): boolean {
  return Boolean(firing && !firing.handled_at && !firing.lost);
}

export function triggerAttention(trigger: Trigger, now: number): TriggerAttention {
  if (trigger.paused && trigger.pause_reason === "runaway") return "runaway";
  // Paused by the person, a failing check no longer asks for them.
  if (trigger.failing && !trigger.paused) return "failing";
  const latest = latestFiring(trigger);
  if (latest && (latest.lost || (open(latest) && now - Date.parse(latest.at) >= NOT_HANDLED_AFTER_MS))) return "not_handled";
  return null;
}

/** "10 min", "3 h", "2 days". */
export function durationWords(ms: number): string {
  const minutes = Math.floor(ms / 60_000);
  if (ms < 60_000) return `${Math.max(0, Math.floor(ms / 1000))} s`;
  if (minutes < 60) return `${minutes} min`;
  const hours = Math.floor(ms / 3_600_000);
  if (hours < 48) return `${hours} h`;
  return `${Math.floor(ms / 86_400_000)} days`;
}

function ago(ms: number): string {
  return ms < 60_000 ? "just now" : `${durationWords(ms)} ago`;
}

/** How often it checks: "Every 10 min". */
export function everyWords(trigger: Pick<Trigger, "interval_seconds">): string {
  return `Every ${durationWords(trigger.interval_seconds * 1000)}`;
}

/**
 * A failure on one line, in words. The daemon writes its reason ("exit status 1", "timed out after 30s"), then what
 * the check printed to stderr. The check's own last words lead; an exit status is never shown (gate 0.4.21): a check
 * that printed nothing "stopped without saying why".
 */
export function failureLine(failure: string | undefined): string {
  const lines = (failure ?? "").trim().split("\n");
  const reason = (lines[0] ?? "").trim();
  const last = lines.length > 1 ? (lines[lines.length - 1] ?? "").trim() : "";
  const exited = /^exit status -?\d+$/i.test(reason);
  const text = exited ? last || "it stopped without saying why" : last ? `${reason}: ${last}` : reason;
  return text.length > 80 ? `${text.slice(0, 80)}…` : text;
}

/** What the newest firing is doing: "fired 3 h ago · not handled". */
export function firingLine(firing: TriggerFiring, now: number): string {
  return `fired ${ago(now - Date.parse(firing.at))} · ${firingState(firing, now)}`;
}

export function firingState(firing: TriggerFiring, now: number): string {
  if (firing.lost) return "not handled";
  if (firing.handled_at) return "handled";
  if (!firing.event_id) return "waiting to reach Brain";
  return now - Date.parse(firing.at) >= NOT_HANDLED_AFTER_MS ? "not handled" : "with Brain";
}

/** The row's line under the title, the most important fact first. */
export function triggerLine(trigger: Trigger, now: number): string {
  const parts: string[] = [];
  if (trigger.paused && trigger.pause_reason === "runaway") parts.push(`Paused: woke Brain ${WAKES_PER_HOUR} times in an hour`);
  else if (trigger.paused) parts.push("Paused");
  else if (trigger.failing) parts.push(`Failing: ${failureLine(trigger.last_failure)}`);
  else parts.push(everyWords(trigger));
  const latest = latestFiring(trigger);
  if (latest) parts.push(firingLine(latest, now));
  else if (trigger.last_checked_at) parts.push(`checked ${ago(now - Date.parse(trigger.last_checked_at))}`);
  else if (!trigger.paused) parts.push("not checked yet");
  return parts.join(" · ");
}

/** A stretch without checks, when it ended within a day: "Not checked for 2 h: Mewla was not running". */
export function gapLine(trigger: Trigger, now: number): string {
  if (!trigger.gap_from || !trigger.gap_to) return "";
  const to = Date.parse(trigger.gap_to);
  if (now - to >= 86_400_000) return "";
  return `Not checked for ${durationWords(to - Date.parse(trigger.gap_from))}: Mewla was not running`;
}

/** Why it needs the person, in words, for its page and its Needs you row. */
export function attentionWords(trigger: Trigger, now: number): string {
  switch (triggerAttention(trigger, now)) {
    case "runaway": return `It paused itself after waking Brain ${WAKES_PER_HOUR} times in an hour. Its latest change waits until you resume it.`;
    case "failing": return `Its check keeps failing (${trigger.failures ?? 0} in a row): ${failureLine(trigger.last_failure)}`;
    case "not_handled": {
      const latest = latestFiring(trigger)!;
      return latest.lost ? "Its last firing is gone from its Work: Brain never handled it." : `It fired ${durationWords(now - Date.parse(latest.at))} ago and Brain has not handled it.`;
    }
    default: return "";
  }
}

/** The Needs you row's short ask. */
export function attentionAsk(attention: TriggerAttention): string {
  switch (attention) {
    case "runaway": return "Paused itself";
    case "failing": return "Check keeps failing";
    case "not_handled": return "Not handled";
    default: return "";
  }
}

/**
 * Whether only the person can move it on: a check that keeps failing, or a Trigger that paused itself. A firing Brain
 * has not handled is Brain's to handle, never the person's (owner 2026-11-19); its page still says so.
 */
export function triggerNeedsPerson(attention: TriggerAttention): boolean {
  return attention === "failing" || attention === "runaway";
}

/** The Triggers that need the person, as the tray lists them. */
export function triggersNeedingYou(triggers: readonly Trigger[], now: number): { id: string; title: string; ask: string }[] {
  return triggers.flatMap((trigger) => {
    const attention = triggerAttention(trigger, now);
    return triggerNeedsPerson(attention) ? [{ id: trigger.id, title: trigger.title, ask: attentionAsk(attention) }] : [];
  });
}

export function sortTriggers(triggers: readonly Trigger[]): Trigger[] {
  return [...triggers].sort((a, b) => Date.parse(a.created_at) - Date.parse(b.created_at) || a.id.localeCompare(b.id));
}

/** A Trigger on the Triggers page: its group, its one line, and the time it last noticed something today. */
export interface TriggerListRow {
  trigger: Trigger;
  line: string;
  /** Today's last noticing, for the row's end ("10:31"). */
  at?: number;
  needsPerson: boolean;
}

export interface TriggerGroups {
  /** It needs the person (its check keeps failing, or it paused itself). */
  needs: TriggerListRow[];
  /** It noticed something today, newest first. */
  today: TriggerListRow[];
  /** The rest: when each last noticed something, newest first, then those that never have. */
  quiet: TriggerListRow[];
  /** How many are watching (not paused). */
  watching: number;
}

function timesWords(count: number): string {
  return count === 1 ? "Noticed something today" : count === 2 ? "Noticed something twice today" : `Noticed something ${count} times today`;
}

/**
 * The Triggers page's groups (owner 2026-11-22): what needs the person, what noticed something today, what is quiet.
 * `day` is the device's date of an instant; `shortDate` says a day ("Nov 19").
 */
export function triggerGroups(list: readonly Trigger[], now: number, day: (at: number) => string, shortDate: (at: number) => string): TriggerGroups {
  const today = day(now);
  const groups: TriggerGroups = { needs: [], today: [], quiet: [], watching: 0 };
  for (const trigger of sortTriggers(list)) {
    if (!trigger.paused) groups.watching++;
    const attention = triggerAttention(trigger, now);
    const latest = latestFiring(trigger);
    const latestAt = latest ? Date.parse(latest.at) : NaN;
    const todays = (trigger.firings ?? []).filter((firing) => day(Date.parse(firing.at)) === today).length;
    if (triggerNeedsPerson(attention)) {
      groups.needs.push({ trigger, line: attention === "runaway" ? "It paused itself" : "Its check keeps failing", needsPerson: true });
    } else if (trigger.paused) {
      groups.quiet.push({ trigger, line: "Paused", needsPerson: false });
    } else if (todays > 0) {
      groups.today.push({ trigger, line: timesWords(todays), at: latestAt, needsPerson: false });
    } else {
      groups.quiet.push({ trigger, line: latest ? `Last noticed ${shortDate(latestAt)}` : "Nothing noticed yet", at: Number.isFinite(latestAt) ? latestAt : undefined, needsPerson: false });
    }
  }
  groups.today.sort((a, b) => (b.at ?? 0) - (a.at ?? 0));
  groups.quiet.sort((a, b) => (b.at ?? -1) - (a.at ?? -1));
  // The quiet ones' time is for order only: their line says the day.
  for (const row of groups.quiet) row.at = undefined;
  return groups;
}

/**
 * The ask card's detail under its title ("Its check keeps failing"): how long and why, or what pausing itself holds
 * back. `time` says an instant on the device's clock.
 */
export function askDetail(trigger: Trigger, time: (at: number) => string): string {
  if (trigger.paused && trigger.pause_reason === "runaway") return `It woke Brain ${WAKES_PER_HOUR} times in an hour. Its latest change waits until you resume it.`;
  const failures = trigger.failures ?? 0;
  const since = trigger.failing_since ? ` since ${time(Date.parse(trigger.failing_since))}` : "";
  const why = failureLine(trigger.last_failure);
  return `${failures === 1 ? "Once" : `${failures} times in a row`}${since}${why ? `: ${why}` : ""}`;
}
