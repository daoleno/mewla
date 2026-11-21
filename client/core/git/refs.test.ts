import { describe, expect, test } from "bun:test";
import { middleEllipsis, parseCommitRefs } from "./refs";

describe("a commit's refs as chips", () => {
  test("HEAD is its own marker, the branch a local chip, a tag a chip and the remote branch a quieter one", () => {
    const refs = parseCommitRefs(["HEAD -> feat/globalresearch-source-eu", "origin/feat/globalresearch-source-eu", "tag: v1.2.0", "origin/HEAD"]);
    expect(refs.head).toBe(true);
    expect(refs.chips.map((c) => [c.kind, c.name])).toEqual([["local", "feat/globalresearch-source-eu"], ["tag", "v1.2.0"], ["remote", "origin/feat/globalresearch-source-eu"]]);
  });

  test("a detached HEAD is a marker with no chip; a local branch with a slash is not a remote", () => {
    expect(parseCommitRefs(["HEAD"])).toEqual({ head: true, chips: [], more: 0 });
    expect(parseCommitRefs(["feat/x"]).chips[0]!.kind).toBe("local");
    expect(parseCommitRefs(["fork/x"], { upstream: "fork/main" }).chips[0]!.kind).toBe("remote");
  });

  test("a long name keeps its start and its end; a short one is whole", () => {
    const label = middleEllipsis("feat/globalresearch-source-eu-ingest", 22);
    expect([...label].length).toBe(22);
    expect(label.startsWith("feat/globalr")).toBe(true);
    expect(label.endsWith("-ingest")).toBe(true);
    expect(label).toContain("…");
    expect(middleEllipsis("main", 22)).toBe("main");
  });

  test("past the limit the rest is counted, not drawn", () => {
    const refs = parseCommitRefs(["a", "b", "c", "d", "e", "f", "g", "h"], { limit: 6 });
    expect(refs.chips).toHaveLength(6);
    expect(refs.more).toBe(2);
  });
});
