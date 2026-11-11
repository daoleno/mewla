import { describe, expect, test } from "bun:test";
import { formatGitAge, spokenGitAge } from "./gitReviewTime";

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
