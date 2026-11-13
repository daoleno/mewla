import { describe, expect, test } from "bun:test";
import { calendarLead, calendarStateTone } from "./colour";

describe("a calendar item's tile says who acts (SPEC §10.4)", () => {
  test("something for you is a bell, a calendar or a flag", () => {
    expect(calendarLead({ kind: "reminder", recurrence: "none" })).toEqual({ kind: "tile", glyph: "bell" });
    expect(calendarLead({ kind: "event", recurrence: "weekly" })).toEqual({ kind: "tile", glyph: "calendar" });
    expect(calendarLead({ kind: "deadline", recurrence: "none" })).toEqual({ kind: "tile", glyph: "flag" });
  });

  test("a job Brain runs once is flash, one it runs again and again is the recurring check, sync", () => {
    expect(calendarLead({ kind: "scheduled_action", recurrence: "none" })).toEqual({ kind: "tile", glyph: "flash" });
    expect(calendarLead({ kind: "scheduled_action", recurrence: "daily" })).toEqual({ kind: "tile", glyph: "sync" });
  });
});

describe("a calendar state", () => {
  test("done ok, failed warn, waiting for you red, the rest off", () => {
    expect(calendarStateTone("completed")).toBe("ok");
    expect(calendarStateTone("failed")).toBe("warn");
    expect(calendarStateTone("waiting")).toBe("error");
    expect(calendarStateTone("scheduled")).toBe("off");
    expect(calendarStateTone("running")).toBe("off");
    expect(calendarStateTone("cancelled")).toBe("off");
  });
});
