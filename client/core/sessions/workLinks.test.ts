import { describe, expect, test } from "bun:test";
import { testClock } from "../calendar/testClock";
import type { CalendarItem } from "../calendar/model";
import type { WorkItem } from "../work/workItems";
import { calendarWorkOwnsSession, sessionWorkLink, sessionWorkLinks } from "./workLinks";

const work = (id: string, frontmatter: Partial<WorkItem["frontmatter"]>, title = "Ship the release"): WorkItem => ({
  id, path: `/w/${id}.md`, project: "mewla", title, body: "", mtime: "1",
  frontmatter: { id, created: "2026-10-27T14:00:00Z", ...frontmatter },
});
const run = work("r1", { kind: "calendar_action", worker_session: "w2", done: "2026-10-27T15:00:00Z", extra: { calendar_item_id: "c1", calendar_run_id: "run1" } }, "Dig drafts");
const calendar = [{ id: "c1", title: "运营：挖稿", runs: [{ id: "run1", scheduled_for: "2026-10-27T14:00:00Z" }] } as unknown as CalendarItem];
const clock = testClock("2026-10-28T10:00:00Z", "UTC");

describe("a Session's Work", () => {
  test("open Work names its Session; done Work does not, except a Calendar run's", () => {
    const links = sessionWorkLinks([work("a", { worker_session: "w1" }), work("b", { worker_session: "w3", done: "2026-10-27T15:00:00Z" }), run]);
    expect(links.get("w1")?.id).toBe("a");
    expect(links.has("w3")).toBe(false);
    expect(links.get("w2")?.id).toBe("r1");
  });

  test("a Calendar run's Session shows the schedule and the run's date", () => {
    const link = sessionWorkLink(sessionWorkLinks([run]), "w2", Date.parse("2026-10-27T14:01:00Z"), calendar, clock);
    expect(link).toMatchObject({ workTitle: "Dig drafts", calendarItemId: "c1" });
    expect(link?.calendarLabel).toStartWith("运营：挖稿 · ");
  });

  test("a later Session that reuses the pane id is not the run's", () => {
    expect(calendarWorkOwnsSession(run, Date.parse("2026-10-27T15:00:00Z"))).toBe(false);
    const link = sessionWorkLink(sessionWorkLinks([run]), "w2", Date.parse("2026-10-27T15:00:00Z"), calendar, clock);
    expect(link?.calendarLabel).toBeUndefined();
    expect(link?.workTitle).toBe("Dig drafts");
  });
});
