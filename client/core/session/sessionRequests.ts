/**
 * The Session screen's requests for its sheets: the Model sheet (get/set the
 * thread runtime, the Providers catalog) and the pictures a reply shows. From the
 * Expo app's services/websocket.ts (getThreadRuntime, setThreadRuntime) and
 * useSessionProviderSheet.ts (the dsh catalog).
 *
 * Each takes the current computer as a `Daemon`; the session's Transport is one.
 * Provider requests settle with a `ProviderError` (core/providers/errors).
 */
import type { Daemon } from "../more/daemon";
import { PICTURE_READ_MS } from "../timeline/pictureRetry";
import {
  invalidProviderReply,
  offlineProviderError,
  ProviderError,
  PROVIDER_ERROR_CODES,
  providerErrorFromPayload,
} from "../providers/errors";
import { assertThreadRuntimeMatches, parseThreadRuntimeSelection } from "../providers/parse";
import { requireAppliedPersistence } from "../providers/persistence";
import { listProviders } from "../providers/requests";
import { isAgentModelRuntime } from "../providers/types";
import type {
  ProvidersSnapshot,
  ThreadRuntimeChoice,
  ThreadRuntimeMutationResult,
  ThreadRuntimeSelection,
} from "../providers/types";
import { sessionAllowsModelProfileActivation, type WorkerSessionCapabilities } from "../timeline/sessionCapabilities";

export { listProviders };

type Reply = Record<string, unknown>;

/** The Session lets the person switch its model: it advertises a managed Model that switches live. */
export function canSwitchModel(capabilities?: WorkerSessionCapabilities | null): boolean {
  return sessionAllowsModelProfileActivation(capabilities);
}

function isTimeout(error: unknown): boolean {
  return error instanceof Error && /timed out$/i.test(error.message);
}

function providerFailure(error: unknown, timeoutMessage: string): ProviderError {
  if (error instanceof ProviderError) return error;
  const message = error instanceof Error ? error.message : String(error ?? "");
  if (/not connected/i.test(message)) return offlineProviderError();
  if (isTimeout(error)) return new ProviderError(PROVIDER_ERROR_CODES.timeout, timeoutMessage, "timeout", true);
  const code = (error as { code?: unknown } | null)?.code;
  return providerErrorFromPayload({ code: typeof code === "string" ? code : undefined, message });
}

function parsedProvider<T>(parse: () => T | null, invalid: string): T {
  try {
    const value = parse();
    if (value === null || value === undefined) throw invalidProviderReply(invalid);
    return value;
  } catch (error) {
    throw error instanceof ProviderError ? error : invalidProviderReply(error instanceof Error ? error.message : invalid);
  }
}

/** The Session's current model, effect and connection (`get_thread_runtime`). */
export async function getThreadRuntime(daemon: Daemon, workerId: string): Promise<ThreadRuntimeSelection> {
  if (daemon.state !== "connected") throw offlineProviderError();
  const frame = await daemon
    .request<Reply>("get_thread_runtime", { worker_id: workerId }, { reply: "thread_runtime", timeoutMs: 15_000 })
    .catch((error) => {
      throw providerFailure(error, "Timed out while loading session provider.");
    });
  return parsedProvider(
    () => parseThreadRuntimeSelection(frame.runtime, workerId),
    "Daemon returned an invalid session provider selection.",
  );
}

/**
 * The catalog the Model sheet lists for a Session: the agent's own models when
 * its runtime carries them, otherwise the computer's Providers.
 */
export async function threadRuntimeCatalog(daemon: Daemon, selection: ThreadRuntimeSelection): Promise<ProvidersSnapshot> {
  if (isAgentModelRuntime(selection)) {
    return {
      revision: 0,
      presets: [],
      defaults: {},
      connections: [
        {
          id: selection.connection_id,
          name: selection.connection_name,
          preset_id: `${selection.client}_native`,
          clients: [selection.client],
          credential_ready: selection.credential_ready,
          advanced: false,
        },
      ],
      models: { [selection.connection_id]: selection.native_models ?? [] },
    };
  }
  return listProviders(daemon);
}

/**
 * Switches the Session's model (and effect) live (`set_thread_runtime`). The reply
 * must say it applied and echo the runtime asked for, or it fails as invalid.
 */
export async function setThreadRuntime(
  daemon: Daemon,
  workerId: string,
  runtime: ThreadRuntimeChoice,
): Promise<ThreadRuntimeMutationResult> {
  if (daemon.state !== "connected") throw offlineProviderError();
  const effect = runtime.effect?.trim() ?? "";
  const frame = await daemon
    .request<Reply>(
      "set_thread_runtime",
      {
        worker_id: workerId,
        runtime: {
          connection_id: runtime.connectionId,
          model_id: runtime.modelId,
          ...(effect ? { effect } : {}),
          ...(runtime.useDefaultEffect ? { use_default_effect: true } : {}),
        },
      },
      { reply: "thread_runtime_set", timeoutMs: 20_000 },
    )
    .catch((error) => {
      throw providerFailure(error, "Timed out while switching model.");
    });
  return parsedProvider(() => {
    const persistence = requireAppliedPersistence(frame);
    const selection = parseThreadRuntimeSelection(frame.runtime, workerId);
    if (!selection || !assertThreadRuntimeMatches(selection, { workerId, runtime })) {
      throw invalidProviderReply("Daemon returned an invalid activation selection.");
    }
    return { runtime: selection, persistence };
  }, "Invalid activation payload.");
}

/**
 * An image a reply shows by its path on the computer (`codex_asset`): a data URL, or null when the computer
 * says it is no image it can show (missing, too large, not a picture). It throws when it could not ask or no
 * answer came (offline, the link dropped, timed out): that read is worth asking again.
 */
export async function codexAsset(daemon: Daemon, path: string, cwd?: string, maxEdge?: number): Promise<string | null> {
  if (daemon.state !== "connected") throw new Error("Daemon is not connected.");
  let frame: Reply;
  try {
    frame = await daemon.request<Reply>("codex_asset", { path, ...(cwd ? { cwd } : {}), ...(maxEdge ? { max_edge: maxEdge } : {}) }, { reply: "codex_asset", timeoutMs: PICTURE_READ_MS });
  } catch (error) {
    if ((error as { code?: string })?.code === "codex_asset_failed") return null;
    throw error;
  }
  const url = typeof frame.data_url === "string" ? frame.data_url : "";
  return url.startsWith("data:image/") ? url : null;
}
