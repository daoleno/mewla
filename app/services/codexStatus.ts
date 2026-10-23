import { codexWindowLabel, normalizeCodexUsedPercent } from "./codexSubscriptionStats";

/** What Codex's own app-server reports about one chat's thread. */
export interface CodexStatus {
  thread?: {
    id: string;
    name?: string;
    cwd?: string;
    model?: string;
    modelProvider?: string;
    effort?: string;
    cliVersion?: string;
    state?: string;
    updatedAt?: number;
  };
  config?: {
    model?: string;
    effort?: string;
    summary?: string;
    serviceTier?: string;
    approval?: string;
    sandbox?: string;
    contextWindow?: number;
  };
  account?: { kind: string; email?: string; plan?: string };
  limits: CodexStatusLimit[];
  tokens?: {
    total: CodexStatusTokens;
    last: CodexStatusTokens;
    contextWindow?: number;
  };
}

export interface CodexStatusLimit {
  id?: string;
  name?: string;
  windows: { usedPercent: number; windowMinutes?: number; resetsAt?: number }[];
  credits?: { unlimited: boolean; balance?: string };
  reached?: string;
}

export interface CodexStatusTokens {
  input: number;
  cached: number;
  output: number;
  reasoning: number;
  total: number;
}

type Raw = Record<string, any>;

function record(value: unknown): Raw | undefined {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Raw)
    : undefined;
}

function text(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

function count(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) && value > 0
    ? value
    : undefined;
}

function tokens(value: unknown): CodexStatusTokens {
  const raw = record(value) ?? {};
  return {
    input: count(raw.input) ?? 0,
    cached: count(raw.cached) ?? 0,
    output: count(raw.output) ?? 0,
    reasoning: count(raw.reasoning) ?? 0,
    total: count(raw.total) ?? 0,
  };
}

export function normalizeCodexStatus(payload: unknown): CodexStatus {
  const raw = record(payload) ?? {};
  const thread = record(raw.thread);
  const config = record(raw.config);
  const account = record(raw.account);
  const usage = record(raw.tokens);
  return {
    thread: thread && {
      id: text(thread.id) ?? "",
      name: text(thread.name),
      cwd: text(thread.cwd),
      model: text(thread.model),
      modelProvider: text(thread.model_provider),
      effort: text(thread.effort),
      cliVersion: text(thread.cli_version),
      state: text(thread.state),
      updatedAt: count(thread.updated_at),
    },
    config: config && {
      model: text(config.model),
      effort: text(config.effort),
      summary: text(config.summary),
      serviceTier: text(config.service_tier),
      approval: text(config.approval),
      sandbox: text(config.sandbox),
      contextWindow: count(config.context_window),
    },
    account: account && {
      kind: text(account.kind) ?? "",
      email: text(account.email),
      plan: text(account.plan),
    },
    limits: (Array.isArray(raw.limits) ? raw.limits : []).flatMap(
      (entry: unknown) => {
        const limit = record(entry);
        if (!limit) return [];
        const credits = record(limit.credits);
        return [
          {
            id: text(limit.id),
            name: text(limit.name),
            windows: (Array.isArray(limit.windows) ? limit.windows : []).flatMap(
              (item: unknown) => {
                const window = record(item);
                if (!window || typeof window.used_percent !== "number") return [];
                return [
                  {
                    usedPercent: normalizeCodexUsedPercent(window.used_percent),
                    windowMinutes: count(window.window_minutes),
                    resetsAt: count(window.resets_at),
                  },
                ];
              },
            ),
            credits: credits && {
              unlimited: credits.unlimited === true,
              balance: text(credits.balance),
            },
            reached: text(limit.reached),
          },
        ];
      },
    ),
    tokens: usage && {
      total: tokens(usage.total),
      last: tokens(usage.last),
      contextWindow: count(usage.context_window),
    },
  };
}

export type CodexStatusRow = { key: string; label: string; value: string };
export type CodexStatusSection = {
  key: string;
  title: string;
  rows: CodexStatusRow[];
  /** Quota windows, drawn as bars. */
  meters?: { key: string; label: string; usedPercent: number; resets?: string }[];
};

const ACCOUNT_KINDS: Record<string, string> = {
  chatgpt: "ChatGPT",
  apiKey: "API key",
  amazonBedrock: "Amazon Bedrock",
};

/** Section titles and rows the sheet draws, in the order Codex's /status shows them. */
export function codexStatusSections(
  status: CodexStatus,
  now: Date = new Date(),
): CodexStatusSection[] {
  const sections: CodexStatusSection[] = [];
  const model = status.thread?.model ?? status.config?.model;
  const effort = status.thread?.effort ?? status.config?.effort;
  const session: CodexStatusRow[] = [];
  if (model) {
    session.push(row("model", "Model", effort ? `${model} (${effort})` : model));
  }
  if (status.config?.summary) {
    session.push(row("summary", "Reasoning summary", status.config.summary));
  }
  if (status.config?.serviceTier) {
    session.push(row("tier", "Service tier", status.config.serviceTier));
  }
  if (status.thread?.cwd) session.push(row("cwd", "Directory", status.thread.cwd));
  if (status.config?.approval) {
    session.push(row("approval", "Approvals", status.config.approval));
  }
  if (status.config?.sandbox) {
    session.push(row("sandbox", "Sandbox", status.config.sandbox));
  }
  if (status.thread?.cliVersion) {
    session.push(row("version", "Codex version", status.thread.cliVersion));
  }
  if (status.thread?.id) session.push(row("thread", "Session", status.thread.id));
  if (session.length > 0) {
    sections.push({ key: "session", title: "Session", rows: session });
  }

  if (status.account) {
    const kind = ACCOUNT_KINDS[status.account.kind] ?? status.account.kind;
    const account: CodexStatusRow[] = [];
    if (kind) account.push(row("kind", "Signed in with", kind));
    if (status.account.email) account.push(row("email", "Account", status.account.email));
    if (status.account.plan) account.push(row("plan", "Plan", status.account.plan));
    if (account.length > 0) {
      sections.push({ key: "account", title: "Account", rows: account });
    }
  }

  const usage = status.tokens;
  if (usage && usage.total.total > 0) {
    const rows = [
      row("total", "Tokens used", formatTokenCount(usage.total.total)),
      row(
        "split",
        "Input / output",
        `${formatTokenCount(usage.total.input)} / ${formatTokenCount(usage.total.output)}`,
      ),
    ];
    const window = usage.contextWindow ?? status.config?.contextWindow;
    if (window && usage.last.total > 0) {
      const left = Math.max(0, Math.round(100 - (usage.last.total / window) * 100));
      rows.push(
        row(
          "context",
          "Context window",
          `${left}% left (${formatTokenCount(usage.last.total)} of ${formatTokenCount(window)})`,
        ),
      );
    }
    sections.push({ key: "tokens", title: "Tokens", rows });
  }

  for (const limit of status.limits) {
    const meters = limit.windows.map((window, index) => ({
      key: `${limit.id ?? limit.name ?? "limit"}:${index}`,
      label: codexWindowLabel(window.windowMinutes),
      usedPercent: window.usedPercent,
      resets: window.resetsAt ? formatReset(window.resetsAt, now) : undefined,
    }));
    const rows: CodexStatusRow[] = [];
    if (limit.credits) {
      rows.push(
        row(
          "credits",
          "Credits",
          limit.credits.unlimited ? "Unlimited" : (limit.credits.balance ?? "Available"),
        ),
      );
    }
    if (limit.reached) rows.push(row("reached", "Limit reached", limit.reached));
    sections.push({
      key: `limits:${limit.id ?? limit.name ?? sections.length}`,
      title: limit.name ? `${limit.name} limits` : "Usage limits",
      rows,
      meters,
    });
  }
  return sections;
}

function row(key: string, label: string, value: string): CodexStatusRow {
  return { key, label, value };
}

export function formatTokenCount(value: number): string {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 1_000) return `${(value / 1_000).toFixed(value >= 100_000 ? 0 : 1)}K`;
  return String(value);
}

/** "resets 14:05" within a day, "resets 25 Oct 14:05" beyond. */
export function formatReset(unixSeconds: number, now: Date): string {
  const at = new Date(unixSeconds * 1000);
  if (!Number.isFinite(at.getTime())) return "";
  const time = at.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  if (Math.abs(at.getTime() - now.getTime()) < 24 * 60 * 60 * 1000) {
    return `resets ${time}`;
  }
  const day = at.toLocaleDateString([], { day: "numeric", month: "short" });
  return `resets ${day} ${time}`;
}
