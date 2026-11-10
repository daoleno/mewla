import type { Daemon } from "../more/daemon";
import { invalidProviderReply, offlineProviderError, ProviderError, PROVIDER_ERROR_CODES, providerErrorFromPayload } from "./errors";
import { parseProviderConnectionTestResult, parseProviderModelsResult, parseProvidersSnapshot } from "./parse";
import { parseOptionalMutationPersistence, requireAppliedPersistence } from "./persistence";
import { ambiguousProviderMutation } from "./errors";
import type { ProviderConnectionInput, ProviderConnectionTestResult, ProviderModelsResult, ProvidersMutationResult, ProvidersSnapshot } from "./types";

/**
 * The Providers requests of the current computer, each settling with a ProviderError
 * (its kind decides the page's words), never a raw parse failure. A credential goes
 * out once, inside its request, and is never read back.
 */

function asProviderError(error: unknown, credentialWrite = false): ProviderError {
  if (error instanceof ProviderError) return error;
  const message = error instanceof Error ? error.message : String(error ?? "");
  if (/not connected/i.test(message)) return offlineProviderError();
  if (/timed out/i.test(message)) return new ProviderError(PROVIDER_ERROR_CODES.timeout, "Timed out waiting for Providers.", "timeout", true);
  const code = (error as { code?: unknown } | null)?.code;
  return providerErrorFromPayload({ code: typeof code === "string" ? code : undefined, message }, { credentialWrite });
}

function parsed<T>(parse: () => T | null, invalid: string): T {
  try {
    const value = parse();
    if (value === null || value === undefined) throw invalidProviderReply(invalid);
    return value;
  } catch (error) {
    throw error instanceof ProviderError ? error : invalidProviderReply(error instanceof Error ? error.message : invalid);
  }
}

async function catalog(daemon: Daemon, type: string, body: Record<string, unknown>, isList: boolean, credentialWrite = false): Promise<ProvidersMutationResult> {
  if (daemon.state !== "connected") throw offlineProviderError();
  const frame = await daemon.request<Record<string, unknown>>(type, body, { reply: "providers", timeoutMs: 15_000 }).catch((error) => {
    throw asProviderError(error, credentialWrite);
  });
  return parsed(() => {
    const snapshot = parseProvidersSnapshot(frame);
    if (!snapshot) throw invalidProviderReply("Daemon returned an invalid Providers catalog.");
    const persistence = isList
      ? (parseOptionalMutationPersistence(frame) ?? { applied: true, durable: true, outcome: "applied" })
      : requireAppliedPersistence(frame);
    if (isList && persistence.ambiguous) throw ambiguousProviderMutation(persistence.warning);
    return { snapshot, catalog: snapshot, persistence };
  }, "Daemon returned an invalid Providers payload.");
}

export async function listProviders(daemon: Daemon): Promise<ProvidersSnapshot> {
  return (await catalog(daemon, "list_providers", {}, true)).snapshot;
}

/** Creates or updates a connection; a non-empty key is written with it, atomically. */
export function upsertProviderConnection(
  daemon: Daemon,
  input: { connection: ProviderConnectionInput; revision: number; operation: "create" | "update"; credential?: string },
): Promise<ProvidersMutationResult> {
  const body: Record<string, unknown> = { provider_connection: input.connection, revision: input.revision, operation: input.operation };
  const credential = input.credential?.trim() ?? "";
  if (credential) body.credential = credential;
  return catalog(daemon, "upsert_provider_connection", body, false, Boolean(credential));
}

export function deleteProviderConnection(daemon: Daemon, connectionId: string, revision: number): Promise<ProvidersMutationResult> {
  return catalog(daemon, "delete_provider_connection", { connection_id: connectionId, revision }, false);
}

/** Routes an agent through a connection; "" is its own official sign-in. */
export function setProviderConnection(daemon: Daemon, input: { client: string; connectionId: string; revision: number }): Promise<ProvidersMutationResult> {
  return catalog(daemon, "set_provider_connection", { client: input.client, executor_id: input.client, connection_id: input.connectionId, revision: input.revision }, false);
}

export function setProviderModels(daemon: Daemon, connectionId: string, modelIds: string[]): Promise<ProvidersMutationResult> {
  return catalog(daemon, "set_provider_models", { connection_id: connectionId, model_ids: modelIds }, false);
}

export async function discoverProviderModels(daemon: Daemon, connectionId: string): Promise<ProviderModelsResult> {
  if (daemon.state !== "connected") throw offlineProviderError();
  const frame = await daemon.request("discover_provider_models", { connection_id: connectionId }, { reply: "provider_models", timeoutMs: 20_000 }).catch((error) => {
    throw asProviderError(error);
  });
  return parsed(() => parseProviderModelsResult(frame, connectionId), "Daemon returned invalid provider models.");
}

/** Tests an unsaved endpoint and key, or a saved connection by id. */
export async function testProviderConnection(
  daemon: Daemon,
  target: { connectionId: string; client: string } | { client: "codex" | "claude"; baseUrl: string; apiKey: string },
): Promise<ProviderConnectionTestResult> {
  if (daemon.state !== "connected") throw offlineProviderError();
  const unsaved = "baseUrl" in target;
  if (unsaved && (!target.baseUrl.trim() || !target.apiKey.trim())) {
    throw new ProviderError(PROVIDER_ERROR_CODES.invalid, "Enter a Base URL and API key.", "credential", false);
  }
  const body = unsaved
    ? { provider_connection: { preset_id: "custom", client: target.client, base_url: target.baseUrl.trim(), advanced: true }, credential: target.apiKey.trim() }
    : { connection_id: target.connectionId };
  const frame = await daemon.request("test_provider_connection", body, { reply: "provider_connection_test", timeoutMs: 20_000 }).catch((error) => {
    throw asProviderError(error, unsaved);
  });
  return parsed(() => parseProviderConnectionTestResult(frame, target.client), "Daemon returned an invalid connection test result.");
}
