import { describe, expect, test } from "bun:test";
import type { WorkerDirectorySection } from "../../services/workerDirectory";
import {
  MAX_REMEMBERED_COLLAPSED_SECTIONS,
  MAX_ANIMATED_NEW_ROWS,
  buildSessionListSections,
  collapsedSectionsToStore,
  newRowKeys,
  sectionAttentionMark,
  toggleCollapsedSection,
} from "./sessionSections";

const worker = (status: string, needs = false) =>
  ({ status, needs_attention: needs }) as never;
const section = (key: string, statuses: [string, boolean?][]) =>
  ({
    key,
    title: key,
    subtitle: "",
    data: statuses.map(([status, needs]) => worker(status, needs)),
  }) as WorkerDirectorySection;

describe("a folded directory's mark", () => {
  test("a Session waiting on you outranks a failure, which outranks running work", () => {
    expect(sectionAttentionMark([worker("running"), worker("done", true)])).toBe("needs");
    expect(sectionAttentionMark([worker("running"), worker("failed")])).toBe("failed");
    expect(sectionAttentionMark([worker("done"), worker("running")])).toBe("running");
  });
  test("finished, blocked and idle Sessions leave it quiet", () => {
    expect(sectionAttentionMark([worker("done"), worker("blocked"), worker("unknown")])).toBeNull();
    expect(sectionAttentionMark([])).toBeNull();
  });
});

describe("the list's sections", () => {
  const sections = [
    section("s1::cwd::/a", [["running"], ["done"]]),
    section("s1::cwd::/b", [["failed"]]),
  ];
  test("a folded section keeps its count and mark but not its rows", () => {
    const out = buildSessionListSections(sections, new Set(["s1::cwd::/a"]), false);
    expect(out[0]).toMatchObject({ collapsed: true, count: 2, mark: "running" });
    expect(out[0].data).toHaveLength(0);
    expect(out[1]).toMatchObject({ collapsed: false, count: 1, mark: null });
    expect(out[1].data).toHaveLength(1);
  });
  test("selecting shows every row, and a single directory cannot fold", () => {
    const folded = new Set(["s1::cwd::/a", "s1::cwd::/b"]);
    expect(buildSessionListSections(sections, folded, true).every((entry) => !entry.collapsed)).toBe(true);
    expect(buildSessionListSections([sections[0]], folded, false)[0].collapsed).toBe(false);
  });
  test("a fold remembered for another server hides nothing here", () => {
    const out = buildSessionListSections(sections, new Set(["s2::cwd::/a"]), false);
    expect(out.every((entry) => !entry.collapsed)).toBe(true);
  });
});

describe("remembering folds", () => {
  test("toggles without mutating, and caps what is stored", () => {
    const empty = new Set<string>();
    const one = toggleCollapsedSection(empty, "a");
    expect([...one]).toEqual(["a"]);
    expect(empty.size).toBe(0);
    expect(toggleCollapsedSection(one, "a").size).toBe(0);
    const many = new Set(Array.from({ length: MAX_REMEMBERED_COLLAPSED_SECTIONS + 5 }, (_, i) => `k${i}`));
    const stored = collapsedSectionsToStore(many);
    expect(stored).toHaveLength(MAX_REMEMBERED_COLLAPSED_SECTIONS);
    expect(stored.at(-1)).toBe(`k${MAX_REMEMBERED_COLLAPSED_SECTIONS + 4}`);
  });
});

describe("rows new to the list", () => {
  const rows = (...keys: string[]) => [{ data: keys.map((key) => ({ key })) as never }];
  test("the first list and an empty one animate nothing", () => {
    expect(newRowKeys(null, rows("a", "b")).size).toBe(0);
    expect(newRowKeys(new Set(), rows("a", "b")).size).toBe(0);
  });
  test("a row that arrives later is new; rows already known are not", () => {
    expect([...newRowKeys(new Set(["a", "b"]), rows("a", "b", "c"))]).toEqual(["c"]);
    expect(newRowKeys(new Set(["a", "b"]), rows("b", "a")).size).toBe(0);
  });
  test("a whole new set (another server) is a reload, not news", () => {
    const many = Array.from({ length: MAX_ANIMATED_NEW_ROWS + 1 }, (_, index) => `n${index}`);
    expect(newRowKeys(new Set(["a"]), rows(...many)).size).toBe(0);
  });
});
