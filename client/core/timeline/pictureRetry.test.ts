import { describe, expect, test } from "bun:test";
import { PICTURE_RETRY_MS, pictureRetry } from "./pictureRetry";

describe("a chat picture's read again", () => {
  test("with the link up it backs off 1, 2, 4, 8 s, then fails", () => {
    expect([1, 2, 3, 4].map((n) => pictureRetry(n, true))).toEqual(PICTURE_RETRY_MS.map((after) => ({ after })));
    expect(pictureRetry(5, true)).toEqual({ fail: true });
  });

  test("with the link down it waits for the link, however many reads did not answer", () => {
    expect(pictureRetry(1, false)).toEqual({ wait: "link" });
    expect(pictureRetry(9, false)).toEqual({ wait: "link" });
  });
});
