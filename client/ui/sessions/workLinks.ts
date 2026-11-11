import { computed } from "nsh";
import type { Worker } from "../../core/sessions/workers";
import { sessionWorkLink, sessionWorkLinks, type SessionWorkLink } from "../../core/sessions/workLinks";
import { clock } from "../calendar/clock";
import { calendarItems } from "../calendar/state";
import { workFiles } from "../work/state";

/** The current computer's Work files by the Session each one runs in. */
const links = computed(() => sessionWorkLinks(workFiles()));

/** The Work a Session runs, and the Calendar run that started it (its row title, badge and header line). */
export function workLinkOf(worker: Pick<Worker, "id" | "started_at">): SessionWorkLink | null {
  return sessionWorkLink(links(), worker.id, worker.started_at, calendarItems(), clock());
}
