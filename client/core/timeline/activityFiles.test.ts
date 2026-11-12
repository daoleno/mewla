import { describe, expect, test } from "bun:test";
import { activityFileRows } from "./activityFiles";
import type { ActivityTimelineItem } from "./activityTypes";

const row = (patch: Partial<ActivityTimelineItem>): ActivityTimelineItem => ({ type: "activity", id: patch.id ?? "r", title: "Edited", tone: "success", icon: "git-compare", ...patch });

describe("edited files in a tool row", () => {
  test("each file shows its +/− once; the paths the patch also lists are not repeated", () => {
    const item = row({
      fileSummaries: [{ path: "src/a.ts", operation: "update", added: 12, removed: 3 }, { path: "src/new.ts", operation: "add", added: 40 }],
      files: ["src/a.ts", "src/new.ts"],
    });
    expect(activityFileRows(item)).toEqual([{ path: "src/a.ts", added: 12, removed: 3 }, { path: "src/new.ts", added: 40 }]);
  });

  test("a renamed file shows and opens its new path, and its old one is not listed again", () => {
    const item = row({ fileSummaries: [{ path: "docs/old.md", movePath: "docs/new.md", operation: "update", added: 1, removed: 1 }], files: ["docs/old.md"] });
    expect(activityFileRows(item)).toEqual([{ path: "docs/new.md", from: "docs/old.md", added: 1, removed: 1 }]);
  });

  test("a count the provider did not report stays unshown", () => {
    expect(activityFileRows(row({ fileSummaries: [{ path: "a", operation: "delete" }] }))).toEqual([{ path: "a" }]);
  });

  test("a folded run lists the files of all its steps, a file edited twice adding up", () => {
    const item = row({
      steps: [
        row({ id: "1", fileSummaries: [{ path: "a.ts", operation: "update", added: 2, removed: 1 }] }),
        row({ id: "2", files: ["README.md"] }),
        row({ id: "3", fileSummaries: [{ path: "a.ts", operation: "update", added: 5 }] }),
      ],
    });
    expect(activityFileRows(item)).toEqual([{ path: "a.ts", added: 7, removed: 1 }, { path: "README.md" }]);
  });
});
