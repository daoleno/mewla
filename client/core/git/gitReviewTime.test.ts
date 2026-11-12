import { describe, expect, test } from "bun:test";
import { formatGitAge, gitCommitDays, spokenGitAge } from "./gitReviewTime";

const now = Date.UTC(2026, 10, 11, 12);
const ago = (seconds: number) => now / 1000 - seconds;

describe("a commit's age", () => {
  test("short on the row, in words for a screen reader (never \"now ago\")", () => {
    expect(formatGitAge(ago(10), now)).toBe("now");
    expect(spokenGitAge(ago(10), now)).toBe("just now");
    expect(spokenGitAge(ago(60), now)).toBe("1 minute ago");
    expect(spokenGitAge(ago(5 * 60), now)).toBe("5 minutes ago");
    expect(spokenGitAge(ago(3 * 3600), now)).toBe("3 hours ago");
    expect(spokenGitAge(ago(21 * 86400), now)).toBe("3 weeks ago");
    expect(spokenGitAge(ago(90 * 86400), now)).toBe("3 months ago");
    expect(spokenGitAge(ago(800 * 86400), now)).toBe("2 years ago");
  });
});

describe("history by day", () => {
  test("groups commits by their local day, newest first, naming today and yesterday", () => {
    const local = new Date(2026, 10, 11, 15).getTime();
    const at = (y: number, m: number, d: number, h: number) => new Date(y, m, d, h).getTime() / 1000;
    const commits = [{ sha: "a", date: at(2026, 10, 11, 14) }, { sha: "b", date: at(2026, 10, 11, 9) }, { sha: "c", date: at(2026, 10, 10, 20) }, { sha: "d", date: at(2026, 10, 2, 8) }, { sha: "e", date: at(2025, 11, 30, 8) }];
    const days = gitCommitDays(commits, local);
    expect(days.map((d) => d.label)).toEqual(["Today", "Yesterday", "Nov 2", "Dec 30, 2025"]);
    expect(days.map((d) => d.commits.map((c) => c.sha))).toEqual([["a", "b"], ["c"], ["d"], ["e"]]);
  });
});
