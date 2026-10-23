import { afterEach, describe, expect, spyOn, test } from "bun:test";
import { failureMessage, resetWarnOnce, warnOnce } from "./warnOnce";

afterEach(() => resetWarnOnce());

describe("warnOnce", () => {
  test("logs the same failure for a scope once", () => {
    const warn = spyOn(console, "warn").mockImplementation(() => {});
    expect(warnOnce("stats", new Error("down"))).toBe(true);
    expect(warnOnce("stats", new Error("down"))).toBe(false);
    expect(warnOnce("stats", new Error("other"))).toBe(true);
    expect(warnOnce("calendar", new Error("down"))).toBe(true);
    expect(warn).toHaveBeenCalledTimes(3);
    expect(warn.mock.calls[0]).toEqual(["[stats] down"]);
    warn.mockRestore();
  });

  test("failureMessage reads errors and strings and never returns empty", () => {
    expect(failureMessage(new Error("x"))).toBe("x");
    expect(failureMessage("y")).toBe("y");
    expect(failureMessage(new Error(""))).toBe("The computer did not answer.");
    expect(failureMessage(undefined, "fallback")).toBe("fallback");
  });
});
