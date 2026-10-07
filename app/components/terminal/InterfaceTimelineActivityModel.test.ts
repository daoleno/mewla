import { describe, expect, test } from "bun:test";
import { splitActivityStatusDetail } from "./InterfaceTimelineActivityModel";

describe("tool row status words become StatusMarks", () => {
  test("maps each status word to its glyph and keeps the rest as copy", () => {
    expect(splitActivityStatusDetail("Running · 4s", "running")).toEqual({ mark: "running", rest: "4s" });
    expect(splitActivityStatusDetail("Waiting", "running")).toEqual({ mark: "running", rest: undefined });
    expect(splitActivityStatusDetail("Succeeded · 2.5s", "success")).toEqual({ mark: "ready", rest: "2.5s" });
    expect(splitActivityStatusDetail("Passed", "success")).toEqual({ mark: "ready", rest: undefined });
    expect(splitActivityStatusDetail("Blocked", "neutral")).toEqual({ mark: "blocked", rest: undefined });
  });

  test("a failure keeps its cause next to the crossed box", () => {
    expect(splitActivityStatusDetail("Failed · exit 1", "failed")).toEqual({ mark: "failed", rest: "exit 1" });
    expect(splitActivityStatusDetail("Failed · exit 2 · 1m 4s", "failed")).toEqual({
      mark: "failed",
      rest: "exit 2 · 1m 4s",
    });
  });

  test("Done and Finished only say the call returned: no mark, no word", () => {
    expect(splitActivityStatusDetail("Done", "success")).toEqual({ mark: null, rest: undefined });
    expect(splitActivityStatusDetail("Finished · 3s", "neutral")).toEqual({ mark: null, rest: "3s" });
  });

  test("other details stay whole; running and failed rows still get a mark", () => {
    expect(splitActivityStatusDetail("+7 -7", "success")).toEqual({ mark: null, rest: "+7 -7" });
    expect(splitActivityStatusDetail("app/strings.ts", "running")).toEqual({ mark: "running", rest: "app/strings.ts" });
    expect(splitActivityStatusDetail(undefined, "failed")).toEqual({ mark: "failed" });
    expect(splitActivityStatusDetail("  ", "success")).toEqual({ mark: null });
  });
});
