import { invalidProviderReply } from "./errors";
import type {
  ProviderConnection,
  ProviderConnectionInput,
  ProvidersSnapshot,
} from "./types";
import {
  isSupportedProviderClient,
  normalizeProviderClient,
  normalizeProviderId,
} from "./types";

export function connectionsForClient(
  snapshot: ProvidersSnapshot,
  client: string,
): ProviderConnection[] {
  const normalized = normalizeProviderClient(client);
  return snapshot.connections.filter((connection) =>
    connection.clients.some(
      (candidate) => normalizeProviderClient(candidate) === normalized,
    ),
  );
}

/**
 * Turning a model off blocks it for every agent on the connection, including
 * Sessions using it right now, so only that direction asks first.
 */
export function modelSupportToggleTurnsOff(
  snapshot: ProvidersSnapshot | null | undefined,
  connectionId: string,
  modelId: string,
): boolean {
  return enabledModelIds(snapshot, connectionId).includes(
    normalizeProviderId(modelId),
  );
}

/**
 * The client-side model support allowlist of a connection as enabled ids in
 * catalog order (availability is the wire's enabled state).
 */
export function enabledModelIds(
  snapshot: ProvidersSnapshot | null | undefined,
  connectionId: string,
): string[] {
  return (snapshot?.models[connectionId] ?? [])
    .filter((model) => model.available)
    .map((model) => model.id);
}

/**
 * Toggle one model's support: returns the next full enabled set (catalog
 * order) after flipping modelId, preserving every other model's state.
 */
export function toggleModelSupport(
  snapshot: ProvidersSnapshot | null | undefined,
  connectionId: string,
  modelId: string,
): string[] {
  const current = new Set(enabledModelIds(snapshot, connectionId));
  const normalized = normalizeProviderId(modelId);
  if (!normalized) return enabledModelIds(snapshot, connectionId);
  if (current.has(normalized)) {
    current.delete(normalized);
  } else {
    current.add(normalized);
  }
  return (snapshot?.models[connectionId] ?? [])
    .map((model) => model.id)
    .filter((id) => current.has(id));
}

/**
 * Advanced/Custom create input. The App sends the selected product client and
 * gateway identity; Mewla derives protocol/auth compatibility internally.
 */
export function advancedConnectionInput(input: {
  existingId?: string;
  name: string;
  client: string;
  baseUrl: string;
  presetId?: string;
  /** Optional explicit upstream model id (discovery-driven when omitted). */
  modelId?: string;
  /**
   * Advanced/custom gateways expose an editable Base URL. Curated edits stay
   * false so the daemon keeps the official endpoint; defaults to true for new
   * custom endpoints.
   */
  advanced?: boolean;
}): ProviderConnectionInput {
  const name = normalizeProviderId(input.name);
  const baseUrl = normalizeProviderId(input.baseUrl);
  const client = normalizeProviderClient(input.client);
  const advanced = input.advanced ?? true;
  if (!name) throw invalidProviderReply("Display name is required.");
  if (!isSupportedProviderClient(client)) {
    throw invalidProviderReply("Choose Codex or Claude Code.");
  }
  if (advanced) {
    if (!baseUrl) throw invalidProviderReply("Base URL is required.");
    let parsed: URL;
    try {
      parsed = new URL(baseUrl);
    } catch {
      throw invalidProviderReply("Enter a valid HTTP or HTTPS base URL.");
    }
    if (parsed.protocol !== "https:" && parsed.protocol !== "http:") {
      throw invalidProviderReply("Base URL must use HTTP or HTTPS.");
    }
  }
  return {
    id: normalizeProviderId(input.existingId) || undefined,
    name,
    preset_id: normalizeProviderId(input.presetId) || "custom",
    client,
    base_url: baseUrl || undefined,
    model_id: normalizeProviderId(input.modelId) || undefined,
    advanced,
  };
}

/**
 * Hostname-only secondary identity for Provider rows: the Base URL is a
 * routing attribute, never the display identity. Non-URL text falls back to
 * the trimmed input so malformed legacy data still renders safely.
 */
export function providerBaseUrlHostname(baseUrl: string): string {
  const normalized = normalizeProviderId(baseUrl);
  if (!normalized) return "";
  try {
    const parsed = new URL(normalized);
    return parsed.hostname || normalized;
  } catch {
    return normalized;
  }
}

/**
 * Inline display-name validation for the unified Add/Edit Provider form:
 * trimmed, non-empty, length-bounded and case-insensitively unique within the
 * current Provider list. Returns a user-facing message or null when the name
 * is acceptable. The daemon re-validates authoritatively on save.
 */
export function providerNameIssue(input: {
  name: string;
  snapshot: ProvidersSnapshot | null | undefined;
  exceptConnectionId?: string;
}): string | null {
  const name = normalizeProviderId(input.name);
  if (!name) return "Provider name is required.";
  if (name.length > 64) return "Provider name must be 64 characters or fewer.";
  if (input.snapshot) {
    const except = normalizeProviderId(input.exceptConnectionId);
    const clash = input.snapshot.connections.find(
      (connection) =>
        connection.id !== except &&
        connection.name.trim().toLowerCase() === name.toLowerCase(),
    );
    if (clash) {
      return `Another Provider is already named “${clash.name}”. Names must be unique.`;
    }
  }
  return null;
}

/**
 * A create reply must identify exactly one new connection before a credential
 * can be addressed. Never guess between same-preset connections.
 */
export function createdConnectionFromMutation(
  previous: ProvidersSnapshot,
  next: ProvidersSnapshot,
  presetId: string,
): ProviderConnection {
  const previousIds = new Set(previous.connections.map((item) => item.id));
  const added = next.connections.filter(
    (connection) =>
      !previousIds.has(connection.id) && connection.preset_id === presetId,
  );
  if (added.length !== 1) {
    throw invalidProviderReply(
      "Could not uniquely identify the new Provider. Refresh before adding an API key.",
    );
  }
  return added[0];
}
