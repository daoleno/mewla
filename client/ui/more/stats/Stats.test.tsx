import { describe, expect, open, test } from "nsh/test";

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
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
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

  test("a switch to another computer drops the last computer's stats", async () => {
    let n = 0;
    const app = await open(HARNESS, computer(() => (++n === 1 ? { ranges: { week: range() } } : { ranges: { week: range({ cost: 99, costKnown: true, projects: [] }) } })));
    await app.waitFor("text:$12.50");
    await app.host.send("app.daemonSwitch", { id: "s2", name: "Laptop" });
    await app.waitFor("text:$99.00");
    expect(app.texts()).not.toContain("$12.50");
  });
});
