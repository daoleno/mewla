import { calendarOriginOfWork, originLabel, type CalendarClock, type CalendarItem } from "../calendar/model";
import type { WorkItem } from "../work/workItems";

/** A Calendar run's Session starts right after its Work is written. */
const SESSION_START_WINDOW_MS = 10 * 60 * 1000;

/**
 * Whether a Session is the one a Calendar run's Work started. Pane ids restart
 * when tmux does, so a later, unrelated Session can carry a finished run's id;
 * it started well after that Work was written. (Expo: calendarWorkOwnsSession.)
 */
export function calendarWorkOwnsSession(work: { frontmatter?: { created?: string } }, sessionStartedAt: number | undefined): boolean {
  const created = Date.parse(work.frontmatter?.created ?? "");
  if (!Number.isFinite(created) || !sessionStartedAt) return true;
  return sessionStartedAt <= created + SESSION_START_WINDOW_MS;
}

export type SessionWorkLink = {
  /** The Work file that runs in this Session. */
  work: WorkItem;
  workTitle?: string;
  /** "Schedule · Oct 27" when a Calendar run started it. */
  calendarLabel?: string;
  calendarItemId?: string;
};

/**
 * The Work each Session runs, by Session id: the Work files naming it as their
 * `worker_session`, open ones only, except a Calendar run's, which keeps naming
 * its Session after it is done (that is where the Session came from).
 */
export function sessionWorkLinks(work: readonly WorkItem[]): Map<string, WorkItem> {
  const links = new Map<string, WorkItem>();
  for (const item of work) {
    const finished = Boolean(item.frontmatter.done) && item.frontmatter.kind !== "calendar_action";
    const session = item.frontmatter.worker_session?.trim();
    if (!finished && session) links.set(session, item);
  }
  return links;
}

/** What a Session's row and header show of its Work and Calendar origin. */
export function sessionWorkLink(
  links: Map<string, WorkItem>,
  workerId: string,
  startedAt: number | undefined,
  calendar: readonly CalendarItem[],
  clock: CalendarClock,
): SessionWorkLink | null {
  const work = links.get(workerId);
  if (!work) return null;
  const origin = calendarWorkOwnsSession(work, startedAt) ? calendarOriginOfWork(work) : null;
  const calendarLabel = origin ? originLabel(origin, calendar, clock, { title: work.frontmatter.title, at: work.frontmatter.created }) ?? undefined : undefined;
  return { work, workTitle: work.title?.trim() || undefined, calendarLabel, calendarItemId: calendarLabel ? origin?.itemId : undefined };
}
