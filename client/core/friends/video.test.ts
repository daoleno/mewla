import { describe, expect, test } from "bun:test";
import { afterScroll, isPlaying, stopped } from "./video";

describe("the one Friends video playing", () => {
  const feed = { owner: "feed", postId: "p1" };

  test("is one post's in one list", () => {
    expect(isPlaying(feed, "feed", "p1")).toBe(true);
    expect(isPlaying(feed, "feed", "p2")).toBe(false);
    expect(isPlaying(feed, "post-page", "p1")).toBe(false);
    expect(isPlaying(null, "feed", "p1")).toBe(false);
  });

  test("stops when its row leaves the screen, and only its own list's scrolling counts", () => {
    expect(afterScroll(feed, "feed", ["p1", "p2"])).toBe(feed);
    expect(afterScroll(feed, "feed", ["p2", "p3"])).toBeNull();
    expect(afterScroll(feed, "bookmarks", [])).toBe(feed);
  });

  test("stops when its list stops it, and not for another list or post", () => {
    expect(stopped(feed, "feed")).toBeNull();
    expect(stopped(feed, "feed", "p1")).toBeNull();
    expect(stopped(feed, "feed", "p2")).toBe(feed);
    expect(stopped(feed, "bookmarks")).toBe(feed);
  });
});
