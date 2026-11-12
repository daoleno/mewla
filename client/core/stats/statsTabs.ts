/** Stats' ranges (SPEC §15 Metrics): a segmented 7 / 14 / 30 days, the daemon's week, fortnight and month. */
export const STATS_RANGE_TABS = [
  { key: 'week', label: '7 days', days: 7 },
  { key: 'fortnight', label: '14 days', days: 14 },
  { key: 'month', label: '30 days', days: 30 },
] as const;

export type StatsRange = (typeof STATS_RANGE_TABS)[number]['key'];
export type StatsRangeTab = (typeof STATS_RANGE_TABS)[number];

export const INITIAL_STATS_RANGE: StatsRange = 'fortnight';

/**
 * The ranges a computer answered with: a daemon from before the 14-day range has 7 and 30 only.
 * Before any answer, all three.
 */
export function availableRanges(ranges: Readonly<Record<string, unknown>> | null | undefined): readonly StatsRangeTab[] {
  if (!ranges) return STATS_RANGE_TABS;
  const there = STATS_RANGE_TABS.filter((tab) => tab.key in ranges);
  return there.length ? there : STATS_RANGE_TABS;
}

/** The range shown: the one picked when the computer has it, else 14 days, else the first it has. */
export function shownRange(picked: StatsRange, tabs: readonly StatsRangeTab[]): StatsRange {
  if (tabs.some((tab) => tab.key === picked)) return picked;
  return tabs.find((tab) => tab.key === INITIAL_STATS_RANGE)?.key ?? tabs[0]!.key;
}

/** "Last 14 days": the eyebrow over the total. */
export function rangeEyebrow(range: StatsRange): string {
  return `Last ${STATS_RANGE_TABS.find((tab) => tab.key === range)!.days} days`;
}

export function rangeDays(range: StatsRange): number {
  return STATS_RANGE_TABS.find((tab) => tab.key === range)!.days;
}
