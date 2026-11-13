import { describe, expect, test } from "bun:test";
import { calendarLead, calendarStateTone } from "./colour";

describe("a calendar item's tile says who acts (SPEC §10.4)", () => {
  test("something for you is pink", () => {
    expect(calendarLead({ kind: "reminder", recurrence: "none" })).toEqual({ kind: "tile", hue: "pink", glyph: "bell" });
    expect(calendarLead({ kind: "event", recurrence: "weekly" })).toEqual({ kind: "tile", hue: "pink", glyph: "calendar" });
    expect(calendarLead({ kind: "deadline", recurrence: "none" })).toEqual({ kind: "tile", hue: "pink", glyph: "flag" });
  });

  test("a job Brain runs once is violet flash, one it runs again and again is the teal recurring check", () => {
    expect(calendarLead({ kind: "scheduled_action", recurrence: "none" })).toEqual({ kind: "tile", hue: "violet", glyph: "flash" });
    expect(calendarLead({ kind: "scheduled_action", recurrence: "daily" })).toEqual({ kind: "tile", hue: "teal", glyph: "sync" });
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
