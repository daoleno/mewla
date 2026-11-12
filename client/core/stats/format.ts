import { formatDateTime, formatShortDate, formatUSD, parseDateOnly } from "../more/dates";
import { isCostKnown, isTotalTokensKnown, type CostProvenance, type DayCell, type ModelStat, type PricingStatus } from "./statsPayload";
import { unpricedReasonLabel } from "./unpricedReason";

/** How Stats words its numbers: compact counts, dollars, and what is not known. */

const UNITS: ReadonlyArray<readonly [number, string]> = [[1e12, "T"], [1e9, "B"], [1e6, "M"], [1e3, "K"]];

function compact(n: number): string {
  const absolute = Math.abs(n);
  for (const [threshold, suffix] of UNITS) {
    if (absolute >= threshold) return `${(n / threshold).toFixed(1).replace(/\.0$/, "")}${suffix}`;
  }
  return String(Math.round(n));
}

/** "1.2M", "980" */
export function formatTokens(n: number): string {
  return compact(n);
}

/** "$1.2K", "$120", "$4.25" */
export function formatCost(n: number): string {
  if (Math.abs(n) >= 1_000) return `$${compact(n)}`;
  if (n >= 100) return `$${n.toFixed(0)}`;
  return `$${n.toFixed(2)}`;
}

/** A value that may be partly unknown: "12K+" when some of it is missing, "—" when none is known. */
export function formatAvailable(value: number, known: boolean | undefined, format: (n: number) => string): string {
  if (known === false) return value > 0 ? `${format(value)}+` : "—";
  return format(value);
}

export const formatAvailableCost = (cost: number, known?: boolean) => formatAvailable(cost, known, formatCost);
export const formatAvailableTokens = (tokens: number, known?: boolean) => formatAvailable(tokens, known, formatTokens);

/** The price's provenance in a word; older computers reported estimates only. */
export function costSourceLabel(source?: CostProvenance): string {
  switch (source) {
    case "reported": return "Reported";
    case "mixed": return "Mixed";
    case "unknown": return "Unavailable";
    default: return "Estimated";
  }
}

export function sessionSummary(sessions: number): string {
  return `${sessions} ${sessions === 1 ? "session" : "sessions"}`;
}

/** A row's second line: "1.2M tokens · 3 sessions", or only the sessions when tokens are not known. */
export function rowActivitySummary(item: { totalTokens: number; totalTokensKnown?: boolean; sessions: number }): string {
  if (!isTotalTokensKnown(item) && item.totalTokens === 0) return sessionSummary(item.sessions);
  return `${formatAvailableTokens(item.totalTokens, item.totalTokensKnown)} tokens · ${sessionSummary(item.sessions)}`;
}

/** Activity is measured in tokens when every item has them, else in sessions. */
export function activityUsesTokens(items: ReadonlyArray<{ totalTokens: number; totalTokensKnown?: boolean }>): boolean {
  return items.every((item) => isTotalTokensKnown(item) || item.totalTokens > 0);
}

export function activityValue(item: { totalTokens: number; sessions: number }, usesTokens: boolean): number {
  return usesTokens ? item.totalTokens : item.sessions;
}

/** Most active first; ties by name. */
export function sortByActivity<T extends { name: string; totalTokens: number; totalTokensKnown?: boolean; sessions: number }>(items: readonly T[]): T[] {
  const usesTokens = activityUsesTokens(items);
  return [...items].sort((a, b) => activityValue(b, usesTokens) - activityValue(a, usesTokens) || (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
}

/** "2026-04-04" → "4/4" */
export function shortDay(date: string): string {
  const parts = date.split("-");
  return parts.length < 3 ? date : `${parseInt(parts[1], 10)}/${parseInt(parts[2], 10)}`;
}

/** "2026-10-27" → "Oct 27" */
export function chartDate(date: string): string {
  const day = parseDateOnly(date);
  return day ? formatShortDate(day) : date;
}

/**
 * What the one chart draws (SPEC §15 Metrics): spend by day; activity (tokens, else sessions) only
 * when no day in it has a known cost.
 */
export type ChartSeries = "cost" | "tokens" | "sessions";

export function chartSeries(days: ReadonlyArray<DayCell | null>): ChartSeries {
  const known = days.filter((day): day is DayCell => day !== null);
  // A known cost above nothing: a range of only unpriced or zero days draws its activity instead.
  if (known.some((day) => isCostKnown(day) && day.cost > 0)) return "cost";
  return activityUsesTokens(known) ? "tokens" : "sessions";
}

export function chartValue(day: DayCell | null, series: ChartSeries): number {
  if (!day) return 0;
  return series === "cost" ? Math.max(0, day.cost) : series === "tokens" ? day.totalTokens : day.sessions;
}

/** The chart's readout for a day: "$5.20", "1.2M tokens", "3 sessions". */
export function chartReadout(day: DayCell | null, series: ChartSeries): string {
  if (series === "cost") return formatAvailableCost(day?.cost ?? 0, day ? day.costKnown : true);
  return activityReadout(day, series === "tokens");
}

/** The activity chart's readout for a day. */
export function activityReadout(day: DayCell | null, usesTokens: boolean): string {
  if (usesTokens) return `${day ? formatAvailableTokens(day.totalTokens, day.totalTokensKnown) : "0"} tokens`;
  return sessionSummary(day?.sessions ?? 0);
}

/** "Resets Oct 27, 8:05 PM" */
export function resetLabel(value?: string): string {
  const at = value ? formatDateTime(value) : "";
  return at ? `Resets ${at}` : "Reset time unavailable";
}

/** The pricing line under the totals: its source, when it was updated, and whether it is current. */
export function pricingLine(pricing: PricingStatus | undefined, formatUpdated: (value: string) => string): string {
  if (!pricing) return "";
  const parts = [pricing.source];
  const updated = pricing.updatedAt ? formatUpdated(pricing.updatedAt) : "";
  if (updated) parts.push(updated);
  if (pricing.lastError) parts.push("Refresh failed");
  else if (pricing.stale) parts.push("Outdated");
  return parts.filter(Boolean).join(" · ");
}

/** A model's facts, in order, without the ones its computer did not report. */
export function modelFacts(model: ModelStat, formatUpdated: (value: string) => string): Array<[string, string]> {
  const money = (value: number) => formatUSD(value, 6);
  const reported = model.costReported ?? model.reportedCost;
  const estimated = model.costEstimated ?? model.estimatedCost;
  const facts: Array<[string, string | undefined]> = [
    ["Provider", model.provider],
    ["Price type", costSourceLabel(model.costProvenance)],
    ["Cost", formatAvailable(model.cost, model.costKnown, money)],
    ["Reported", model.costProvenance === "mixed" && reported !== undefined && reported > 0 ? money(reported) : undefined],
    ["Estimated", model.costProvenance === "mixed" && estimated !== undefined && estimated > 0 ? money(estimated) : undefined],
    ["Tokens", formatAvailableTokens(model.totalTokens, model.totalTokensKnown)],
    ["Sessions", String(model.sessions)],
    ["Input", formatAvailableTokens(model.inputTokens, model.tokenBreakdownKnown)],
    ["Output", formatAvailableTokens(model.outputTokens, model.tokenBreakdownKnown)],
    ["Reasoning", formatAvailableTokens(model.reasoningTokens, model.tokenBreakdownKnown)],
    ["Cache read", formatAvailableTokens(model.cacheRead, model.tokenBreakdownKnown)],
    ["Cache write", formatAvailableTokens(model.cacheCreate, model.tokenBreakdownKnown)],
    ["Source", model.estimateSource],
    ["Reference provider", model.referenceProvider],
    ["Updated", model.pricingUpdatedAt ? formatUpdated(model.pricingUpdatedAt) : undefined],
  ];
  return facts.filter((fact): fact is [string, string] => fact[1] !== undefined && fact[1] !== "");
}

/** The notes under a model's facts: why it has no price, how estimates are made, the last pricing failure. */
export function modelNotes(model: ModelStat, pricing?: PricingStatus): string[] {
  const estimated = model.costEstimated ?? model.estimatedCost;
  return [
    unpricedReasonLabel(model.unpricedReason) ?? "",
    estimated !== undefined && estimated > 0 && pricing?.basis ? pricing.basis : "",
    pricing?.lastError ?? "",
  ].filter(Boolean);
}

/** A day's cells in the day detail. */
export function dayFacts(day: DayCell): Array<[string, string]> {
  return [
    ["Cost", formatAvailableCost(day.cost, day.costKnown)],
    ["Sessions", String(day.sessions)],
    ["Total", formatAvailableTokens(day.totalTokens, day.totalTokensKnown)],
    ["Input", formatAvailableTokens(day.inputTokens, day.tokenBreakdownKnown)],
    ["Cache", formatAvailableTokens(day.cacheRead, day.tokenBreakdownKnown)],
    ["Output", formatAvailableTokens(day.outputTokens, day.tokenBreakdownKnown)],
    ["Reason", formatAvailableTokens(day.reasoningTokens, day.tokenBreakdownKnown)],
  ];
}

/** The empty range's title. */
export function emptyRangeTitle(days: number, anyStats: boolean): string {
  if (!anyStats) return "No stats yet";
  return `No activity in the last ${days} days`;
}
