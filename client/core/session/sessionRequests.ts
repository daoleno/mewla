/**
 * The Session screen's requests for its sheets and composer: the Model sheet
 * (get/set the thread runtime, the Providers catalog), Codex's /status, its
 * skills and its slash commands. From the Expo app's services/websocket.ts
 * (getThreadRuntime, setThreadRuntime, getCodexStatus, getCodexSkills,
 * getCodexSlashCommands) and useSessionProviderSheet.ts (the dsh catalog).
 *
 * Each takes the current computer as a `Daemon`; the session's Transport is one.
 * Provider requests settle with a `ProviderError` (core/providers/errors), the
 * Codex ones with an `Error` carrying the daemon's `code` when it gave one.
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
import type {
  ProvidersSnapshot,
  ThreadRuntimeChoice,
  ThreadRuntimeMutationResult,
  ThreadRuntimeSelection,
} from "../providers/types";
import { sessionAllowsModelProfileActivation, type WorkerSessionCapabilities } from "../timeline/sessionCapabilities";
import { normalizeCodexStatus, type CodexStatus } from "./codexStatus";
import {
  buildChatuiSlashCommands,
  type CodexSlashCommand,
  type CodexSlashCommandInput,
  type CodexSlashCommandOutput,
  type CodexSlashCommandSnapshot,
} from "./codexSlashCommands";

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
 * The catalog the Model sheet lists for a Session: a dsh Session's own models
 * (its runtime carries them), otherwise the computer's Providers.
 */
export async function threadRuntimeCatalog(daemon: Daemon, selection: ThreadRuntimeSelection): Promise<ProvidersSnapshot> {
  if (selection.client === "dsh") {
    return {
      revision: 0,
      presets: [],
      defaults: {},
      connections: [
        {
          id: selection.connection_id,
          name: selection.connection_name,
          preset_id: "dsh_native",
          clients: ["dsh"],
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

function codexFailure(error: unknown, fallback: string, timeoutMessage: string): Error & { code?: string } {
  if (isTimeout(error)) return Object.assign(new Error(timeoutMessage), { code: "timeout" });
  if (error instanceof Error && /not connected/i.test(error.message)) return Object.assign(new Error("Not connected to the server."), { code: "offline" });
  const failure: Error & { code?: string } = new Error((error instanceof Error && error.message) || fallback);
  const code = (error as { code?: unknown } | null)?.code;
  if (typeof code === "string") failure.code = code;
  return failure;
}

/** The code `codexStatus` fails with when Mewla did not start this chat's Codex: there is no status to read. */
export const CODEX_STATUS_UNAVAILABLE = "codex_status_unavailable";

/** What Codex's own app-server reports about the chat (`codex_status`), for the status sheet. */
export async function codexStatus(daemon: Daemon, workerId: string): Promise<CodexStatus> {
  if (daemon.state !== "connected") throw Object.assign(new Error("Not connected to the server."), { code: "offline" });
  const frame = await daemon
    .request<Reply>("codex_status", { target_id: workerId }, { reply: "codex_status", timeoutMs: 20_000 })
    .catch((error) => {
      throw codexFailure(error, "Failed to load Codex status.", "Timed out while loading Codex status.");
    });
  return normalizeCodexStatus(frame.status);
}

export interface CodexSkill {
  name: string;
  description?: string;
  path: string;
  scope: string;
  enabled: boolean;
}

export interface CodexSkillsSnapshot {
  cwd?: string;
  skills: CodexSkill[];
}

/** The skills Codex has for a folder (`codex_skills`); a skill without a name or path is dropped. */
export async function codexSkills(daemon: Daemon, cwd?: string): Promise<CodexSkillsSnapshot> {
  if (daemon.state !== "connected") throw Object.assign(new Error("Not connected to the server."), { code: "offline" });
  const frame = await daemon
    .request<Reply>("codex_skills", cwd ? { cwd } : {}, { reply: "codex_skills", timeoutMs: 10_000 })
    .catch((error) => {
      throw codexFailure(error, "Failed to load Codex skills.", "Timed out while loading Codex skills.");
    });
  const skills = Array.isArray(frame.skills)
    ? (frame.skills as unknown[]).flatMap((raw) => {
        const skill = (raw && typeof raw === "object" ? raw : {}) as Record<string, unknown>;
        const parsed: CodexSkill = {
          name: typeof skill.name === "string" ? skill.name : "",
          description: typeof skill.description === "string" ? skill.description : undefined,
          path: typeof skill.path === "string" ? skill.path : "",
          scope: typeof skill.scope === "string" && skill.scope ? skill.scope : "user",
          enabled: typeof skill.enabled === "boolean" ? skill.enabled : true,
        };
        return parsed.name && parsed.path ? [parsed] : [];
      })
    : [];
  return { cwd: typeof frame.cwd === "string" ? frame.cwd : cwd, skills };
}

/** The skills a search matches, by name, description, scope or path. */
export function filterCodexSkills(skills: CodexSkill[], query: string): CodexSkill[] {
  const normalized = query.trim().toLowerCase();
  if (!normalized) return skills;
  return skills.filter((skill) =>
    [skill.name, skill.description || "", skill.scope, skill.path].join(" ").toLowerCase().includes(normalized),
  );
}

function commandInput(value: unknown): CodexSlashCommandInput {
  const input = (value && typeof value === "object" ? value : {}) as Record<string, unknown>;
  return {
    kind: typeof input.kind === "string" && input.kind ? input.kind : "",
    placeholder: typeof input.placeholder === "string" ? input.placeholder : undefined,
    picker: typeof input.picker === "string" ? input.picker : undefined,
    required: typeof input.required === "boolean" ? input.required : undefined,
  };
}

function commandOutput(value: unknown): CodexSlashCommandOutput {
  const output = (value && typeof value === "object" ? value : {}) as Record<string, unknown>;
  return { kind: typeof output.kind === "string" && output.kind ? output.kind : "" };
}

/** Codex's slash commands as the daemon lists them (`codex_slash_commands`); one without a `/value` or name is dropped. */
export async function codexSlashCommands(daemon: Daemon): Promise<CodexSlashCommandSnapshot> {
  if (daemon.state !== "connected") throw Object.assign(new Error("Not connected to the server."), { code: "offline" });
  const frame = await daemon
    .request<Reply>("codex_slash_commands", {}, { reply: "codex_slash_commands", timeoutMs: 10_000 })
    .catch((error) => {
      throw codexFailure(error, "Failed to load Codex commands.", "Timed out while loading Codex commands.");
    });
  const commands = Array.isArray(frame.commands)
    ? (frame.commands as unknown[]).flatMap((raw) => {
        const command = (raw && typeof raw === "object" ? raw : {}) as Record<string, unknown>;
        const execution = typeof command.execution === "string" && command.execution ? command.execution : "";
        const parsed: CodexSlashCommand = {
          value: typeof command.value === "string" ? command.value : "",
          name: typeof command.name === "string" ? command.name : "",
          title: typeof command.title === "string" ? command.title : "",
          description: typeof command.description === "string" ? command.description : "",
          source: typeof command.source === "string" ? command.source : undefined,
          category: typeof command.category === "string" && command.category ? command.category : "",
          execution,
          input: commandInput(command.input),
          output: commandOutput(command.output),
          interactive: Boolean(command.interactive),
          chat_supported: Boolean(command.chat_supported),
          terminal_supported: typeof command.terminal_supported === "boolean" ? command.terminal_supported : execution !== "unsupported",
        };
        return parsed.value.startsWith("/") && parsed.name ? [parsed] : [];
      })
    : [];
  return { source: typeof frame.source === "string" ? frame.source : undefined, commands };
}

/** The command list the chat composer offers: the daemon's, with ChatUI's overrides and hidden commands applied. */
export async function chatSlashCommands(daemon: Daemon): Promise<CodexSlashCommand[]> {
  return buildChatuiSlashCommands((await codexSlashCommands(daemon)).commands);
}

/**
 * An image a reply shows by its path on the computer (`codex_asset`): a data URL, or null when the computer
 * says it is no image it can show (missing, too large, not a picture). It throws when it could not ask or no
 * answer came (offline, the link dropped, timed out): that read is worth asking again.
 */
export async function codexAsset(daemon: Daemon, path: string, cwd?: string): Promise<string | null> {
  if (daemon.state !== "connected") throw new Error("Daemon is not connected.");
  let frame: Reply;
  try {
    frame = await daemon.request<Reply>("codex_asset", { path, ...(cwd ? { cwd } : {}) }, { reply: "codex_asset", timeoutMs: PICTURE_READ_MS });
  } catch (error) {
    if ((error as { code?: string })?.code === "codex_asset_failed") return null;
    throw error;
  }
  const url = typeof frame.data_url === "string" ? frame.data_url : "";
  return url.startsWith("data:image/") ? url : null;
}
