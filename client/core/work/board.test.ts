import { describe, expect, test } from "bun:test";
import { boardColumns } from "./board";

describe("the Work board's layout", () => {
  test("a 1920 window's pane: two cards a row; 2560: the groups side by side; 3440: six cards a row", () => {
    expect(boardColumns(1600 - 800, 3)).toEqual({ side: false, card: 370 });
    expect(boardColumns(2240 - 800, 3).side).toBe(true);
    const ultrawide = boardColumns(3120 - 800, 3);
    expect(ultrawide.side).toBe(false);
    expect(Math.floor((2320 - 48 + 12) / (ultrawide.card + 12))).toBe(6);
  });

  test("one group never stands alone as a column; a narrow board is one card wide", () => {
    expect(boardColumns(1440, 1).side).toBe(false);
    expect(boardColumns(360, 3)).toEqual({ side: false, card: 312 });
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
