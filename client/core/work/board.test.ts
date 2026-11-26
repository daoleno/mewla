import { describe, expect, test } from "bun:test";
import { boardLayout, BOARD_GAP, BOARD_PADDING } from "./board";

const groups = (layout: ReturnType<typeof boardLayout>) => layout.columns.map((c) => c.groups);
const tracks = (layout: ReturnType<typeof boardLayout>) => layout.columns.map((c) => c.tracks);
/** What the columns and their gaps take of the pane: all of it but the padding and what rounding leaves. */
const filled = (layout: ReturnType<typeof boardLayout>, width: number) =>
  layout.columns.reduce((sum, c) => sum + c.width, 0) + (layout.columns.length - 1) * BOARD_GAP + 2 * BOARD_PADDING > width - layout.columns.length * 2 - 4;

// Brain's Work groups (Needs you, Running, Back, Waiting, Queued) with invented counts: one long group, and a desk with every group.
const evening = [0, 13, 3, 4, 0];
const desk = [3, 12, 1, 3, 0];

describe("the Work board's layout", () => {
  test("an ultrawide's pane: the long column takes the spare tracks, so the columns end near one line", () => {
    const width = 3440 - 320 - 880;
    const layout = boardLayout(width, evening);
    expect(groups(layout)).toEqual([[1], [2], [3]]);
    expect(tracks(layout)).toEqual([4, 1, 1]);
    expect(layout.cardWidth).toBeGreaterThanOrEqual(300);
    expect(filled(layout, width)).toBe(true);
    expect(tracks(boardLayout(width, desk))).toEqual([1, 3, 1, 1]);
  });

  test("2560 and 1920: fewer tracks, the same rule; with fewer tracks than groups, later groups go under the lighter column", () => {
    expect(tracks(boardLayout(2560 - 320 - 880, evening))).toEqual([2, 1, 1]);
    const narrow = boardLayout(1920 - 320 - 800, desk);
    expect(groups(narrow)).toEqual([[0, 2, 3], [1]]);
    expect(narrow.cardWidth).toBe(368);
  });

  test("tracks nothing needs widen the cards; empty groups take no column", () => {
    // Two Works, one group: two cards a row across the whole pane, never two small cards in a corner.
    const lone = boardLayout(3440 - 320 - 880, [0, 2, 0, 0, 0]);
    expect(tracks(lone)).toEqual([2]);
    expect(filled(lone, 3440 - 320 - 880)).toBe(true);
    const width = 3440 - 320 - 880;
    const two = boardLayout(width, [0, 1, 0, 1, 0]);
    expect(tracks(two)).toEqual([1, 1]);
    expect(filled(two, width)).toBe(true);
    expect(groups(boardLayout(2000, [0, 0, 0, 0, 0]))).toEqual([[]]);
    expect(groups(boardLayout(300, desk))).toEqual([[0, 1, 2, 3]]);
  });
});

describe("what a Work asks, as the tray shows it", () => {
  test("a request naming the Work or one of its Sessions puts it under Needs you", async () => {
    const { workRequest } = await import("./board");
    const slip = { workId: "k1", sessionId: "s1", workerSessions: [{ session_id: "s2" }] };
    expect(workRequest(slip, [{ workId: "k1", title: "Which one?" }])?.title).toBe("Which one?");
    expect(workRequest(slip, [{ workerId: "s2", title: "Run git push?" }])?.title).toBe("Run git push?");
    expect(workRequest(slip, [{ workerId: "s9", title: "Other" }, { workId: "k2", title: "Other" }])).toBeUndefined();
  });
});
