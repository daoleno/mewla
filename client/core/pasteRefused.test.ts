import { describe, expect, test } from "bun:test";
import { pasteRefused } from "./pairFlow";

describe("a refused paste says it was refused (iPhone, 11-14)", () => {
  test("with the system's words, never 'Nothing to paste'", () => {
    const problem = pasteRefused(new Error("Operation not authorized."));
    expect(problem.title).toBe("Paste wasn't allowed");
    expect(problem.detail).toBe("Operation not authorized. Tap Paste again and allow it, or type the link.");
  });

  test("without words of its own", () => {
    expect(pasteRefused("nope").detail).toBe("Tap Paste again and allow it, or type the link.");
  });
});
