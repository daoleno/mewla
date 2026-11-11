import type { NavStackState } from "nsh/base";
import { WorkFilePage } from "./WorkFilePage";
import { WorkDetail, WorkPage, type WorkLinks, type WorkRoute } from "./WorkPage";

export type { WorkRoute } from "./WorkPage";
/** The Work pages on Brain's stack, and a Work file there (a Session's linked Work, opened from its menu). */
export type BrainWorkRoute = WorkRoute | { name: "workFile"; id: string };

/** Where the Work pages lead outside themselves; the notifications module sets it at start. */
export const workLinks: WorkLinks = { openSession: () => {}, openCalendarItem: () => {}, askBrain: () => {} };

export function workRoute(route: BrainWorkRoute, nav: NavStackState<BrainWorkRoute | { name: "brain" }>) {
  switch (route.name) {
    case "work": return <WorkPage nav={nav} links={workLinks} />;
    case "workDetail": return <WorkDetail workId={route.workId} nav={nav} links={workLinks} />;
    case "workFile": return <WorkFilePage id={route.id} onClose={() => nav.pop()} openCalendarItem={(id, runId) => workLinks.openCalendarItem(id, runId)} />;
  }
}
