import { describe, expect, open, test } from "nsh/test";
import { answering } from "../../testing/fixtures";

const HARNESS = "../testing/Harness.tsx";

const range = (overrides: Record<string, unknown> = {}) => ({
  cost: 12.5, costKnown: true, costProvenance: "reported", totalTokens: 1_250_000, inputTokens: 1_000_000, outputTokens: 250_000,
  reasoningTokens: 0, cacheRead: 0, cacheCreate: 0, sessions: 3,
  models: [
    { name: "claude-opus-5-5", id: "claude-opus-5-5", provider: "anthropic", cost: 10, costKnown: true, costProvenance: "reported", totalTokens: 1_000_000, inputTokens: 800_000, outputTokens: 200_000, reasoningTokens: 0, cacheRead: 0, cacheCreate: 0, sessions: 2 },
    { name: "gpt-6", id: "gpt-6", provider: "openai", cost: 2.5, costKnown: true, costProvenance: "estimated", totalTokens: 250_000, inputTokens: 200_000, outputTokens: 50_000, reasoningTokens: 0, cacheRead: 0, cacheCreate: 0, sessions: 1, estimateSource: "LiteLLM" },
  ],
  projects: [{ name: "mewla", cost: 12.5, costKnown: true, totalTokens: 1_250_000, inputTokens: 0, outputTokens: 0, reasoningTokens: 0, cacheRead: 0, cacheCreate: 0, sessions: 3 }],
  skills: [], tools: [{ name: "Bash", calls: 40 }],
  days: [{ date: today(), cost: 12.5, totalTokens: 1_250_000, inputTokens: 0, outputTokens: 0, reasoningTokens: 0, cacheRead: 0, cacheCreate: 0, sessions: 3 }],
  ...overrides,
});

function today(): string {
  return daysAgo(0);
}

function daysAgo(n: number): string {
  const d = new Date();
  d.setDate(d.getDate() - n);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

/** A week with activity on the days `active` names (days ago), `1000 × (n + 1)` tokens each. */
function week(active: number[]) {
  return active.map((n) => ({ date: daysAgo(n), cost: 1, totalTokens: 1000 * (n + 1), inputTokens: 0, outputTokens: 0, reasoningTokens: 0, cacheRead: 0, cacheCreate: 0, sessions: 1 })).reverse();
}

function computer(stats: () => unknown, providers: unknown = { defaults: {} }) {
  const asked: string[] = [];
  return {
    asked,
    host: {
      params: { page: { name: "stats" } },
      handlers: {
        "app.daemon": ({ type }: { type: string }) => {
          asked.push(type);
          if (type === "list_providers") return providers;
          if (type === "get_stats") return stats();
          return { error: `unexpected ${type}` };
        },
      },
    },
  };
}

describe("Stats", () => {
  test("opens on Week with the totals, the models and the projects", async () => {
    const c = computer(() => ({ ranges: { day: range(), week: range(), month: range(), all: range() } }));
    const app = await open(HARNESS, c);
    await app.waitFor("id:stats-summary");
    const texts = app.texts().join(" | ");
    expect(texts).toContain("$12.50");
    expect(texts).toContain("1.3M");
    expect(texts).toContain("Cost · Reported");
    expect(texts).toContain("claude-opus-5-5");
    expect(texts).toContain("≈$2.50");
    expect(texts).toContain("mewla");
    expect(texts).toContain("40 calls");
  });

  test("a model opens its full details", async () => {
    const app = await open(HARNESS, computer(() => ({ ranges: { week: range(), all: range() } })));
    await app.waitFor("text:gpt-6");
    await app.press("text:gpt-6");
    await app.waitFor("id:stats-model");
    const texts = app.texts().join(" | ");
    expect(texts).toContain("Price type");
    expect(texts).toContain("$2.50");
    expect(texts).toContain("LiteLLM");
  });

  test("an empty computer says so after asking again, and a failure offers to try again", async () => {
    const empty = computer(() => ({ ranges: {} }));
    const app = await open(HARNESS, empty);
    await app.waitFor("text:No stats yet", { timeout: 8_000 });
    expect(empty.asked.filter((t) => t === "get_stats").length).toBe(4);

    let fail = true;
    const flaky = await open(HARNESS, computer(() => (fail ? { error: "Stats request timed out." } : { ranges: { week: range() } })));
    await flaky.waitFor("text:Stats didn't load");
    expect(flaky.texts()).toContain("Stats request timed out.");
    fail = false;
    await flaky.press("id:stats-failed-action");
    await flaky.waitFor("id:stats-summary");
  }, 15_000);

  test("Day says there was no activity today when other ranges have some", async () => {
    const app = await open(HARNESS, computer(() => ({ ranges: { day: range({ cost: 0, totalTokens: 0, sessions: 0, models: [], projects: [], tools: [], days: [] }), week: range(), all: range() } })));
    await app.waitFor("id:stats-summary");
    await app.press("id:stats-range-day");
    await app.waitFor("text:No activity today");
  });

  test("a Provider routing Codex hides the subscription card; ChatGPT sign-in shows the time left", async () => {
    const subscription = { authKind: "official", state: "available", plan: "pro", windows: [{ name: "primary", usedPercent: 30, windowMinutes: 300 }] };
    const direct = await open(HARNESS, computer(() => ({ ranges: { week: range() }, codexSubscription: subscription })));
    await direct.waitFor("id:codex-card");
    expect(direct.texts()).toContain("ChatGPT Pro");
    expect(direct.texts()).toContain("70% left");
    const routed = await open(HARNESS, computer(() => ({ ranges: { week: range() }, codexSubscription: subscription }), { defaults: { codex: { connection_id: "c1" } } }));
    await routed.waitFor("id:stats-summary");
    expect(routed.shows("id:codex-card")).toBe(false);
  });

  test("a swipe across the page moves between ranges; the tabs still do too", async () => {
    const app = await open(HARNESS, computer(() => ({ ranges: { day: range({ cost: 1 }), week: range(), month: range({ cost: 99 }), all: range({ cost: 500 }) } })));
    await app.waitFor("text:$12.50");
    await app.drag("id:stats-summary", { x: -140 });
    await app.waitFor("text:$99.00");
    await app.drag("id:stats-summary", { x: 140 });
    await app.waitFor("text:$12.50");
    await app.drag("id:stats-summary", { x: 140 });
    await app.waitFor("text:$1.00");
    // Past the first range there is nothing more to the right.
    await app.drag("id:stats-summary", { x: 140 });
    expect(app.texts()).toContain("$1.00");
    await app.press("id:stats-range-all");
    await app.waitFor("text:$500");
  });

  test("in a browser the ranges change by their tabs only; a swipe leaves the range alone", async () => {
    const app = await open(HARNESS, { ...computer(() => ({ ranges: { day: range({ cost: 1 }), week: range(), month: range({ cost: 99 }), all: range({ cost: 500 }) } })), platform: "web" });
    await app.waitFor("text:$12.50");
    await app.drag("id:stats-summary", { x: -140 });
    await app.wait(100);
    expect(app.texts()).toContain("$12.50");
    expect(app.texts()).not.toContain("$99.00");
    await app.press("id:stats-range-month");
    await app.waitFor("text:$99.00");
  });

  test("a swipe that starts on the activity chart scrubs it and leaves the range alone", async () => {
    const app = await open(HARNESS, computer(() => ({ ranges: { week: range({ days: week([0, 1, 2, 3, 4, 5, 6]) }), month: range({ cost: 99 }) } })));
    await app.waitFor("id:activity-chart");
    expect(app.texts()).toContain("Today");
    expect(app.texts()).toContain("1K tokens");
    await app.dragAndHold("id:activity-chart", { x: -100 });
    expect(app.texts()).not.toContain("1K tokens");
    expect(app.texts()).not.toContain("Today");
    await app.drop();
    // Lifting the finger resets the readout and opens nothing.
    await app.wait(100);
    expect(app.shows("id:stats-day")).toBe(false);
    expect(app.texts()).toContain("1K tokens");
    expect(app.texts()).toContain("$12.50");
  });

  test("a tap on the chart opens that day when it had activity, and nothing on an empty day", async () => {
    const busy = await open(HARNESS, computer(() => ({ ranges: { week: range({ days: week([0, 1, 2, 3, 4, 5, 6]) }) } })));
    await busy.waitFor("id:activity-chart");
    await busy.press("id:activity-chart");
    await busy.waitFor("id:stats-day");
    expect(busy.texts()).toContain("4K");

    const quiet = await open(HARNESS, computer(() => ({ ranges: { week: range({ days: week([0]) }) } })));
    await quiet.waitFor("id:activity-chart");
    await quiet.press("id:activity-chart");
    await quiet.wait(100);
    expect(quiet.shows("id:stats-day")).toBe(false);
  });

  test("the chart and the model rows say what they are to a screen reader", async () => {
    const app = await open(HARNESS, computer(() => ({ ranges: { week: range() } })));
    await app.waitFor("id:stats-summary");
    expect(app.shows("label:Daily activity")).toBe(true);
    expect(app.shows("label:gpt-6, openai, Estimated cost $2.50, 250K tokens · 1 session")).toBe(true);
  });

  test("a model's id, facts and notes can be copied", async () => {
    const app = await open(HARNESS, computer(() => ({ ranges: { week: range() }, pricing: { basis: "Estimated from public list prices", source: "LiteLLM", stale: false, lastError: "Catalog refresh failed" } })));
    await app.waitFor("text:gpt-6");
    await app.press("text:gpt-6");
    await app.waitFor("id:stats-model");
    await app.press("id:copy-model-id");
    await app.waitUntil(() => app.clipboard() === "gpt-6");
    app.onDialog((dialog) => (dialog.sheet ? "Copy" : null));
    await answering(app, () => app.longPress("id:model-fact-Source"));
    expect(app.dialogs().at(-1)?.title).toBe("Source");
    await app.waitUntil(() => app.clipboard() === "LiteLLM");
    await answering(app, () => app.longPress("text:Catalog refresh failed"));
    await app.waitUntil(() => app.clipboard() === "Catalog refresh failed");
  });

  test("a switch to another computer closes an open model or day page", async () => {
    const app = await open(HARNESS, computer(() => ({ ranges: { week: range({ days: week([0, 1, 2, 3, 4, 5, 6]) }) } })));
    await app.waitFor("text:gpt-6");
    await app.press("text:gpt-6");
    await app.waitFor("id:stats-model");
    await app.host.send("app.daemonSwitch", { id: "s2", name: "Laptop" });
    await app.waitFor("id:stats-model", { state: "gone" });
    await app.waitFor("id:stats-summary");
    await app.press("id:activity-chart");
    await app.waitFor("id:stats-day");
    await app.host.send("app.daemonSwitch", { id: "s3", name: "Desk" });
    await app.waitFor("id:stats-day", { state: "gone" });
    // Only the detail page closes: Stats stays.
    expect(app.shows("id:stats")).toBe(true);
  });

  test("a range with stats but no models, then one with models, shows the Models card", async () => {
    const app = await open(HARNESS, computer(() => ({ ranges: { day: range({ models: [], projects: [] }), week: range() } })));
    await app.waitFor("id:stats-summary");
    await app.press("id:stats-range-day");
    await app.waitFor("id:stats-models", { state: "gone" });
    await app.press("id:stats-range-week");
    await app.waitFor("id:stats-models");
    expect(app.shows("id:stats-projects")).toBe(true);
  });

  test("a switch to another computer drops the last computer's stats", async () => {
    let n = 0;
    const app = await open(HARNESS, computer(() => (++n === 1 ? { ranges: { week: range() } } : { ranges: { week: range({ cost: 99, costKnown: true, projects: [] }) } })));
    await app.waitFor("text:$12.50");
    await app.host.send("app.daemonSwitch", { id: "s2", name: "Laptop" });
    await app.waitFor("text:$99.00");
    expect(app.texts()).not.toContain("$12.50");
  });
});
