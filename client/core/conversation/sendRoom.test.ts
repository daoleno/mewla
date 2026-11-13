import { describe, expect, test } from "bun:test";
import { fromNewest, roomLeft, sentRowIndex } from "./sendRoom";

const at = (ms: number) => new Date(ms).toISOString();
const SENT = 1_800_000_000_000;

describe("the row a send made", () => {
  test("is the newest pending message of the person's made at the send", () => {
    const rows = [
      { type: "message", role: "user", timestamp: at(SENT - 60_000) },
      { type: "message", role: "assistant", timestamp: at(SENT - 50_000) },
      { type: "message", role: "user", pending: true, timestamp: at(SENT + 3) },
      { type: "activity" },
    ];
    expect(sentRowIndex(rows, SENT)).toBe(2);
  });

  test("not yet in the rows: none, never the previous message (which put Brain near its end)", () => {
    const rows = [
      { type: "message", role: "user", timestamp: at(SENT - 60_000) },
      { type: "message", role: "assistant", timestamp: at(SENT - 50_000) },
    ];
    expect(sentRowIndex(rows, SENT)).toBe(-1);
  });

  test("an older message that failed to go out is not it", () => {
    const rows = [{ type: "message", role: "user", pending: true, timestamp: at(SENT - 600_000) }];
    expect(sentRowIndex(rows, SENT)).toBe(-1);
  });
});

describe("the room under the sent message", () => {
  test("shrinks as the reply grows into it, and is gone once the reply fills the list", () => {
    expect(roomLeft(700, 800, 300)).toBe(500);
    expect(roomLeft(500, 800, 650)).toBe(150);
    expect(roomLeft(150, 800, 900)).toBe(0);
  });

  test("never grows back when the person scrolls down into it", () => {
    expect(roomLeft(150, 800, 100)).toBe(150);
  });

  test("is not counted as unread: at the reply's end the list is at its newest", () => {
    // 2000 of content of which 600 is room; the viewport (700) ends where the reply does.
    expect(fromNewest(2000, 600, 700, 700)).toBe(0);
  });
});
