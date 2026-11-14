import { describe, expect, test } from "bun:test";
import { officialLoginLead, providerLead } from "./lead";

describe("Model providers' marks (SPEC §10.4)", () => {
  test("a preset is its vendor's mark, or its agent's logo", () => {
    expect(providerLead({ preset_id: "openai" })).toEqual({ kind: "mark", mark: "openai" });
    expect(providerLead({ preset_id: "openrouter" })).toEqual({ kind: "mark", mark: "openrouter" });
    expect(providerLead({ preset_id: "anthropic" })).toEqual({ kind: "agent", agent: "claude" });
    expect(providerLead({ preset_id: "deepseek" })).toEqual({ kind: "agent", agent: "dsh" });
  });

  test("a custom endpoint is known by its host", () => {
    expect(providerLead({ preset_id: "custom", base_url: "https://bedrock-runtime.us-east-1.amazonaws.com" })).toEqual({ kind: "mark", mark: "bedrock" });
    expect(providerLead({ preset_id: "custom", base_url: "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/us-central1/endpoints/openapi" })).toEqual({ kind: "mark", mark: "vertex" });
    expect(providerLead({ preset_id: "custom", base_url: "https://acme.openai.azure.com/openai/v1" })).toEqual({ kind: "mark", mark: "azure" });
    expect(providerLead({ preset_id: "custom", base_url: "https://acme.services.ai.azure.com/models" })).toEqual({ kind: "mark", mark: "azure" });
    expect(providerLead({ base_url: "https://openrouter.ai/api/v1" })).toEqual({ kind: "mark", mark: "openrouter" });
    expect(providerLead({ base_url: "https://api.openai.com/v1" })).toEqual({ kind: "mark", mark: "openai" });
  });

  test("anything else is the key on the neutral tile, never a guessed mark", () => {
    expect(providerLead({ preset_id: "custom", base_url: "https://llm.internal.example/v1" })).toEqual({ kind: "tile", glyph: "key" });
    // Another AWS service, or a look-alike domain, is not Bedrock or OpenAI.
    expect(providerLead({ base_url: "https://s3.amazonaws.com" })).toEqual({ kind: "tile", glyph: "key" });
    expect(providerLead({ base_url: "https://api.openai.com.evil.example/v1" })).toEqual({ kind: "tile", glyph: "key" });
    expect(providerLead({ preset_id: "custom" })).toEqual({ kind: "tile", glyph: "key" });
  });

  test("Official login is the account the agent signs in with", () => {
    expect(officialLoginLead("codex")).toEqual({ kind: "mark", mark: "openai" });
    expect(officialLoginLead("claude")).toEqual({ kind: "agent", agent: "claude" });
  });
});
