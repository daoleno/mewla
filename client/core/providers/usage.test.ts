import { describe, expect, test } from "bun:test";
import { errorLine, localDay, parseProviderUsage, requestCostLabel, requestTokensLine, usageCostLabel, usageSince, usageTotals } from "./usage";

const NOW = Date.UTC(2031, 5, 15, 12, 0, 0);
const ago = (minutes: number) => new Date(NOW - minutes * 60_000).toISOString();

describe("Provider usage", () => {
  test("reads the daemon's reply and leaves out what it cannot read", () => {
    const usage = parseProviderUsage({
      type: "provider_usage",
      usage: {
        requests: [
          { at: ago(3), agent: "claude", model: "glm-5", input_tokens: 12, cache_read_tokens: 800, cache_write_tokens: 40, output_tokens: 57, cost_usd: 0.0042, priced: true, ms: 900 },
          { at: "not a time", agent: "codex" },
          "junk",
        ],
        errors: [{ at: ago(1), agent: "codex", model: "kimi-k2", status: 503, message: "Busy" }, { message: "no time" }],
        days: { "2031-06-15": { requests: 1, errors: 1, input_tokens: 12, output_tokens: 57, cost_usd: 0.0042 }, "June 15": { requests: 9 } },
      },
    });
    expect(usage.requests).toHaveLength(1);
    expect(usage.requests[0]).toMatchObject({ agent: "claude", model: "glm-5", input: 12, cacheRead: 800, cacheWrite: 40, output: 57, priced: true });
    expect(usage.errors).toHaveLength(1);
    expect(Object.keys(usage.days)).toEqual(["2031-06-15"]);
    expect(parseProviderUsage({ type: "provider_usage" })).toEqual({ requests: [], errors: [], days: {} });
  });

  test("adds up the days in range, and says when some had no price", () => {
    const day = (requests: number, cost: number, unpriced = 0) => ({ requests, errors: 0, input: 100, cacheRead: 0, cacheWrite: 0, output: 10, cost, unpriced });
    const today = localDay(NOW);
    const old = localDay(NOW - 40 * 86_400_000);
    const usage = { requests: [], errors: [], days: { [today]: day(2, 1.5), [old]: day(5, 9) } };
    const month = usageTotals(usage, usageSince(NOW, 30));
    expect(month).toEqual({ requests: 2, errors: 0, tokens: 110, cost: 1.5, costKnown: true });
    expect(usageCostLabel(month)).toBe("$1.50");
    usage.days[today] = day(2, 1.5, 1);
    expect(usageCostLabel(usageTotals(usage, today))).toBe("$1.50+");
    usage.days[today] = day(2, 0, 2);
    expect(usageCostLabel(usageTotals(usage, today))).toBe("—");
    // Failed requests are requests too.
    usage.days[today] = { ...day(2, 1.5), errors: 1 };
    expect(usageTotals(usage, today)).toMatchObject({ requests: 3, errors: 1 });
    expect(usageSince(NOW, 1)).toBe(today);
  });

  test("words a request and a failure", () => {
    const r = { at: NOW - 3 * 60_000, agent: "claude", model: "glm-5", input: 1200, cacheRead: 48_000, cacheWrite: 0, output: 350, cost: 0.0123, priced: true, ms: 2100 };
    expect(requestTokensLine(r)).toBe("1.2K in · 48K cached · 350 out");
    expect(requestTokensLine({ ...r, cacheRead: 0, cacheWrite: 2048 })).toBe("1.2K in · 2K to cache · 350 out");
    expect(requestCostLabel(r)).toBe("$0.0123");
    expect(requestCostLabel({ ...r, cost: 1.234 })).toBe("$1.23");
    expect(requestCostLabel({ ...r, cost: 0 })).toBe("Free");
    expect(requestCostLabel({ ...r, priced: false })).toBe("");
    expect(errorLine({ at: NOW - 60_000, agent: "codex", model: "kimi-k2", status: 429, message: "Slow down" }, NOW)).toBe("429 · Codex · kimi-k2 · 1m");
    expect(errorLine({ at: NOW, agent: "codex", model: "", status: 0, message: "Unreachable" }, NOW)).toBe("Codex · now");
  });
});
