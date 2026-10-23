import { describe, expect, test } from "bun:test";
import {
  codexStatusSections,
  formatReset,
  formatTokenCount,
  normalizeCodexStatus,
} from "./codexStatus";

const payload = {
  thread: { id: "th", model: "gpt-6", effort: "high", cwd: "/work", cli_version: "0.163.0", name: " " },
  config: { approval: "on-request", sandbox: "workspace-write", context_window: 272000 },
  account: { kind: "chatgpt", email: "me@example.com", plan: "pro" },
  limits: [
    {
      id: "codex",
      windows: [
        { used_percent: 12, window_minutes: 300, resets_at: 1000 },
        { used_percent: 140, window_minutes: 10080 },
      ],
      credits: { has_credits: true, unlimited: false, balance: "5.00" },
    },
    { id: "bad", windows: [{ used_percent: "x" }] },
  ],
  tokens: {
    total: { input: 10, cached: 4, output: 5, reasoning: 2, total: 15000 },
    last: { input: 1, total: 68000 },
    context_window: 272000,
  },
};

describe("codex status", () => {
  test("normalizes the daemon payload and drops what is malformed", () => {
    const status = normalizeCodexStatus(payload);
    expect(status.thread?.name).toBeUndefined();
    expect(status.limits[0].windows.map((w) => w.usedPercent)).toEqual([12, 100]);
    expect(status.limits[1].windows).toEqual([]);
    expect(normalizeCodexStatus(undefined).limits).toEqual([]);
  });

  test("lays the sections out in the order /status shows them", () => {
    const sections = codexStatusSections(normalizeCodexStatus(payload), new Date(0));
    expect(sections.map((s) => s.key)).toEqual(["session", "account", "tokens", "limits:codex", "limits:bad"]);
    const session = sections[0].rows.map((r) => `${r.label}=${r.value}`);
    expect(session).toContain("Model=gpt-6 (high)");
    expect(session).toContain("Approvals=on-request");
    expect(sections[1].rows.map((r) => r.value)).toEqual(["ChatGPT", "me@example.com", "pro"]);
    expect(sections[2].rows.find((r) => r.key === "context")?.value).toBe("75% left (68.0K of 272K)");
    expect(sections[3].meters?.map((m) => m.label)).toEqual(["5 hours", "1 week"]);
    expect(sections[3].rows[0]).toEqual({ key: "credits", label: "Credits", value: "5.00" });
  });

  test("formats counts and reset times", () => {
    expect(formatTokenCount(950)).toBe("950");
    expect(formatTokenCount(1_500_000)).toBe("1.5M");
    expect(formatReset(60, new Date(0)).startsWith("resets ")).toBe(true);
  });
});
