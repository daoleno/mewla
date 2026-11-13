import type { BrandMark } from "../more/colour";
import { providerBaseUrlHostname } from "./presentation";
import type { ProviderClient, ProviderConnection } from "./types";

/**
 * Model providers' leading icons (soft-final SPEC §10.4 "Page by page"): a provider row is its vendor's mark, an
 * agent's own vendor its agent logo (Anthropic is Claude's, DeepSeek DSH's), and anything else the key on the
 * neutral tile. The daemon's presets are openai, openrouter, anthropic, deepseek and custom; a custom connection
 * (Bedrock, Vertex AI, Azure…) is known by its endpoint's host, which is never shown for it here.
 */
export type ProviderLead = { kind: "mark"; mark: BrandMark } | { kind: "agent"; agent: "claude" | "dsh" } | { kind: "tile"; glyph: "key" | "person-circle" };

const PRESETS: Record<string, ProviderLead> = {
  openai: { kind: "mark", mark: "openai" },
  openrouter: { kind: "mark", mark: "openrouter" },
  anthropic: { kind: "agent", agent: "claude" },
  deepseek: { kind: "agent", agent: "dsh" },
};

/** Endpoint hosts by the domain they end with, most specific first. */
const HOSTS: [string, ProviderLead][] = [
  ["aiplatform.googleapis.com", { kind: "mark", mark: "vertex" }],
  ["amazonaws.com", { kind: "mark", mark: "bedrock" }],
  ["openai.azure.com", { kind: "mark", mark: "azure" }],
  ["cognitiveservices.azure.com", { kind: "mark", mark: "azure" }],
  ["services.ai.azure.com", { kind: "mark", mark: "azure" }],
  ["models.ai.azure.com", { kind: "mark", mark: "azure" }],
  ["openrouter.ai", { kind: "mark", mark: "openrouter" }],
  ["api.openai.com", { kind: "mark", mark: "openai" }],
  ["api.anthropic.com", { kind: "agent", agent: "claude" }],
  ["api.deepseek.com", { kind: "agent", agent: "dsh" }],
];

const KEY: ProviderLead = { kind: "tile", glyph: "key" };

export function providerLead(c: Pick<ProviderConnection, "preset_id" | "base_url">): ProviderLead {
  const preset = c.preset_id ? PRESETS[c.preset_id] : undefined;
  if (preset) return preset;
  if (!c.base_url) return KEY;
  const host = providerBaseUrlHostname(c.base_url).toLowerCase();
  // Bedrock's runtime is one of many amazonaws.com hosts: only a bedrock host is Bedrock. Vertex AI's regional
  // hosts put the region before a hyphen (us-central1-aiplatform.googleapis.com).
  for (const [domain, lead] of HOSTS) {
    const regional = domain === "aiplatform.googleapis.com" && host.endsWith(`-${domain}`);
    if (host !== domain && !host.endsWith(`.${domain}`) && !regional) continue;
    if (domain === "amazonaws.com" && !host.split(".").some((part) => part.startsWith("bedrock"))) continue;
    return lead;
  }
  return KEY;
}

/** The Official login row: the account the agent signs in with (Codex's is OpenAI's, Claude's is Claude's). */
export function officialLoginLead(client: ProviderClient): ProviderLead {
  if (client === "codex") return { kind: "mark", mark: "openai" };
  if (client === "claude") return { kind: "agent", agent: "claude" };
  return { kind: "tile", glyph: "person-circle" };
}
