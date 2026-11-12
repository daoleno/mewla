import { describe, expect, test } from "bun:test";
import { appendToDraft } from "./draftText";

describe("text written into a draft from elsewhere (Ask Brain about this)", () => {
  test("an empty draft becomes the text", () => {
    expect(appendToDraft("", "> Work: fix-sync\n\n")).toBe("> Work: fix-sync\n\n");
    expect(appendToDraft("  \n", "quote")).toBe("quote");
  });

  test("what the person typed stays, the text goes below it after a blank line", () => {
    expect(appendToDraft("also check the logs\n", "> Work: fix-sync")).toBe("also check the logs\n\n> Work: fix-sync");
  });

  test("the same text twice is added once", () => {
    const once = appendToDraft("hi", "> Work: fix-sync\n");
    expect(appendToDraft(once, "> Work: fix-sync\n")).toBe(once);
  });
});
