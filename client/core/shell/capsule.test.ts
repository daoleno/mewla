import { describe, expect, test } from "bun:test";
import { CAPSULE, CAPSULE_OPEN, capsuleDragStart, capsuleExpand, capsuleInset, capsuleRest, capsuleScroll, capsuleTitleIsLong } from "./capsule";

const read = (from: number, ...offsets: number[]) => offsets.reduce(capsuleScroll, capsuleDragStart(CAPSULE_OPEN, from));

describe("the capsule collapses while the person reads", () => {
  test("after the threshold, either way, from where their drag began", () => {
    expect(read(500, 500 - CAPSULE.collapseAfter + 1).collapsed).toBe(false);
    expect(read(500, 500 - CAPSULE.collapseAfter).collapsed).toBe(true);
    expect(read(500, 500 + CAPSULE.collapseAfter).collapsed).toBe(true);
  });

  test("never from a scroll the person did not start (a new message the list follows, a jump)", () => {
    expect([900, 1400, 2000].reduce(capsuleScroll, CAPSULE_OPEN).collapsed).toBe(false);
    // Nor after their own scroll came to rest.
    const rested = capsuleRest(read(500, 300), false);
    expect([1200, 2000].reduce(capsuleScroll, rested)).toEqual(rested);
  });

  test("expands when their scroll comes to rest at the newest end; stays collapsed resting elsewhere", () => {
    expect(capsuleRest(read(500, 900), true).collapsed).toBe(false);
    expect(capsuleRest(read(500, 300), false).collapsed).toBe(true);
    // Brought to the newest end some other way (the jump control, a send), it expands too; a rest elsewhere
    // after no drag of theirs changes nothing.
    const collapsed = capsuleRest(read(500, 300), false);
    expect(capsuleRest(collapsed, true).collapsed).toBe(false);
    expect(capsuleRest(collapsed, false)).toBe(collapsed);
  });

  test("a tap, or something that needs them, expands it", () => {
    const collapsed = capsuleRest(read(500, 300), false);
    expect(capsuleExpand(collapsed).collapsed).toBe(false);
    expect(capsuleExpand(CAPSULE_OPEN)).toBe(CAPSULE_OPEN);
  });
});

describe("its room", () => {
  test("the content starts below it at rest", () => {
    expect(capsuleInset(36)).toBe(36 + CAPSULE.top + CAPSULE.height + CAPSULE.below);
  });

  test("a long title takes two lines", () => {
    expect(capsuleTitleIsLong("mewla")).toBe(false);
    expect(capsuleTitleIsLong("Daily Hacker News Briefing · Nov 13")).toBe(true);
  });
});
