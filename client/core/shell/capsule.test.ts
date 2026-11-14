import { describe, expect, test } from "bun:test";
import {
  CAPSULE, CAPSULE_OPEN, capsuleCollapse, capsuleCompactNameWidth, capsuleDragStart, capsuleExpand, capsuleInset, capsuleNameIsLong,
  capsuleRest, capsuleScroll, capsuleTerminalInset, crampedOverKeyboard, type CapsuleState,
} from "./capsule";

const far = 2000;
/** Rested at `at`, then the person drags through `offsets`, far from the newest end. */
const read = (at: number, ...offsets: number[]) =>
  offsets.reduce((state: CapsuleState, offset) => capsuleScroll(state, offset, far), capsuleDragStart(capsuleRest(CAPSULE_OPEN, at, far)));

describe("the capsule collapses while the person reads (SPEC §11)", () => {
  test("past 24 pt from where the content last came to rest, either way", () => {
    expect(read(500, 500 - CAPSULE.collapseAfter).collapsed).toBe(false);
    expect(read(500, 500 - CAPSULE.collapseAfter - 1).collapsed).toBe(true);
    expect(read(500, 500 + CAPSULE.collapseAfter + 1).collapsed).toBe(true);
  });

  test("not near the newest end", () => {
    const state = capsuleDragStart(capsuleRest(CAPSULE_OPEN, 500, 0));
    expect(capsuleScroll(state, 540, CAPSULE.atEnd).collapsed).toBe(false);
  });

  test("never from content arriving, a streaming reply or a scroll the app makes", () => {
    const opened = capsuleRest(CAPSULE_OPEN, 900, 0);
    const followed = [1400, 2000, 2600].reduce((state: CapsuleState, offset) => capsuleScroll(state, offset, far), opened);
    expect(followed.collapsed).toBe(false);
    // Such a scroll moves where the content rests: the person's next drag counts from there.
    expect(followed.anchor).toBe(2600);
  });

  test("expands when a scroll comes to rest within 48 pt of the newest end, however it got there", () => {
    const collapsed = capsuleRest(read(500, 300), 300, far);
    expect(collapsed.collapsed).toBe(true);
    expect(capsuleRest(collapsed, 2000, CAPSULE.atEnd).collapsed).toBe(false);
    expect(capsuleRest(collapsed, 1000, CAPSULE.atEnd + 1).collapsed).toBe(true);
  });

  test("a tap on the name, or something that needs them, expands it; the terminal collapses it", () => {
    const collapsed = capsuleRest(read(500, 300), 300, far);
    expect(capsuleExpand(collapsed).collapsed).toBe(false);
    expect(capsuleExpand(CAPSULE_OPEN)).toBe(CAPSULE_OPEN);
    expect(capsuleCollapse(CAPSULE_OPEN).collapsed).toBe(true);
  });
});

describe("its room", () => {
  test("the content is padded once, for the expanded capsule; the terminal for the collapsed one", () => {
    expect(capsuleInset(36)).toBe(36 + 6 + 56);
    expect(capsuleTerminalInset(36)).toBe(36 + 6 + 44 + 4);
  });

  test("a long name takes two lines; collapsed, it stops at min(200, 52 % of the width)", () => {
    expect(capsuleNameIsLong("mewla")).toBe(false);
    expect(capsuleNameIsLong("Daily Hacker News Briefing · Nov 13")).toBe(true);
    expect(capsuleCompactNameWidth(360)).toBe(187);
    expect(capsuleCompactNameWidth(412)).toBe(200);
  });
});

describe("the terminal over the keyboard of a phone held sideways (11-14)", () => {
  test("an SE (375 pt) and an iPhone 18 Pro (402 pt) sideways with the keyboard up are cramped", () => {
    expect(crampedOverKeyboard(375, 194)).toBe(true);
    expect(crampedOverKeyboard(402, 209)).toBe(true);
  });

  test("upright, or with the keyboard down, never", () => {
    expect(crampedOverKeyboard(667, 260)).toBe(false);
    expect(crampedOverKeyboard(874, 336)).toBe(false);
    expect(crampedOverKeyboard(375, 0)).toBe(false);
  });
});
