import { describe, expect, test } from "bun:test";
import type { Daemon } from "../more/daemon";
import {
  activityReadout,
  chartDate,
  costSourceLabel,
  dayFacts,
  emptyRangeTitle,
  formatAvailableCost,
  formatAvailableTokens,
  formatCost,
  formatTokens,
  modelFacts,
  modelNotes,
  pricingLine,
  resetLabel,
  rowActivitySummary,
  shortDay,
  sortByActivity,
} from "./format";
import { loadStats } from "./load";
import type { DayCell, ModelStat } from "./statsPayload";

const model = (overrides: Partial<ModelStat> = {}): ModelStat => ({
  name: "gpt-6-astra", id: "gpt-6-astra", provider: "openai", cost: 2, costKnown: true, costProvenance: "estimated",
  totalTokens: 100, inputTokens: 80, outputTokens: 20, reasoningTokens: 0, cacheRead: 0, cacheCreate: 0, sessions: 1, ...overrides,
});

describe("Stats numbers", () => {
  test("counts are compact, dollars keep cents under a hundred", () => {
    expect(formatTokens(980)).toBe("980");
    expect(formatTokens(1_000)).toBe("1K");
    expect(formatTokens(1_250_000)).toBe("1.3M");
    expect(formatTokens(2_000_000_000_000)).toBe("2T");
    expect(formatCost(4.251)).toBe("$4.25");
    expect(formatCost(120.4)).toBe("$120");
    expect(formatCost(1_500)).toBe("$1.5K");
  });

  test("a partly unknown value says so with +, a wholly unknown one is a dash, never an invented zero", () => {
    expect(formatAvailableCost(3, false)).toBe("$3.00+");
    expect(formatAvailableCost(0, false)).toBe("—");
    expect(formatAvailableCost(0, undefined)).toBe("$0.00");
    expect(formatAvailableTokens(12_000, false)).toBe("12K+");
    expect(rowActivitySummary({ totalTokens: 0, totalTokensKnown: false, sessions: 1 })).toBe("1 session");
    expect(rowActivitySummary({ totalTokens: 1_200_000, sessions: 3 })).toBe("1.2M tokens · 3 sessions");
  });

  test("older computers reported estimates only", () => {
    expect(costSourceLabel(undefined)).toBe("Estimated");
    expect(costSourceLabel("unknown")).toBe("Unavailable");
    expect(costSourceLabel("mixed")).toBe("Mixed");
  });

  test("activity ranks by tokens when every item has them, else by sessions, ties by name", () => {
    const rows = [
      { name: "b", totalTokens: 10, sessions: 9 },
      { name: "a", totalTokens: 10, sessions: 1 },
      { name: "c", totalTokens: 50, sessions: 1 },
    ];
    expect(sortByActivity(rows).map((r) => r.name)).toEqual(["c", "a", "b"]);
    const unknown = [...rows, { name: "d", totalTokens: 0, totalTokensKnown: false, sessions: 20 }];
    expect(sortByActivity(unknown).map((r) => r.name)).toEqual(["d", "b", "a", "c"]);
  });

  test("dates read in English whatever the device language", () => {
    expect(shortDay("2026-04-04")).toBe("4/4");
    expect(chartDate("2026-10-27")).toBe("Oct 27");
    expect(resetLabel(undefined)).toBe("Reset time unavailable");
    expect(resetLabel("not a date")).toBe("Reset time unavailable");
    expect(resetLabel(new Date(2026, 9, 27, 20, 5).toISOString())).toBe("Resets Oct 27, 8:05 PM");
    expect(activityReadout(null, true)).toBe("0 tokens");
    expect(activityReadout(null, false)).toBe("0 sessions");
  });

  test("the pricing line names its source and says when a refresh failed before saying it is outdated", () => {
    const updated = (value: string) => (value ? "Oct 27, 2026" : "");
    expect(pricingLine({ basis: "", source: "LiteLLM", stale: true, updatedAt: "x" }, updated)).toBe("LiteLLM · Oct 27, 2026 · Outdated");
    expect(pricingLine({ basis: "", source: "LiteLLM", stale: true, lastError: "offline" }, updated)).toBe("LiteLLM · Refresh failed");
    expect(pricingLine(undefined, updated)).toBe("");
  });

  test("a model's details keep exact costs and its diagnostics, and leave out what was not reported", () => {
    const facts = Object.fromEntries(modelFacts(model({ cost: 0.0012346, costProvenance: "mixed", costReported: 0.001, costEstimated: 0.0002345 }), () => "Oct 27"));
    expect(facts.Cost).toBe("$0.001235");
    expect(facts.Reported).toBe("$0.001");
    expect(facts.Estimated).toBe("$0.000235");
    expect(facts.Source).toBeUndefined();
    expect(modelNotes(model({ unpricedReason: "missing_model", costEstimated: 1 }), { basis: "List price", source: "x", stale: false, lastError: "rate limit" })).toEqual([
      "Price not in catalog",
      "List price",
      "rate limit",
    ]);
  });

  test("a day's detail and the empty range's title", () => {
    const day: DayCell = { date: "2026-10-27", totalTokens: 0, totalTokensKnown: false, inputTokens: 0, outputTokens: 0, reasoningTokens: 0, cacheRead: 0, cacheCreate: 0, cost: 0, sessions: 2 };
    expect(Object.fromEntries(dayFacts(day)).Total).toBe("—");
    expect(emptyRangeTitle("week", false)).toBe("No stats yet");
    expect(emptyRangeTitle("day", true)).toBe("No activity today");
    expect(emptyRangeTitle("month", true)).toBe("No month activity");
  });
});

describe("loading stats", () => {
  function daemon(answers: { stats: () => unknown; providers?: () => unknown }): Daemon & { asked: string[] } {
    const asked: string[] = [];
    return {
      asked,
      server: { id: "s1", name: "Desk", url: "ws://x/ws", daemonId: "", daemonPublicKey: "" },
      state: "connected",
      on: () => () => {},
      onState: () => () => {},
      send: () => {},
      request: async (type) => {
        asked.push(type);
        if (type === "list_providers") {
          if (!answers.providers) throw new Error("no providers");
          return answers.providers() as never;
        }
        return answers.stats() as never;
      },
    };
  }
  const subscription = { authKind: "official", state: "available", windows: [] };

  test("an empty answer is asked again three times, then accepted", async () => {
    const d = daemon({ stats: () => ({ ranges: {} }) });
    const waits: number[] = [];
    const view = await loadStats(d, { wait: async (ms) => void waits.push(ms) });
    expect(waits).toEqual([700, 1400, 2100]);
    expect(d.asked.filter((t) => t === "get_stats").length).toBe(4);
    expect(Object.keys(view?.ranges ?? {})).toEqual([]);
  });

  test("a Provider routing Codex hides the subscription card; a failed Provider list does not hide the stats", async () => {
    const routed = await loadStats(daemon({ stats: () => ({ ranges: { week: {} }, codexSubscription: subscription }), providers: () => ({ defaults: { codex: { connection_id: "c1" } } }) }));
    expect(routed?.codexSubscriptions).toEqual([]);
    const direct = await loadStats(daemon({ stats: () => ({ ranges: { week: {} }, codexSubscription: subscription }) }));
    expect(direct?.codexSubscriptions.length).toBe(1);
  });

  test("a failure rejects with its words", async () => {
    await expect(loadStats(daemon({ stats: () => { throw new Error("get_stats timed out"); } }))).rejects.toThrow("Stats request timed out.");
  });
});
