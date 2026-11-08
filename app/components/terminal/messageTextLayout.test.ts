import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

import { MESSAGE_TEXT_LAYOUT } from "./messageTextLayout";

// Native text measurement is not available under bun, so this pins the one
// prop that keeps the measured height equal to the drawn height of a
// shrink-wrapped sent bubble (see messageTextLayout.ts).
describe("message text line breaking", () => {
  test("breaks greedily so a bubble that shrinks to its widest line keeps its lines", () => {
    expect(MESSAGE_TEXT_LAYOUT.textBreakStrategy).toBe("simple");
  });

  test("every Text a message body draws carries it", () => {
    const source = readFileSync(
      join(import.meta.dir, "InterfaceMessageBlock.tsx"),
      "utf8",
    );
    const texts = source.match(/<Text\b[^>]*>/g) ?? [];
    expect(texts.length).toBeGreaterThan(0);
    for (const open of texts) {
      expect(open).toContain("{...MESSAGE_TEXT_LAYOUT}");
    }
  });
});
