import { describe, expect, test } from "bun:test";
import { clipboardRead, pasteRefused } from "./pairFlow";

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

describe("a clipboard read: text, empty or refused (iOS Deny, Mac re-run 11-14 6c)", () => {
  test("no text from a clipboard that holds some is a refusal", () => {
    expect(clipboardRead({ text: "", has: true })).toEqual({ kind: "refused" });
  });

  test("an empty clipboard, or only spaces, is empty", () => {
    expect(clipboardRead({ text: "", has: false })).toEqual({ kind: "empty" });
    expect(clipboardRead({ text: "  \n", has: true })).toEqual({ kind: "empty" });
  });

  test("text is trimmed; an error is a refusal with its words", () => {
    expect(clipboardRead({ text: " mewla://settings?p=x \n", has: true })).toEqual({ kind: "text", text: "mewla://settings?p=x" });
    const error = new Error("Operation not authorized.");
    expect(clipboardRead({ error })).toEqual({ kind: "refused", error });
  });
});
