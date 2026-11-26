/**
 * Secret-free App DTOs for the daemon's public Provider wire.
 *
 * Credential values intentionally have no representation in any reply,
 * snapshot, connection, or session-selection type.
 */

export const PROVIDER_CLIENTS = ["codex", "claude"] as const;
export type ProviderClient = (typeof PROVIDER_CLIENTS)[number];

export type ProviderConnection = {
  id: string;
  slug?: string;
  name: string;
  preset_id?: string;
  clients: string[];
  credential_ready: boolean;
  advanced: boolean;
  /** Advanced/Custom only. Curated presentation must never render this. */
  base_url?: string;
  /** Advanced/Custom only. Curated presentation must never render this. */
  manual_model_id?: string;
  /**
   * Conservative masked preview of the active stored secret (bounded
   * prefix/suffix, fixed bullet center), daemon-generated from the private
   * credential store. Presentation only: the editable API-key input stays
   * logically empty and this value is never submitted as a credential.
   */
  credential_hint?: string;
  models_fetched_at?: string;
  models_stale?: boolean;
  models_warning?: string;
  /** The connection that takes this one's requests while it fails. */
  fallback_connection_id?: string;
  /** The API its requests go out in: anthropic_messages, openai_chat or openai_responses. */
  upstream_api?: string;
};

export type ProviderPreset = {
  id: string;
  label: string;
  clients: string[];
  advanced: boolean;
};

export type ProviderConnectionSelection = {
  connection_id: string;
};

export type ProviderModel = {
  id: string;
  display_name?: string;
  available: boolean;
  source: string;
  reasoning_effort_default?: string;
  reasoning_efforts?: string[];
  context_window_tokens?: number;
  modalities?: string[];
  temperature_supported?: boolean;
  input_price_per_million?: number;
  output_price_per_million?: number;
  release_date?: string;
  metadata_source?: string;
  metadata_fetched_at?: string;
  /**
   * Whether daemon-owned display/effect metadata exists for this model.
   * Unknown gateway-only models remain selectable opaque identities.
   */
  known?: boolean;
  /** The agent always runs one of `reasoning_efforts` (Pi): no "Default" is offered. */
  reasoning_effort_required?: boolean;
};

export type ProvidersSnapshot = {
  revision: number;
  connections: ProviderConnection[];
  defaults: Record<string, ProviderConnectionSelection>;
  presets: ProviderPreset[];
  models: Record<string, ProviderModel[]>;
  gateway?: GatewayStatus;
};

export type GatewayStatus = {
  running: boolean;
  address?: string;
  endpoint?: string;
  protocols: string[];
  model_count: number;
};

export type ProviderConnectionInput = {
  id?: string;
  name?: string;
  preset_id: string;
  client: ProviderClient | string;
  base_url?: string;
  /** Optional exact catalog identity for advanced/manual catalog seeding. */
  model_id?: string;
  advanced?: boolean;
};

export type ThreadRuntimeSelection = {
  native_models?: ProviderModel[];
  session_id: string;
  client: string;
  connection_id: string;
  connection_name: string;
  provider_label?: string;
  model_id: string;
  /**
   * Daemon-owned Reasoning Effort override for this Session ("" = none; the
   * model/CLI default applies). Terminal and Interface project the same value.
   */
  reasoning_effort?: string;
  /** Documented model default effort (present only when the model has an effort contract). */
  reasoning_effort_default?: string;
  /** Supported effort values of the Session's model (present only when configurable). */
  reasoning_efforts?: string[];
  credential_ready: boolean;
  hot_switchable: boolean;
  /**
   * A model (and effort) the person picked that the agent takes at its next
   * possible moment (the end of the turn it is in); `model_id` stays what it
   * runs on until then.
   */
  pending_model_id?: string;
  pending_reasoning_effort?: string;
  /**
   * A pick held for the end of a turn that the agent did not take then, and
   * the agent's reason; it stays until the next pick.
   */
  failed_model_id?: string;
  failed_reasoning_effort?: string;
  failed_reason?: string;
};

export type ProviderModelsResult = {
  connectionId: string;
  models: ProviderModel[];
  discoveryWarning?: string;
  persistenceWarning?: string;
  persistenceDurable?: boolean;
};

export type ProviderConnectionTestResult = {
  client: ProviderClient;
  modelCount: number;
  latencyMs: number;
};

export type ProvidersMutationResult = {
  snapshot: ProvidersSnapshot;
  catalog: ProvidersSnapshot;
  persistence: import("./persistence").MutationPersistence;
};

export type ThreadRuntimeChoice = {
  connectionId: string;
  modelId: string;
  effect?: string;
  useDefaultEffect?: boolean;
};

export type ThreadRuntimeMutationResult = {
  runtime: ThreadRuntimeSelection;
  persistence: import("./persistence").MutationPersistence;
};

export type CreateSessionResult = {
  workerId: string;
  /** Present only when the daemon included persistence_* fields. */
  persistence?: import("./persistence").MutationPersistence;
};

export function normalizeProviderId(
  value: string | null | undefined,
): string {
  return (value ?? "").trim();
}

export function normalizeProviderClient(
  value: string | null | undefined,
): string {
  return normalizeProviderId(value).toLowerCase();
}

/**
 * A Session's runtime that carries its agent's own models (`native_models`):
 * the agent lists and switches them itself, whichever agent it is. Otherwise
 * the Session runs on one of the computer's Providers.
 */
export function isAgentModelRuntime(selection: Pick<ThreadRuntimeSelection, "native_models"> | null | undefined): boolean {
  return Array.isArray(selection?.native_models);
}

export function isSupportedProviderClient(
  value: string | null | undefined,
): value is ProviderClient {
  const client = normalizeProviderClient(value);
  return client === "codex" || client === "claude";
}
