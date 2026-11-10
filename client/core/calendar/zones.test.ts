import { describe, expect, test } from "bun:test";
import { testClock } from "./testClock";
import { Zones } from "./zones";

const { zones, format } = testClock("2026-07-14T10:00:00Z");

describe("a wall-clock time in a zone", () => {
  test("resolves without the phone's zone", () => {
    expect(zones.resolveLocal("2026-07-14", "18:20", "Asia/Shanghai")).toEqual({ status: "resolved", instant: Date.parse("2026-07-14T10:20:00Z") });
  });
  test("is refused in a DST spring-forward gap", () => {
    const result = zones.resolveLocal("2026-03-08", "02:30", "America/New_York");
    expect(result.status).toBe("gap");
    expect(result.status === "gap" && result.message).toContain("does not exist");
  });
  test("gives both instants of a repeated fall-back time", () => {
    const result = zones.resolveLocal("2026-11-01", "01:30", "America/New_York");
    expect(result).toMatchObject({ status: "ambiguous", instants: [Date.parse("2026-11-01T05:30:00Z"), Date.parse("2026-11-01T06:30:00Z")] });
  });
  test("refuses a malformed date or an unknown zone", () => {
    expect(zones.resolveLocal("2026-02-30", "10:00", "UTC").status).toBe("invalid");
    expect(zones.resolveLocal("2026-02-01", "10:00", "Mars/Olympus")).toEqual({ status: "invalid", message: "Enter a valid IANA timezone, such as Asia/Shanghai." });
  });
  test("round-trips the fields of an instant", () => {
    expect(zones.localFields(Date.parse("2026-07-14T10:20:00Z"), "Asia/Shanghai")).toEqual({ date: "2026-07-14", time: "18:20" });
  });
  test("an instant goes to the computer with its zone's offset then, which the computer requires", () => {
    expect(zones.iso(Date.parse("2026-11-12T02:30:00Z"), "Asia/Shanghai")).toBe("2026-11-12T10:30:00+08:00");
    expect(zones.iso(Date.parse("2026-07-14T16:30:00Z"), "America/New_York")).toBe("2026-07-14T12:30:00-04:00");
    expect(zones.iso(Date.parse("2026-12-14T16:30:00Z"), "America/New_York")).toBe("2026-12-14T11:30:00-05:00");
    expect(zones.iso(Date.parse("2026-07-14T16:30:00Z"), "UTC")).toBe("2026-07-14T16:30:00+00:00");
  });
  test("an instant's date depends on the zone it is read in", () => {
    const at = Date.parse("2026-07-14T16:30:00Z");
    expect(zones.dateKey(at, "Asia/Shanghai")).toBe("2026-07-15");
    expect(zones.dateKey(at, "America/Los_Angeles")).toBe("2026-07-14");
  });
});

describe("dates in English", () => {
  const at = Date.parse("2026-10-27T12:30:00Z");
  test("in the item's zone, its name added when asked", () => {
    expect(format.time(at, "Asia/Shanghai")).toBe("8:30 PM");
    expect(format.time(at, "Asia/Shanghai", true)).toBe("8:30 PM GMT+8");
    expect(format.day(at, "Asia/Shanghai")).toBe("Tue, Oct 27");
    expect(format.longDay(at, "America/New_York")).toBe("Tuesday, October 27");
    expect(format.dateTime(Date.parse("2026-07-14T15:30:00Z"), "America/New_York", true)).toBe("Jul 14, 11:30 AM EDT");
    expect(format.full(at, "Asia/Shanghai")).toBe("Tuesday, October 27, 2026, 8:30 PM GMT+8");
  });
  test("in 24-hour style when the phone shows times so", () => {
    expect(testClock(at, "UTC", [], false).format.time(at, "Asia/Shanghai")).toBe("20:30");
  });
});

describe("the host's zone table", () => {
  test("is read span by span: the offset in force at an instant", () => {
    const z = new Zones({
      device: "Europe/Berlin",
      hour12: false,
      zones: {
        "Europe/Berlin": [
          { atMs: 0, offsetMinutes: 60, name: "GMT+1" },
          { atMs: Date.parse("2026-03-29T01:00:00Z"), offsetMinutes: 120, name: "GMT+2" },
        ],
        "Nowhere/Gone": null,
      },
    });
    expect(z.offsetMinutes("Europe/Berlin", Date.parse("2026-03-29T00:59:00Z"))).toBe(60);
    expect(z.name("Europe/Berlin", Date.parse("2026-03-29T01:00:00Z"))).toBe("GMT+2");
    expect(z.resolveLocal("2026-03-29", "02:30", "Europe/Berlin").status).toBe("gap");
    expect(z.knows("Nowhere/Gone")).toBe(false);
    expect(z.knows("UTC")).toBe(true);
  });
});
