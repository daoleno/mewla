import { describe, expect, test } from "bun:test";
import { DateStyle, formatDate, formatNumber } from "./dateFormat";

const at = Date.UTC(2026, 9, 27, 20, 5);
const utc = { timeZone: "UTC" };

describe("formatDate", () => {
  test("reads in English whatever the device language", () => {
    expect(formatDate(at, DateStyle.day, utc)).toBe("Tue, Oct 27");
    expect(formatDate(at, DateStyle.time, utc)).toBe("8:05 PM");
    expect(formatDate(at, DateStyle.dateTime, utc)).toBe("Oct 27, 8:05 PM");
    expect(formatDate(at, DateStyle.monthYear, utc)).toBe("October 2026");
    expect(formatDate(at, DateStyle.longDay, utc)).toBe("Tuesday, October 27");
  });

  test("an invalid date formats as nothing", () => {
    expect(formatDate("not a date")).toBe("");
    expect(formatDate(Number.NaN)).toBe("");
  });

  test("numbers group the English way", () => {
    expect(formatNumber(1234.5, { style: "currency", currency: "USD" })).toBe("$1,234.50");
  });
});

describe("no device-locale formatting", () => {
  test("app code formats dates only through dateFormat", async () => {
    const { Glob } = await import("bun");
    const offenders: string[] = [];
    const pattern = /toLocale(Date|Time)?String\(\s*(undefined|\[\]|\))|DateTimeFormat\(\s*(undefined|\[\])/;
    for (const dir of ["app", "components", "constants", "services", "store"]) {
      for await (const file of new Glob(`${dir}/**/*.{ts,tsx}`).scan(".")) {
        if (/\.test\.tsx?$/.test(file)) continue;
        const source = await Bun.file(file).text();
        if (pattern.test(source)) offenders.push(file);
      }
    }
    expect(offenders).toEqual([]);
  });
});
