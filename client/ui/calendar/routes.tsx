import { createNavStack } from "nsh/base";
import { WorkFilePage } from "../work/WorkFilePage";
import { CalendarPage } from "./CalendarPage";
import { ItemPage, RunPage } from "./ItemPage";

/** The Calendar tab's pages: the agenda, an item, one of its runs, the Work file a run wrote. */
export type CalendarRoute =
  | { name: "calendar" }
  | { name: "item"; id: string }
  | { name: "run"; itemId: string; runId: string }
  | { name: "workFile"; id: string };

export const calendarNav = createNavStack<CalendarRoute>([{ name: "calendar" }]);

/** Where the Calendar leads outside its tab; the notifications module sets it at start. */
export const calendarLinks = { openSession: (_sessionId: string) => {} };

export function calendarRoute(route: CalendarRoute) {
  switch (route.name) {
    case "calendar": return <CalendarPage nav={calendarNav} />;
    case "item": return <ItemPage id={route.id} nav={calendarNav} />;
    case "run": return <RunPage itemId={route.itemId} runId={route.runId} nav={calendarNav} openSession={(id) => calendarLinks.openSession(id)} />;
    case "workFile": return <WorkFilePage id={route.id} onClose={() => calendarNav.pop()} openCalendarItem={(id) => calendarNav.push({ name: "item", id })} />;
  }
}

/** Opens an item from anywhere (a notification, a Work's link): the agenda under it, so back leads there. */
export function showCalendarItem(id: string): void {
  calendarNav.setRoutes([{ name: "calendar" }, { name: "item", id }]);
}
