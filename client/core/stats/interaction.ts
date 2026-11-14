import { formatLongDay, parseDateOnly } from "../more/dates";
import { costSourceLabel, formatAvailableCost, formatAvailableTokens, rowActivitySummary, sessionSummary } from "./format";
import type { DayCell, ModelStat } from "./statsPayload";
import { STATS_RANGE_TABS, type StatsRange, type StatsRangeTab } from "./statsTabs";

/** How Stats is read aloud and moved through: the chart's steps, the range swipe, the rows' labels. */

/** "2026-10-27" → "Sunday, October 27, 2026"; a date it cannot read stays as it is. */
export function accessibleDate(date: string): string {
  const day = parseDateOnly(date);
  return day ? formatLongDay(day) : date;
}

/** "1.2M tokens, 3 sessions, $4.25 reported cost" */
export function dayAccessibilityValue(day: DayCell): string {
  return [
    `${formatAvailableTokens(day.totalTokens, day.totalTokensKnown)} tokens`,
    sessionSummary(day.sessions),
    `${formatAvailableCost(day.cost, day.costKnown)} ${costSourceLabel(day.costProvenance).toLowerCase()} cost`,
  ].join(", ");
}

/** What a screen reader hears for the chart's day: the long date, then its numbers. */
export function activityAccessibilityValue(date: string, day: DayCell | null): string {
  return `${accessibleDate(date)}, ${day ? dayAccessibilityValue(day) : "No recorded activity"}`;
}

export const ACTIVITY_ACCESSIBILITY_HINT = "Adjust to move between days; activate for the day's details";
export const ACTIVITY_ACCESSIBILITY_ACTIONS = [{ name: "increment" }, { name: "decrement" }, { name: "activate" }];

/** What a screen reader's action on the chart does: move to the next or previous day, or open this one (only a day with activity). */
export function activityAction(
  action: string,
  index: number,
  days: ReadonlyArray<{ day: DayCell | null }>,
): { focus: number } | { open: DayCell } | null {
  const last = days.length - 1;
  if (last < 0) return null;
  const at = Math.max(0, Math.min(last, index));
  if (action === "increment") return { focus: Math.min(last, at + 1) };
  if (action === "decrement") return { focus: Math.max(0, at - 1) };
  if (action === "activate") {
    const day = days[at]?.day;
    return day ? { open: day } : null;
  }
  return null;
}

/** The day a tap on the chart opens: only one that has activity. */
export function dayToOpen(days: ReadonlyArray<{ day: DayCell | null }>, index: number): DayCell | null {
  return days[index]?.day ?? null;
}

/**
 * A finger on the chart: a scrub once it moved 6 points across, a page scroll once it
 * moved 10 points down or up first. Decided once; then it stays.
 */
export type ScrubPhase = "pending" | "scrub" | "scroll";
export const SCRUB_ACTIVE_X = 6;
export const SCRUB_FAIL_Y = 10;

export function scrubPhase(phase: ScrubPhase, dx: number, dy: number): ScrubPhase {
  if (phase !== "pending") return phase;
  const across = Math.abs(dx);
  const down = Math.abs(dy);
  if (across > SCRUB_ACTIVE_X && across >= down) return "scrub";
  if (down > SCRUB_FAIL_Y) return "scroll";
  return "pending";
}

/** A horizontal swipe over the page moves one range on: leftwards to the next, rightwards to the previous. */
export const RANGE_SWIPE_DISTANCE = 48;

export function swipedRange(range: StatsRange, dx: number, dy: number, tabs: readonly StatsRangeTab[] = STATS_RANGE_TABS): StatsRange | null {
  if (Math.abs(dx) < RANGE_SWIPE_DISTANCE || Math.abs(dx) <= Math.abs(dy)) return null;
  const next = tabs.findIndex((tab) => tab.key === range) + (dx < 0 ? 1 : -1);
  if (next < 0 || next >= tabs.length) return null;
  return tabs[next]!.key;
}

/** A model row read aloud: "gpt-6, openai, Estimated cost $2.50, 250K tokens · 1 session" */
export function modelAccessibilityLabel(model: ModelStat): string {
  return `${model.id || model.name}, ${model.provider || ""}, ${costSourceLabel(model.costProvenance)} cost ${formatAvailableCost(model.cost, model.costKnown)}, ${rowActivitySummary(model)}`;
}

export const MODEL_ROW_HINT = "Open model usage and pricing details";

/** A Codex window read aloud as one: "5 hours, 70 percent remaining, Resets Oct 27, 8:05 PM" */
export function codexWindowAccessibilityLabel(label: string, remainingPercent: number, reset: string): string {
  return `${label}, ${remainingPercent.toFixed(0)} percent remaining, ${reset}`;
}
