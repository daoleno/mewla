import { formatUSD } from "../more/dates";
import { relativeTime } from "../friends/api";
import { formatAvailableCost, formatTokens } from "../stats/format";

/** What a Provider's model requests used, as its daemon kept it: newest first. */

export type ProviderUsageRequest = {
  at: number;
  agent: string;
  model: string;
  /** Input not read from the prompt cache. */
  input: number;
  cacheRead: number;
  cacheWrite: number;
  output: number;
  cost: number;
  /** The model's prices were known; a cost of 0 is then free. */
  priced: boolean;
  ms: number;
};

export type ProviderUsageError = { at: number; agent: string; model: string; status: number; message: string };

export type ProviderUsageDay = { requests: number; errors: number; input: number; cacheRead: number; cacheWrite: number; output: number; cost: number; unpriced: number };

export type ProviderUsage = { requests: ProviderUsageRequest[]; errors: ProviderUsageError[]; days: Record<string, ProviderUsageDay> };

const num = (v: unknown) => (typeof v === "number" && Number.isFinite(v) ? v : 0);
const str = (v: unknown) => (typeof v === "string" ? v : "");
const time = (v: unknown) => {
  const t = Date.parse(str(v));
  return Number.isFinite(t) ? t : 0;
};
const record = (v: unknown) => (v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : null);

/** The `provider_usage` reply; anything unreadable is left out. */
export function parseProviderUsage(raw: unknown): ProviderUsage {
  const usage = record(record(raw)?.usage);
  const out: ProviderUsage = { requests: [], errors: [], days: {} };
  if (!usage) return out;
  for (const item of Array.isArray(usage.requests) ? usage.requests : []) {
    const r = record(item);
    if (!r || !time(r.at)) continue;
    out.requests.push({
      at: time(r.at), agent: str(r.agent), model: str(r.model),
      input: num(r.input_tokens), cacheRead: num(r.cache_read_tokens), cacheWrite: num(r.cache_write_tokens), output: num(r.output_tokens),
      cost: num(r.cost_usd), priced: r.priced === true, ms: num(r.ms),
    });
  }
  for (const item of Array.isArray(usage.errors) ? usage.errors : []) {
    const e = record(item);
    if (!e || !time(e.at)) continue;
    out.errors.push({ at: time(e.at), agent: str(e.agent), model: str(e.model), status: num(e.status), message: str(e.message) });
  }
  for (const [day, item] of Object.entries(record(usage.days) ?? {})) {
    const d = record(item);
    if (!d || !/^\d{4}-\d{2}-\d{2}$/.test(day)) continue;
    out.days[day] = {
      requests: num(d.requests), errors: num(d.errors), input: num(d.input_tokens), cacheRead: num(d.cache_read_tokens),
      cacheWrite: num(d.cache_write_tokens), output: num(d.output_tokens), cost: num(d.cost_usd), unpriced: num(d.unpriced),
    };
  }
  return out;
}

/** "2026-11-27" in the phone's own day, as the daemon keys its days in the computer's. */
export function localDay(ms: number): string {
  const d = new Date(ms);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

/** `requests` counts every request, `errors` the ones among them that failed. */
export type ProviderUsageTotals = { requests: number; errors: number; tokens: number; cost: number; costKnown: boolean };

/** The days from `since` (a local "YYYY-MM-DD") on, added up. */
export function usageTotals(usage: ProviderUsage, since: string): ProviderUsageTotals {
  const t = { requests: 0, errors: 0, tokens: 0, cost: 0, costKnown: true };
  for (const [day, d] of Object.entries(usage.days)) {
    if (day < since) continue;
    // The daemon counts a day's answered requests and its failed ones apart; Requests is both.
    t.requests += d.requests + d.errors;
    t.errors += d.errors;
    t.tokens += d.input + d.cacheRead + d.cacheWrite + d.output;
    t.cost += d.cost;
    if (d.unpriced > 0) t.costKnown = false;
  }
  return t;
}

/** The first day of the last `days` days, today among them. */
export function usageSince(now: number, days: number): string {
  const d = new Date(now);
  d.setDate(d.getDate() - (days - 1));
  return localDay(d.getTime());
}

export const USAGE_DAYS = 30;

/** "$4.20", "$1.10+" when some requests had no price, "—" when none had one. */
export function usageCostLabel(t: ProviderUsageTotals): string {
  return formatAvailableCost(t.cost, t.costKnown);
}

/** A request's cost, to the cent from a dollar and finer below it: "$1.20", "$0.0123"; "" without a price. */
export function requestCostLabel(r: ProviderUsageRequest): string {
  if (!r.priced) return "";
  if (r.cost === 0) return "Free";
  return formatUSD(r.cost, r.cost >= 1 ? 2 : 4);
}

const AGENT_NAMES: Record<string, string> = { claude: "Claude Code", codex: "Codex" };

export function usageAgentName(agent: string): string {
  return AGENT_NAMES[agent] ?? agent;
}

/** A request's tokens: "12K in · 800K cached · 1.2K out", the cache only when it was used. */
export function requestTokensLine(r: ProviderUsageRequest): string {
  const parts = [`${formatTokens(r.input)} in`];
  if (r.cacheRead > 0) parts.push(`${formatTokens(r.cacheRead)} cached`);
  if (r.cacheWrite > 0) parts.push(`${formatTokens(r.cacheWrite)} to cache`);
  parts.push(`${formatTokens(r.output)} out`);
  return parts.join(" · ");
}

/** "now", "3m", "2h", "yesterday" */
export function usageWhen(at: number, now: number): string {
  return relativeTime(at, now);
}

/** "503 · Codex · gpt-5.5 · 3m" */
export function errorLine(e: ProviderUsageError, now: number): string {
  return [e.status > 0 ? String(e.status) : "", usageAgentName(e.agent), e.model, relativeTime(e.at, now)].filter(Boolean).join(" · ");
}
