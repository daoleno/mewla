import type { NavStackState } from "nsh/base";
import { WorkFilePage } from "../work/WorkFilePage";
import type { Route } from "../shell/routes";
import { openCalendarItem, openSession } from "../shell/nav";
import { CalendarPage } from "./CalendarPage";
import { ItemPage, RunPage } from "./ItemPage";

/** The Calendar's pages on the one stack: the agenda, an item, one of its runs, the Work file a run wrote. */
export type CalendarRoute =
  | { name: "calendar" }
  | { name: "item"; id: string }
  | { name: "run"; itemId: string; runId: string }
  | { name: "workFile"; id: string };

export function calendarRoute(route: CalendarRoute, nav: NavStackState<Route>) {
  switch (route.name) {
    case "calendar": return <CalendarPage nav={nav} openSession={(id) => openSession(id)} />;
    case "item": return <ItemPage id={route.id} nav={nav} openSession={(id) => openSession(id)} />;
    case "run": return <RunPage itemId={route.itemId} runId={route.runId} nav={nav} openSession={(id) => openSession(id)} />;
    case "workFile": return <WorkFilePage id={route.id} onClose={() => nav.pop()} openCalendarItem={(id, runId) => openCalendarItem(id, runId)} />;
  }
}
