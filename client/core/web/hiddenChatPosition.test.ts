import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const UI = join(import.meta.dir, "../../ui");
const read = (path: string) => readFileSync(join(UI, path), "utf8");

// A browser drops a `display: none` list's scroll: Brain came back some twenty screens up after
// every tab switch (FAILURES R4 #14), and a page under a pushed one is hidden the same way. Headless tests keep the scroll, so this guards the wiring;
// the browser replay is in FIXES-ROUND4.md.
describe("a chat under a pushed page keeps its place on the web", () => {
  test("the timeline ignores scrolls while hidden and brings its place back when shown", () => {
    const timeline = read("chat/Timeline.tsx");
    expect(timeline).toMatch(/shown\?: \(\) => boolean;/);
    expect(timeline).toMatch(/if \(!untracked\(shown\)\) return;/);
    expect(timeline).toMatch(/platform\.os === "web" && props\.shown/);
    expect(timeline).toMatch(/target\.atEnd \? list\?\.scrollToEnd\(\{ animated: false \}\) : list\?\.scrollToOffset\(\{ offset: target\.offset, animated: false \}\)/);
  });

  test("Brain and a Session tell their chat when it is on screen", () => {
    expect(read("home/Home.tsx")).toMatch(/<ChatTimeline testID="brain-chat".*shown=\{homeShown\}/);
    expect(read("screens/Session.tsx")).toMatch(/<ChatTimeline testID="session-chat".*shown=\{\(\) => props\.active\(\) && mode\(\) === "chat"\}/);
  });
});
