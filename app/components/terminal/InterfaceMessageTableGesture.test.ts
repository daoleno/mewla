// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const read = (path: string) => readFileSync(join(import.meta.dir, path), "utf8");

describe("Interface horizontal scroller native gesture arbitration", () => {
  test("the shared gesture is a direction-gated native region, not React state", () => {
    const source = read("../ui/useNestedHorizontalScrollGesture.ts");

    expect(source).toContain("Gesture.Simultaneous(");
    expect(source).toContain("Gesture.Pan()");
    expect(source).toContain(".activeOffsetX([-6, 6])");
    expect(source).toContain(".failOffsetY([-8, 8])");
    expect(source).toContain("Gesture.Native()");
  });

  test("tables and both code block renderers claim their horizontal drags", () => {
    const table = read("InterfaceMessageBlock.tsx");
    expect(table).toContain("<GestureDetector gesture={tableHorizontalGesture}>");
    expect(table).toContain("useNestedHorizontalScrollGesture()");
    expect(table).not.toContain("onTouchStart");
    expect(table).not.toContain("swipeEnabled");

    for (const file of ["InterfaceMessageCodeBlock.tsx", "PreformattedCodeWebView.tsx"]) {
      const source = read(file);
      expect(source).toContain("useNestedHorizontalScrollGesture()");
      expect(source).toContain("<GestureDetector gesture={horizontalScrollGesture}>");
    }
  });
});
