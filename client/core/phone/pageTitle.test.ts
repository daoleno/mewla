import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

describe("the Phone page's title (phone 15)", () => {
  // In a centred row an unshrinkable title wider than the screen was cut at the left edge
  // ("honebot2 (mh-…"); the float's title ends in "…" on the right. No layout query in nsh test.
  test("shrinks in its row and keeps one line, ending in an ellipsis", () => {
    const page = readFileSync(join(import.meta.dir, "../../ui/phone/PhonePage.tsx"), "utf8");
    const title = page.match(/<Text testID="phone-live-title" class="([^"]*)"/)?.[1] ?? "";
    expect(title.split(" ")).toEqual(expect.arrayContaining(["min-w-0", "shrink", "line-clamp-1"]));
  });
});
