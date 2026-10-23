import type { ConnectionRequest, ConnectionResponse } from "./connections";
import type { StoredServer } from "./storage";
import type { StatsPayload } from "./statsPayload";
import {
  isWorkerSessionFeedType,
  WorkerSessionFeed,
} from "./workerSessionFeed";
import { Platform } from "react-native";
import { buildAuthorizationHeader } from "./auth";
import { diagnoseConnectionIssue } from "./connectionIssue";
import { rememberDirectAddresses } from "./directAddressSync";
import { authSubprotocol } from "./webSocketAuth";
import {
  directPathBecameAvailable,
  resolveStoredServerURL,
  transportFailed,
} from "./pinnedTransport";
import type {
  GitDiffFileContentPayload,
  GitDiffPatchPayload,
  GitRepoBrowserPayload,
  GitRepoFileContentPayload,
  GitDiffStatusSnapshot,
} from "./gitDiff";
import type { SessionService, SessionServiceSnapshot } from "./sessionServices";
import {
  normalizeResourceTelemetry,
  type ResourceTelemetry,
} from "./resourceTelemetry";
import {
  normalizeCodexConversation,
  type CodexConversation,
} from "./codexConversation";
import {
  normalizeSessionFileMetadata,
  normalizeSessionFileText,
  type SessionFileMetadata,
  type SessionFileRequest,
  type SessionFileTextPreview,
} from "./sessionFilePreview";
import type { CalendarItem } from "../store/calendar";
import type { BrainWorkUserActionKind } from "../store/brain";
import {
  normalizeSkillsInspectDetail,
  normalizeSkillsInventory,
  normalizeSkillsMutationCommand,
  normalizeSkillsMutationResult,
  assertSkillsMutationMatchesRequest,
  assertSkillsCommandMatchesRequest,
  type PackageDetail,
  type SkillDeleteIdentity,
  type SkillsInventory,
  type SkillsMutationCommand,
  type SkillsMutationResult,
} from "./skillsManagement";
import {
  normalizePluginsInventory,
  normalizePluginMutationCommand,
  normalizePluginMutationResult,
  assertPluginCommandMatchesRequest,
  assertPluginMutationMatchesRequest,
  type PluginInventory,
  type PluginMutationCommand,
  type PluginMutationInput,
  type PluginMutationResult,
} from "./pluginsManagement";
import {
  PLUGINS_INVENTORY_TIMEOUT_MS,
  PLUGIN_COMMAND_TIMEOUT_MS,
  PLUGIN_MUTATION_TIMEOUT_MS,
  SKILLS_MUTATION_TIMEOUT_MS,
} from "./pluginsDeadlines";
import {
  dispatchStructuredCommand,
  sendWebSocketMessageNow,
  structuredActionMessage,
  type WorkerAction,
  structuredInputMessage,
  type SendAttachment,
  type StructuredCommandReceipt,
} from "./structuredWebSocketTransport";
import {
  ProviderError,
  PROVIDER_ERROR_CODES,
  ambiguousProviderMutation,
  assertThreadRuntimeMatches,
  classifyMutationPersistence,
  invalidProviderReply,
  newProviderRequestId,
  offlineProviderError,
  parseOptionalMutationPersistence,
  parseProviderCredentialResult,
  parseProviderConnectionTestResult,
  parseProviderModelsResult,
  parseThreadRuntimeSelection,
  parseProvidersSnapshot,
  providerErrorFromPayload,
  requireAppliedPersistence,
  type CreateSessionResult,
  type ThreadRuntimeMutationResult,
  type ProviderClient,
  type ProviderConnectionInput,
  type ProviderConnectionTestResult,
  type ProviderCredentialResult,
  type ProviderConnectionSelectionInput,
  type ProviderModelsResult,
  type ProviderSwitchInput,
  type ThreadRuntimeSelection,
  type ProvidersMutationResult,
  type ProvidersSnapshot,
} from "./providers";

type MessageHandler = (data: any) => void;

function normalizeCodexSlashCommandInput(value: any): CodexSlashCommandInput {
  const input = value && typeof value === "object" ? value : {};
  return {
    kind: typeof input.kind === "string" && input.kind ? input.kind : "",
    placeholder:
      typeof input.placeholder === "string" ? input.placeholder : undefined,
    picker: typeof input.picker === "string" ? input.picker : undefined,
    required: typeof input.required === "boolean" ? input.required : undefined,
  };
}

function normalizeCodexSlashCommandOutput(value: any): CodexSlashCommandOutput {
  const output = value && typeof value === "object" ? value : {};
  return {
    kind: typeof output.kind === "string" && output.kind ? output.kind : "",
  };
}

export interface CodexAssetPreview {
  path: string;
  content_type: string;
  data_url: string;
}

export interface CodexSlashCommand {
  value: string;
  name: string;
  title: string;
  description: string;
  source?: string;
  category: CodexSlashCommandCategory;
  execution: CodexSlashCommandExecution;
  input: CodexSlashCommandInput;
  output: CodexSlashCommandOutput;
  interactive: boolean;
  chat_supported: boolean;
  terminal_supported: boolean;
}

export type CodexSlashCommandCategory =
  | "session"
  | "navigation"
  | "settings"
  | "tools"
  | "management"
  | "debug"
  | "danger"
  | "unknown"
  | string;

export type CodexSlashCommandExecution =
  "terminal-required" | "insert-only" | "native" | "unsupported" | string;

export interface CodexSlashCommandInput {
  kind: "none" | "inline-args" | "form" | "picker" | "freeform" | string;
  placeholder?: string;
  picker?: string;
  required?: boolean;
}

export interface CodexSlashCommandOutput {
  kind:
    | "none"
    | "markdown"
    | "monospace-log"
    | "diff"
    | "status-card"
    | "management-screen"
    | "terminal"
    | string;
}

export interface CodexSlashCommandSnapshot {
  source?: string;
  commands: CodexSlashCommand[];
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

export interface BrainWorkspaceEntry {
  name: string;
  path: string;
  kind: "directory" | "file" | string;
  size?: number;
  modified_at?: string;
  children: BrainWorkspaceEntry[];
}

export interface BrainWorkspaceTree {
  workspace?: string;
  path?: string;
  generated_at?: string;
  entries: BrainWorkspaceEntry[];
}

export interface BrainWorkspaceFile {
  data_url?: string;
  name: string;
  path: string;
  kind: "file" | string;
  language: "markdown" | "text" | string;
  content: string;
  size?: number;
  modified_at?: string;
}

export interface BrainContextPayload {
  thread_id?: string;
  workspace?: string;
  worklog_path?: string;
  notes?: { path: string; bytes: number; budget_bytes: number; over_budget?: boolean }[];
  personality?: string;
  host_worker?: any;
  host_executor?: any;
  executors?: any[];
  workers?: any[];
  generated_at?: string;
}

export interface BrainHousekeepingPayload {
  workspace?: string;
  current_path?: string;
  notes?: { path: string; bytes: number; budget_bytes: number; over_budget?: boolean }[];
  unmanaged_paths?: string[];
  policy_paths?: string[];
  worklog_path?: string;
  open_delegated_workers?: any[];
  changed_paths?: string[];
  recommended_next_steps?: string[];
  generated_at?: string;
}

export type TelegramConnectionState =
  | "disabled"
  | "setup_pending"
  | "connected"
  | "degraded";

export interface TelegramConnectionStatus {
  state: TelegramConnectionState;
  enabled: boolean;
  bot_name?: string;
  bot_username?: string;
  owner_hint?: string;
  binding_pending: boolean;
  topics_available?: boolean;
  topic_notice?: string;
  topic_mappings?: number;
  recipient_id?: string;
  recipient_label?: string;
  brain_thread_id?: string;
  brain_topic_id?: number;
  users_create_topics?: boolean;
  topic_ambiguous_ops_count?: number;
  topic_failed_ops_count?: number;
  topic_failed_messages_count?: number;
  last_receive_at?: string;
  last_send_at?: string;
  last_error?: string;
  webhook_conflict?: boolean;
  ambiguous_delivery_count?: number;
}

export interface TelegramBindingChallenge {
  url: string;
  expires_at: string;
}

export interface CodexConversationSnapshotPayload {
  request_id?: string;
  worker_id?: string;
  conversation_id?: string;
  revision: number;
  server_generation?: string;
  conversation: CodexConversation;
}

export interface CodexConversationDeltaPayload {
  request_id?: string;
  worker_id?: string;
  conversation_id?: string;
  revision: number;
  base_revision: number;
  server_generation?: string;
  available?: boolean;
  reason?: string;
  source?: string;
  path?: string;
  session_id?: string;
  cwd?: string;
  updated_at?: string;
  activity?: CodexConversation["activity"] | null;
  upserts: CodexConversation["events"];
  deletes: string[];
}

export interface CodexConversationSyncStatusPayload {
  request_id?: string;
  worker_id?: string;
  conversation_id?: string;
  revision: number;
  server_generation?: string;
  state: "syncing" | "ready" | "unavailable" | string;
  reason?: string;
}

export interface CodexConversationSubscriptionOptions {
  targetId?: string;
  workerId?: string;
  cwd?: string;
  command?: string;
  name?: string;
  startedAt?: number;
  processId?: number;
  conversationScopeKey?: string;
}

export interface CodexConversationSubscriptionHandlers {
  onSnapshot(payload: CodexConversationSnapshotPayload): void;
  onDelta(payload: CodexConversationDeltaPayload): void;
  onSyncStatus(payload: CodexConversationSyncStatusPayload): void;
  onError(error: Error): void;
}

interface ConnectionMeta {
  serverId: string;
  serverName: string;
  serverUrl: string;
  daemonId: string;
  daemonPublicKey: string;
  server: StoredServer;
}

export class DaemonRequestError extends Error {
  readonly code: string | undefined;

  constructor(message: string, code?: string) {
    super(message);
    this.name = "DaemonRequestError";
    this.code = code;
  }
}

export function daemonRequestError(
  message: string,
  code?: string,
): DaemonRequestError {
  return new DaemonRequestError(message, code);
}

/** The app's foreground state, as the root layout last reported it. */
let appState = "unknown";

/**
 * The daemon sends a frame at least every 10 s and pings every 20 s. A socket
 * silent for longer than this is dead whatever its readyState says (a phone
 * that changed networks or woke from doze); it is replaced at once rather than
 * left for the OS to notice minutes later.
 */
export const socketWatchdog = { silentLimitMs: 25_000, checkMs: 5_000 };
/** How often a connection on the relay looks for a direct path. */
const DIRECT_UPGRADE_CHECK_MS = 20_000;
/** A connection that lived this long and then dropped reconnects without delay. */
const HEALTHY_CONNECTION_MS = 10_000;

class ServerSocket {
  private ws: WebSocket | null = null;
  private reconnectDelay = 1000;
  private readonly maxReconnectDelay = 30000;
  private shouldReconnect = true;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private attemptSequence = 0;
  /** When the live socket last delivered a frame (the daemon sends one at least every 10 s). */
  lastFrameAt: number | undefined;
  /** Ends the current socket now, whatever state it is in. */
  private endCurrent: ((reason: string, code?: number) => void) | null = null;
  private watchdog: ReturnType<typeof setInterval> | null = null;
  private upgradeProbe: ReturnType<typeof setInterval> | null = null;
  private probingForDirect = false;
  /** How the last socket ended, told to the daemon once the next one opens. */
  private pendingEndReport: string | null = null;
  /** Where this connection is in the daemon's sequenced Session feed. */
  private readonly workerFeed = new WorkerSessionFeed();

  constructor(
    private meta: ConnectionMeta,
    private emit: (type: string, payload: any) => void,
  ) {}

  updateMeta(server: StoredServer) {
    this.meta = toConnectionMeta(server);
  }

  connect() {
    this.shouldReconnect = true;
    this.reconnectDelay = 1000;
    this.startConnectAttempt();
  }

  disconnect() {
    this.shouldReconnect = false;
    this.attemptSequence += 1;

    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }

    this.emit("connection_issue", { issue: null });

    this.stopWatchdog();
    this.endCurrent = null;
    const ws = this.ws;
    this.ws = null;
    ws?.close();
  }

  /**
   * Ends a connection that is not answering and reconnects at once, without
   * waiting for the close handshake a dead link will never finish.
   */
  abandon(reason: string) {
    this.endCurrent?.(reason);
  }

  private startWatchdog() {
    this.stopWatchdog();
    this.watchdog = setInterval(() => {
      const quietFor = Date.now() - (this.lastFrameAt ?? 0);
      if (this.isConnected && quietFor > socketWatchdog.silentLimitMs) {
        this.abandon("silent");
      }
    }, socketWatchdog.checkMs);
    // A phone that joined Wi-Fi after connecting through the relay moves onto
    // the direct path with one quick reconnect.
    this.upgradeProbe = setInterval(() => {
      if (this.probingForDirect || !this.isConnected || appState === "background") {
        return;
      }
      this.probingForDirect = true;
      void directPathBecameAvailable(this.meta.server)
        .then((available) => {
          if (available && this.isConnected) this.abandon("direct-available");
        })
        .catch(() => undefined)
        .finally(() => {
          this.probingForDirect = false;
        });
    }, DIRECT_UPGRADE_CHECK_MS);
  }

  private stopWatchdog() {
    if (this.watchdog) {
      clearInterval(this.watchdog);
      this.watchdog = null;
    }
    if (this.upgradeProbe) {
      clearInterval(this.upgradeProbe);
      this.upgradeProbe = null;
    }
  }

  sendNow(msg: object) {
    sendWebSocketMessageNow(this.ws, msg);
  }

  trySendNow(msg: object) {
    try {
      this.sendNow(msg);
      return true;
    } catch {
      return false;
    }
  }

  get isConnected() {
    return this.ws?.readyState === WebSocket.OPEN;
  }

  private startConnectAttempt() {
    const attemptId = ++this.attemptSequence;
    this.emit("connecting", {});
    void this.doConnect(attemptId);
  }

  private scheduleReconnect(immediate = false) {
    if (!this.shouldReconnect) {
      return;
    }

    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
    }

    // A connection that was healthy and then dropped is retried at once; only
    // repeated failures back off.
    const delay = immediate ? 0 : this.reconnectDelay;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      if (!this.shouldReconnect) {
        return;
      }
      this.startConnectAttempt();
    }, delay);
    this.reconnectDelay = Math.min(
      this.reconnectDelay * 2,
      this.maxReconnectDelay,
    );
  }

  /** Skip backoff and reconnect now (e.g. app returned to foreground). */
  resumeReconnect() {
    if (!this.shouldReconnect || this.isConnected) {
      return;
    }
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.reconnectDelay = 1000;
    this.startConnectAttempt();
  }

  private async reportConnectionIssue(attemptId: number) {
    const issue = await diagnoseConnectionIssue({
      server: this.meta.server,
    });

    if (attemptId !== this.attemptSequence) {
      return;
    }
    if (!this.shouldReconnect) {
      return;
    }
    if (this.ws?.readyState === WebSocket.OPEN) {
      return;
    }

    this.emit("connection_issue", { issue });
  }

  private async doConnect(attemptId: number) {
    let opened = false;
    let openedAt = 0;

    try {
      const authHeader = await buildAuthorizationHeader({
        daemonId: this.meta.daemonId,
        purpose: "mewla-connect",
      });
      if (attemptId !== this.attemptSequence || !this.shouldReconnect) {
        return;
      }

      const wsOptions = { headers: { Authorization: authHeader } };
      const transportURL = await resolveStoredServerURL(this.meta.server);
      if (attemptId !== this.attemptSequence || !this.shouldReconnect) {
        return;
      }
      const WebSocketCtor = WebSocket as any;
      const ws =
        Platform.OS === "web"
          ? new WebSocketCtor(transportURL, [authSubprotocol(authHeader)])
          : new WebSocketCtor(transportURL, [], wsOptions);
      this.ws = ws;

      ws.onopen = () => {
        if (attemptId !== this.attemptSequence) {
          ws.close();
          return;
        }

        opened = true;
        this.workerFeed.reset();
        openedAt = Date.now();
        this.lastFrameAt = openedAt;
        this.reconnectDelay = 1000;
        this.startWatchdog();
        this.emit("connection_issue", { issue: null });
        this.emit("connected", {});
        if (this.pendingEndReport) {
          // Only the phone knows why a link ended; the daemon sees a bare 1006.
          this.trySendNow({
            type: "client_diagnostic",
            name: "ws_closed",
            body: this.pendingEndReport,
          });
          this.pendingEndReport = null;
        }
      };

      ws.onmessage = (event: any) => {
        // A replaced or ended socket can still deliver a late frame.
        if (attemptId !== this.attemptSequence) {
          return;
        }
        this.lastFrameAt = Date.now();
        try {
          const data = JSON.parse(event.data);
          if (data.type === "direct_addresses") {
            void rememberDirectAddresses(this.meta.serverId, data.addresses);
          }
          if (isWorkerSessionFeedType(data.type)) {
            const verdict = this.workerFeed.accept(data);
            if (verdict === "resync") {
              // A change went missing: the list is the only way back in step.
              if (!this.trySendNow({ type: "list_worker_sessions" })) {
                this.workerFeed.resyncNotSent();
              }
            }
            if (verdict !== "apply") {
              return;
            }
          }
          this.emit(data.type, data);
        } catch (error) {
          console.warn("[ws] malformed payload", {
            serverId: this.meta.serverId,
            dataType: typeof event?.data,
            error: error instanceof Error ? error.message : String(error),
            sample:
              typeof event?.data === "string"
                ? event.data.slice(0, 200)
                : String(event?.data),
          });
        }
      };

      // A socket ends once, whichever event reports it first.
      let ended = false;
      const handleEnd = (reason = "closed", code?: number) => {
        if (ended) {
          return;
        }
        ended = true;
        if (this.endCurrent === endThis) {
          this.endCurrent = null;
          this.stopWatchdog();
        }
        if (this.ws === ws) {
          this.ws = null;
        }
        if (attemptId !== this.attemptSequence) {
          return;
        }
        const lived = opened ? Date.now() - openedAt : 0;
        if (opened) {
          this.pendingEndReport =
            `reason=${reason} code=${code ?? ""} ` +
            `livedMs=${lived} ` +
            `quietMs=${Math.max(0, Date.now() - (this.lastFrameAt ?? Date.now()))} ` +
            `appState=${appState}`;
        }

        // Transient close (background suspension, network blip). Keep
        // reconnecting; UI should retain caches and show "connecting".
        this.emit("disconnected", { reason: "transport_closed" });
        if (this.shouldReconnect) {
          this.emit("connecting", {});
          // Moving to the direct path leaves the relay open as the fallback.
          if (reason !== "direct-available") {
            void transportFailed(this.meta.server);
          }
        }
        if (!opened) {
          void this.reportConnectionIssue(attemptId);
        }
        this.scheduleReconnect(lived >= HEALTHY_CONNECTION_MS);
      };
      const endThis = (reason: string, code?: number) => {
        handleEnd(reason, code);
        try {
          ws.close();
        } catch {
          // Already gone.
        }
      };
      this.endCurrent = endThis;
      ws.onclose = (event: any) =>
        handleEnd("closed", typeof event?.code === "number" ? event.code : undefined);

      ws.onerror = () => {
        try {
          ws.close();
        } catch {
          // Ignore close errors from failed handshake attempts.
        }
        // A refused handshake can end in error with no close event (seen in
        // headless Chrome). Without this the client waits in "connecting"
        // forever: no retry and no connection issue.
        if (ws.readyState === WebSocket.CLOSED) {
          handleEnd("error");
        }
      };
    } catch {
      if (attemptId !== this.attemptSequence) {
        return;
      }

      this.ws = null;
      this.emit("disconnected", { reason: "transport_closed" });
      if (this.shouldReconnect) {
        this.emit("connecting", {});
        void transportFailed(this.meta.server);
      }
      void this.reportConnectionIssue(attemptId);
      this.scheduleReconnect();
    }
  }
}

/** One correlated request: the reply that settles it and how it can fail. */
interface RequestOptions<T> {
  /** The reply type that resolves the request. */
  responseType: string;
  timeoutMs: number;
  /** Maps the reply to the result; a throw rejects the request. */
  parse: (payload: any) => T;
  /** The rejection for a timeout, or its message. */
  timeout: string | (() => Error);
  /** The rejection for an error reply, or its fallback message. */
  error?: string | ((payload: any) => Error);
  /** Error reply types. Defaults to the generic `error` channel. */
  errorTypes?: readonly string[];
  requestId?: string;
  /** A further reply check beyond the server and the request id. */
  matchesReply?: (payload: any) => boolean;
  /** Rejects with this message when the server disconnects first. */
  disconnectMessage?: string;
  signal?: AbortSignal;
  abortMessage?: string;
}

/**
 * The App's one live daemon connection. Many servers can be saved, but only
 * the current one is ever connected: connecting to another server first ends
 * the live connection, whose intentional disconnect clears that server's
 * state before the new socket can publish anything. Every send names its
 * server and fails unless that server is the live one.
 */
/** Same event at most this often, so a flapping stream cannot flood the log. */
const DIAGNOSTIC_MIN_GAP_MS = 10_000;

export class WebSocketClient {
  private readonly handlers = new Map<string, MessageHandler[]>();
  private readonly diagnosticSentAt = new Map<string, number>();
  private live: { meta: ConnectionMeta; socket: ServerSocket } | null = null;

  /** Connects to `server`, ending any live connection first. */
  connect(server: StoredServer) {
    if (this.live && this.live.meta.serverId !== server.id) {
      this.disconnect();
    }
    // Reconnecting the live server replaces only its socket and keeps state.
    const stale = this.live?.socket;
    const meta = toConnectionMeta(server);
    const socket = new ServerSocket(meta, (type, payload) => {
      this.emit(type, server.id, payload);
    });
    this.live = { meta, socket };
    stale?.disconnect();
    socket.connect();
  }

  /**
   * Ends the live connection, or does nothing when `serverId` names a server
   * that is not live. Listeners see an intentional disconnect.
   */
  disconnect(serverId?: string) {
    const live = this.live;
    if (!live || (serverId !== undefined && serverId !== live.meta.serverId)) {
      return;
    }
    live.socket.disconnect();
    this.live = null;
    this.emit("disconnected", live.meta.serverId, { reason: "intentional" });
    this.emit("connection_issue", live.meta.serverId, { issue: null });
  }

  /** On foreground, immediately resume a suspended reconnect backoff. */
  resumeReconnect() {
    this.live?.socket.resumeReconnect();
  }

  /** The live server's id, or null when nothing is connected or connecting. */
  liveServerId(): string | null {
    return this.live?.meta.serverId ?? null;
  }

  private liveSocket(serverId: string): ServerSocket | null {
    return this.live?.meta.serverId === serverId ? this.live.socket : null;
  }

  on(type: string, handler: MessageHandler) {
    const existing = this.handlers.get(type) || [];
    this.handlers.set(type, [...existing, handler]);
  }

  off(type: string, handler: MessageHandler) {
    const existing = this.handlers.get(type) || [];
    const remaining = existing.filter((current) => current !== handler);
    if (remaining.length === 0) {
      this.handlers.delete(type);
      return;
    }
    this.handlers.set(type, remaining);
  }

  send(serverId: string, msg: object) {
    const socket = this.liveSocket(serverId);
    if (!socket) {
      throw new Error("Daemon is not connected.");
    }
    socket.sendNow(msg);
  }

  private trySendNow(serverId: string, msg: object) {
    return this.liveSocket(serverId)?.trySendNow(msg) ?? false;
  }

  /**
   * Sends `{type, request_id, ...body}` now and settles on the first reply
   * correlated to this server and request id, an error reply, the timeout,
   * or (when asked) a disconnect or abort. Every path releases its listeners
   * and timer exactly once.
   */
  private request<T>(
    serverId: string,
    type: string,
    body: Record<string, unknown>,
    options: RequestOptions<T>,
  ): Promise<T> {
    const requestId = options.requestId ?? newRequestId();
    const errorTypes = options.errorTypes ?? ["error"];
    return new Promise<T>((resolve, reject) => {
      const correlated = (payload: any) =>
        payload.serverId === serverId && payload.request_id === requestId;
      const cleanup = () => {
        clearTimeout(timer);
        this.off(options.responseType, handleReply);
        for (const errorType of errorTypes) {
          this.off(errorType, handleError);
        }
        if (options.disconnectMessage) {
          this.off("disconnected", handleDisconnect);
        }
        options.signal?.removeEventListener("abort", handleAbort);
      };
      const handleReply = (payload: any) => {
        if (!correlated(payload)) return;
        if (options.matchesReply && !options.matchesReply(payload)) return;
        cleanup();
        try {
          resolve(options.parse(payload));
        } catch (error) {
          reject(error);
        }
      };
      const handleError = (payload: any) => {
        if (!correlated(payload)) return;
        cleanup();
        reject(
          typeof options.error === "function"
            ? options.error(payload)
            : new Error(payload.message || options.error),
        );
      };
      const handleDisconnect = (payload: any) => {
        if (payload.serverId !== serverId) return;
        cleanup();
        reject(new Error(options.disconnectMessage));
      };
      const handleAbort = () => {
        cleanup();
        reject(new Error(options.abortMessage));
      };
      const timer = setTimeout(() => {
        cleanup();
        reject(
          typeof options.timeout === "function"
            ? options.timeout()
            : new Error(options.timeout),
        );
      }, options.timeoutMs);

      this.on(options.responseType, handleReply);
      for (const errorType of errorTypes) {
        this.on(errorType, handleError);
      }
      if (options.disconnectMessage) {
        this.on("disconnected", handleDisconnect);
      }
      if (options.signal) {
        options.signal.addEventListener("abort", handleAbort, { once: true });
        if (options.signal.aborted) {
          handleAbort();
          return;
        }
      }
      try {
        this.send(serverId, { type, request_id: requestId, ...body });
      } catch (error) {
        cleanup();
        reject(
          error instanceof Error ? error : new Error("Daemon is not connected."),
        );
      }
    });
  }

  createSession(
    serverId: string,
    options?: {
      browserId?: string;
      targetId?: string;
      cwd?: string;
      command?: string;
      name?: string;
      /** Provider connection for the new Session (managed clients only). */
      connectionId?: string;
      /** Client-selected model, carried end-to-end into the launch. */
      modelId?: string;
    },
  ) {
    return this.request<CreateSessionResult>(
      serverId,
      "create_session",
      {
        ...(options?.browserId ? { browser_id: options.browserId } : {}),
        target_id: options?.targetId,
        cwd: options?.cwd,
        command: options?.command,
        name: options?.name,
        ...(options?.connectionId?.trim()
          ? { connection_id: options.connectionId.trim() }
          : {}),
        ...(options?.modelId?.trim()
          ? { model_id: options.modelId.trim() }
          : {}),
      },
      {
        requestId: newProviderRequestId(),
        responseType: "session_created",
        timeoutMs: 10000,
        timeout: providerTimeout("Timed out while creating a new terminal."),
        error: (payload) =>
          payload.code
            ? providerErrorFromPayload(payload)
            : new Error(payload.message || "Failed to create terminal."),
        parse: (payload) => {
          if (
            payload.worker_session &&
            typeof payload.worker_session === "object"
          ) {
            this.emit("worker_session_created", serverId, {
              worker_session: payload.worker_session,
            });
          }
          if (typeof payload.worker_id !== "string" || !payload.worker_id) {
            throw new Error("Daemon returned an invalid session id.");
          }
          return parseProviderReply(() => {
            const persistence = parseOptionalMutationPersistence(payload);
            if (persistence) {
              const classification = classifyMutationPersistence(persistence);
              if (classification === "ambiguous") {
                throw ambiguousProviderMutation(persistence.warning);
              }
              if (classification === "not_applied") {
                throw providerErrorFromPayload({
                  code: payload.code || PROVIDER_ERROR_CODES.invalid,
                  message:
                    payload.persistence_warning ||
                    payload.message ||
                    "Session was not created.",
                });
              }
            }
            return { workerId: payload.worker_id, persistence };
          }, "Invalid create_session payload.");
        },
      },
    );
  }

  listProviders(serverId: string): Promise<ProvidersSnapshot> {
    return this.requestProvidersCatalog(
      serverId,
      "list_providers",
      {},
      "Timed out while loading Providers.",
      true,
    ).then((result) => result.snapshot);
  }

  /**
   * Create/update a Provider connection. The optional credential is written
   * atomically with the connection: empty omits the key (preserving any
   * stored secret), non-empty replaces it. The value is scrubbed after send.
   */
  upsertProviderConnection(
    serverId: string,
    input: {
      connection: ProviderConnectionInput;
      revision: number;
      operation?: "create" | "update";
      credential?: string;
    },
  ): Promise<ProvidersMutationResult> {
    let transientCredential = input.credential?.trim() ?? "";
    const body: Record<string, unknown> = {
      provider_connection: input.connection,
      revision: input.revision,
      operation: input.operation ?? "update",
    };
    if (transientCredential) {
      body.credential = transientCredential;
    }
    return this.requestProvidersCatalog(
      serverId,
      "upsert_provider_connection",
      body,
      "Timed out while saving Provider connection.",
      false,
    ).finally(() => {
      transientCredential = "";
    });
  }

  deleteProviderConnection(
    serverId: string,
    connectionId: string,
    revision: number,
  ): Promise<ProvidersMutationResult> {
    return this.requestProvidersCatalog(
      serverId,
      "delete_provider_connection",
      { connection_id: connectionId, revision },
      "Timed out while deleting Provider connection.",
      false,
    );
  }

  setProviderConnection(
    serverId: string,
    input: ProviderConnectionSelectionInput,
  ): Promise<ProvidersMutationResult> {
    return this.requestProvidersCatalog(
      serverId,
      "set_provider_connection",
      {
        client: input.client,
        executor_id: input.client,
        connection_id: input.connectionId,
        revision: input.revision,
      },
      "Timed out while updating Provider connection.",
      false,
    );
  }

  switchProvider(
    serverId: string,
    input: ProviderSwitchInput,
  ): Promise<ProvidersMutationResult> {
    return this.requestProvidersCatalog(
      serverId,
      "switch_provider",
      {
        client: input.client,
        executor_id: input.client,
        connection_id: input.connectionId,
        revision: input.revision,
      },
      "Timed out while switching Provider.",
      false,
    );
  }

  /**
   * Persist the client-side model support allowlist of one connection: the
   * full set of discovered models the client wants exposed. The gateway never
   * owns a model; this write only selects the Provider connection.
   */
  setProviderModels(
    serverId: string,
    input: { connectionId: string; modelIds: string[] },
  ): Promise<ProvidersMutationResult> {
    return this.requestProvidersCatalog(
      serverId,
      "set_provider_models",
      { connection_id: input.connectionId, model_ids: input.modelIds },
      "Timed out while updating model support.",
      false,
    );
  }

  discoverProviderModels(
    serverId: string,
    connectionId: string,
  ): Promise<ProviderModelsResult> {
    return this.request(
      serverId,
      "discover_provider_models",
      { connection_id: connectionId },
      {
        requestId: newProviderRequestId(),
        responseType: "provider_models",
        timeoutMs: 20000,
        timeout: providerTimeout("Timed out while discovering models."),
        error: (payload) => providerErrorFromPayload(payload),
        parse: (payload) =>
          parseProviderReply(() => {
            const parsed = parseProviderModelsResult(payload, connectionId);
            if (!parsed) {
              throw invalidProviderReply(
                "Daemon returned invalid provider models.",
              );
            }
            return parsed;
          }, "Invalid models payload."),
      },
    );
  }

  refreshModelsDevMetadata(serverId: string): Promise<void> {
    return this.request(
      serverId,
      "refresh_models_dev_metadata",
      {},
      {
        requestId: newProviderRequestId(),
        responseType: "models_dev_metadata_refresh",
        timeoutMs: 20000,
        timeout: providerTimeout("Timed out while refreshing model metadata."),
        error: (payload) => providerErrorFromPayload(payload),
        parse: (payload) => {
          if (payload.ok !== true) {
            throw new ProviderError(
              PROVIDER_ERROR_CODES.invalid,
              payload.error || "Metadata refresh failed.",
              "invalid",
              true,
            );
          }
        },
      },
    );
  }

  testProviderConnection(
    serverId: string,
    input: { client: "codex" | "claude"; baseUrl: string; apiKey: string },
  ): Promise<ProviderConnectionTestResult> {
    let transientCredential = input.apiKey.trim();
    const baseUrl = input.baseUrl.trim();
    if (!baseUrl || !transientCredential) {
      return Promise.reject(
        new ProviderError(
          PROVIDER_ERROR_CODES.invalid,
          "Enter a Base URL and API key.",
          "credential",
          false,
        ),
      );
    }
    const pending = this.requestProviderConnectionTest(
      serverId,
      {
        provider_connection: {
          preset_id: "custom",
          client: input.client,
          base_url: baseUrl,
          advanced: true,
        },
        credential: transientCredential,
      },
      input.client,
      true,
    );
    transientCredential = "";
    return pending;
  }

  /**
   * Test the exact saved connection by stable Provider ID. The daemon resolves
   * the persisted Base URL, compiled protocol and active stored credential ref
   * internally; the App never supplies or receives the secret.
   */
  testSavedProviderConnection(
    serverId: string,
    connectionId: string,
    client: string = "codex",
  ): Promise<ProviderConnectionTestResult> {
    const id = connectionId.trim();
    if (!id) {
      return Promise.reject(
        new ProviderError(
          PROVIDER_ERROR_CODES.invalid,
          "Provider id is required to test the saved connection.",
          "invalid",
          false,
        ),
      );
    }
    return this.requestProviderConnectionTest(
      serverId,
      { connection_id: id },
      client,
      false,
    );
  }

  private requestProviderConnectionTest(
    serverId: string,
    body: Record<string, unknown>,
    client: string,
    credentialWrite: boolean,
  ): Promise<ProviderConnectionTestResult> {
    return this.request(serverId, "test_provider_connection", body, {
      requestId: newProviderRequestId(),
      responseType: "provider_connection_test",
      timeoutMs: 20000,
      timeout: providerTimeout("Connection test timed out."),
      error: (payload) => providerErrorFromPayload(payload, { credentialWrite }),
      parse: (payload) =>
        parseProviderReply(() => {
          const parsed = parseProviderConnectionTestResult(payload, client);
          if (!parsed) {
            throw invalidProviderReply(
              "Daemon returned an invalid connection test result.",
            );
          }
          return parsed;
        }, "Invalid connection test payload."),
    });
  }

  getThreadRuntime(
    serverId: string,
    workerId: string,
  ): Promise<ThreadRuntimeSelection> {
    return this.request(
      serverId,
      "get_thread_runtime",
      { worker_id: workerId },
      {
        requestId: newProviderRequestId(),
        responseType: "thread_runtime",
        timeoutMs: 15000,
        timeout: providerTimeout("Timed out while loading session provider."),
        error: (payload) => providerErrorFromPayload(payload),
        parse: (payload) =>
          parseProviderReply(() => {
            const selection = parseThreadRuntimeSelection(
              payload.runtime,
              workerId,
            );
            if (!selection) {
              throw invalidProviderReply(
                "Daemon returned an invalid session provider selection.",
              );
            }
            return selection;
          }, "Invalid session provider payload."),
      },
    );
  }

  setThreadRuntime(
    serverId: string,
    input: {
      workerId: string;
      runtime: import("./providers").ThreadRuntimeChoice;
    },
  ): Promise<ThreadRuntimeMutationResult> {
    return this.request(
      serverId,
      "set_thread_runtime",
      {
        worker_id: input.workerId,
        runtime: {
          connection_id: input.runtime.connectionId,
          model_id: input.runtime.modelId,
          ...(input.runtime.effect?.trim()
            ? { effect: input.runtime.effect.trim() }
            : {}),
          ...(input.runtime.useDefaultEffect
            ? { use_default_effect: true }
            : {}),
        },
      },
      {
        requestId: newProviderRequestId(),
        responseType: "thread_runtime_set",
        timeoutMs: 20000,
        timeout: providerTimeout("Timed out while switching model."),
        error: (payload) => providerErrorFromPayload(payload),
        parse: (payload) =>
          parseProviderReply(() => {
            const persistence = requireAppliedPersistence(payload);
            const selection = parseThreadRuntimeSelection(
              payload.runtime,
              input.workerId,
            );
            if (!selection || !assertThreadRuntimeMatches(selection, input)) {
              throw invalidProviderReply(
                "Daemon returned an invalid activation selection.",
              );
            }
            return { runtime: selection, persistence };
          }, "Invalid activation payload."),
      },
    );
  }

  setProviderCredential(
    serverId: string,
    connectionId: string,
    credential: string,
  ): Promise<ProviderCredentialResult> {
    let transientCredential = credential.trim();
    if (!transientCredential) {
      return Promise.reject(
        new ProviderError(
          PROVIDER_ERROR_CODES.invalid,
          "Enter an API key.",
          "credential",
          false,
        ),
      );
    }
    const pending = this.request<ProviderCredentialResult>(
      serverId,
      "set_provider_credential",
      { connection_id: connectionId, credential: transientCredential },
      {
        requestId: newProviderRequestId(),
        responseType: "provider_credential",
        timeoutMs: 15000,
        timeout: providerTimeout("Timed out while saving API key."),
        error: (payload) =>
          providerErrorFromPayload(payload, { credentialWrite: true }),
        parse: (payload) =>
          parseProviderReply(() => {
            const parsed = parseProviderCredentialResult(payload, connectionId);
            if (!parsed) {
              throw invalidProviderReply(
                "Daemon returned an invalid credential result.",
              );
            }
            return parsed;
          }, "Invalid credential payload."),
      },
    );
    transientCredential = "";
    return pending;
  }

  private requestProvidersCatalog(
    serverId: string,
    type: string,
    body: Record<string, unknown>,
    timeoutMessage: string,
    isList: boolean,
  ): Promise<ProvidersMutationResult> {
    if (!this.liveSocket(serverId)) {
      return Promise.reject(offlineProviderError());
    }
    return this.request(serverId, type, body, {
      requestId: newProviderRequestId(),
      responseType: "providers",
      timeoutMs: 15000,
      timeout: providerTimeout(timeoutMessage),
      error: (payload) => providerErrorFromPayload(payload),
      parse: (payload) =>
        parseProviderReply(() => {
          const snapshot = parseProvidersSnapshot(payload);
          if (!snapshot) {
            throw invalidProviderReply(
              "Daemon returned an invalid Providers catalog.",
            );
          }
          const persistence = isList
            ? (parseOptionalMutationPersistence(payload) ?? {
                applied: true,
                durable: true,
                outcome: "applied",
              })
            : requireAppliedPersistence(payload);
          if (isList && persistence.ambiguous) {
            throw ambiguousProviderMutation(persistence.warning);
          }
          return { snapshot, catalog: snapshot, persistence };
        }, "Daemon returned an invalid Providers payload."),
    });
  }

  listDir(serverId: string, path?: string) {
    return this.request<{
      path: string;
      entries: { name: string; path: string }[];
    }>(
      serverId,
      "list_dir",
      { cwd: path ?? "" },
      {
        responseType: "dir_list",
        timeoutMs: 10000,
        timeout: "Timed out while listing directory.",
        error: "Failed to list directory.",
        parse: (payload) => ({
          path: payload.path,
          entries: payload.entries ?? [],
        }),
      },
    );
  }

  getGitDiffStatus(
    serverId: string,
    options?: {
      targetId?: string;
      cwd?: string;
    },
  ) {
    return this.request<GitDiffStatusSnapshot>(
      serverId,
      "git_diff_status",
      { target_id: options?.targetId, cwd: options?.cwd },
      {
        responseType: "git_diff_status",
        timeoutMs: 10000,
        timeout: "Timed out while loading git diff status.",
        error: "Failed to load git diff status.",
        parse: (payload) =>
          (payload.status ?? {
            available: false,
            clean: true,
            file_count: 0,
            staged_file_count: 0,
            unstaged_file_count: 0,
            untracked_file_count: 0,
            additions: 0,
            deletions: 0,
            files: [],
          }) as GitDiffStatusSnapshot,
      },
    );
  }

  getGitDiffPatch(
    serverId: string,
    options: {
      targetId?: string;
      cwd?: string;
      path: string;
    },
  ) {
    return this.request<GitDiffPatchPayload>(
      serverId,
      "git_diff_patch",
      { target_id: options.targetId, cwd: options.cwd, path: options.path },
      {
        responseType: "git_diff_patch",
        timeoutMs: 10000,
        timeout: "Timed out while loading git diff patch.",
        error: "Failed to load git diff patch.",
        parse: (payload) => payload.patch as GitDiffPatchPayload,
      },
    );
  }

  getGitDiffPage(
    serverId: string,
    options: import("./gitDiff").GitDiffPageRequest & {
      targetId: string;
      cwd: string;
    },
  ) {
    return this.request<import("./gitDiff").GitDiffPage>(
      serverId,
      "git_diff_page",
      {
        target_id: options.targetId,
        cwd: options.cwd,
        path: options.path,
        scope: options.scope,
        row: options.row,
        file_generation: options.version,
        query: options.query,
      },
      {
        responseType: "git_diff_page",
        timeoutMs: 10000,
        timeout: "Timed out while loading diff page.",
        error: "Could not load diff page.",
        parse: (payload) => payload.page,
      },
    );
  }

  getGitDiffFileContent(
    serverId: string,
    options: {
      targetId?: string;
      cwd?: string;
      path: string;
    },
  ) {
    return this.request<GitDiffFileContentPayload>(
      serverId,
      "git_diff_file_content",
      { target_id: options.targetId, cwd: options.cwd, path: options.path },
      {
        responseType: "git_diff_file_content",
        timeoutMs: 10000,
        timeout: "Timed out while loading git diff file content.",
        error: "Failed to load git diff file content.",
        parse: (payload) => payload.content as GitDiffFileContentPayload,
      },
    );
  }

  getGitRepoEntries(
    serverId: string,
    options?: {
      targetId?: string;
      cwd?: string;
      path?: string;
    },
  ) {
    return this.request<GitRepoBrowserPayload>(
      serverId,
      "git_repo_entries",
      { target_id: options?.targetId, cwd: options?.cwd, path: options?.path },
      {
        responseType: "git_repo_entries",
        timeoutMs: 10000,
        timeout: "Timed out while loading repository files.",
        error: "Failed to load repository files.",
        parse: (payload) => payload.browser as GitRepoBrowserPayload,
      },
    );
  }

  getGitRepoFileContent(
    serverId: string,
    options: {
      targetId?: string;
      cwd?: string;
      path: string;
    },
  ) {
    return this.request<GitRepoFileContentPayload>(
      serverId,
      "git_repo_file_content",
      { target_id: options.targetId, cwd: options.cwd, path: options.path },
      {
        responseType: "git_repo_file_content",
        timeoutMs: 10000,
        timeout: "Timed out while loading repository file.",
        error: "Failed to load repository file.",
        parse: (payload) => payload.content as GitRepoFileContentPayload,
      },
    );
  }

  subscribeCodexConversation(
    serverId: string,
    options: CodexConversationSubscriptionOptions,
    handlers: CodexConversationSubscriptionHandlers,
  ) {
    const requestId = newRequestId();

    const handleSnapshot = (payload: any) => {
      if (payload.serverId !== serverId || payload.request_id !== requestId) {
        return;
      }
      handlers.onSnapshot(normalizeCodexConversationSnapshotPayload(payload));
    };

    const handleDelta = (payload: any) => {
      if (payload.serverId !== serverId || payload.request_id !== requestId) {
        return;
      }
      handlers.onDelta(normalizeCodexConversationDeltaPayload(payload));
    };

    const handleSyncStatus = (payload: any) => {
      if (payload.serverId !== serverId || payload.request_id !== requestId) {
        return;
      }
      handlers.onSyncStatus(
        normalizeCodexConversationSyncStatusPayload(payload),
      );
    };

    const handleError = (payload: any) => {
      if (payload.serverId !== serverId || payload.request_id !== requestId) {
        return;
      }
      handlers.onError(new Error(payload.message ?? ""));
    };

    const removeHandlers = () => {
      this.off("codex_conversation_snapshot", handleSnapshot);
      this.off("codex_conversation_delta", handleDelta);
      this.off("codex_conversation_sync_status", handleSyncStatus);
      this.off("error", handleError);
    };

    this.on("codex_conversation_snapshot", handleSnapshot);
    this.on("codex_conversation_delta", handleDelta);
    this.on("codex_conversation_sync_status", handleSyncStatus);
    this.on("error", handleError);
    try {
      this.send(serverId, {
        type: "codex_conversation_subscribe",
        request_id: requestId,
        target_id: options.targetId,
        worker_id: options.workerId,
        cwd: options.cwd,
        command: options.command,
        name: options.name,
        started_at: options.startedAt,
        process_id: options.processId,
        conversation_scope_key: options.conversationScopeKey,
      });
    } catch (error) {
      removeHandlers();
      throw error;
    }

    let subscribed = true;
    return () => {
      if (!subscribed) {
        return;
      }
      subscribed = false;
      removeHandlers();
      this.trySendNow(serverId, {
        type: "codex_conversation_unsubscribe",
        request_id: requestId,
        target_id: options.targetId,
        worker_id: options.workerId,
      });
    };
  }

  getCodexSlashCommands(serverId: string) {
    return this.request<CodexSlashCommandSnapshot>(
      serverId,
      "codex_slash_commands",
      {},
      {
        responseType: "codex_slash_commands",
        timeoutMs: 10000,
        timeout: "Timed out while loading Codex commands.",
        error: "Failed to load Codex commands.",
        parse: (payload) => {
          const commands = Array.isArray(payload.commands)
            ? payload.commands
                .map((command: any) => ({
                  value: typeof command.value === "string" ? command.value : "",
                  name: typeof command.name === "string" ? command.name : "",
                  title:
                    typeof command.title === "string" ? command.title : "",
                  description:
                    typeof command.description === "string"
                      ? command.description
                      : "",
                  source:
                    typeof command.source === "string"
                      ? command.source
                      : undefined,
                  category:
                    typeof command.category === "string" && command.category
                      ? command.category
                      : "",
                  execution:
                    typeof command.execution === "string" && command.execution
                      ? command.execution
                      : "",
                  input: normalizeCodexSlashCommandInput(command.input),
                  output: normalizeCodexSlashCommandOutput(command.output),
                  interactive: Boolean(command.interactive),
                  chat_supported: Boolean(command.chat_supported),
                  terminal_supported:
                    typeof command.terminal_supported === "boolean"
                      ? command.terminal_supported
                      : command.execution !== "unsupported",
                }))
                .filter(
                  (command: CodexSlashCommand) =>
                    command.value.startsWith("/") && command.name.length > 0,
                )
            : [];
          return {
            source:
              typeof payload.source === "string" ? payload.source : undefined,
            commands,
          };
        },
      },
    );
  }

  getCodexSkills(
    serverId: string,
    options: {
      cwd?: string;
    } = {},
  ) {
    return this.request<CodexSkillsSnapshot>(
      serverId,
      "codex_skills",
      { cwd: options.cwd },
      {
        responseType: "codex_skills",
        timeoutMs: 10000,
        timeout: "Timed out while loading Codex skills.",
        error: "Failed to load Codex skills.",
        parse: (payload) => {
          const skills = Array.isArray(payload.skills)
            ? payload.skills
                .map((skill: any) => ({
                  name: typeof skill.name === "string" ? skill.name : "",
                  description:
                    typeof skill.description === "string"
                      ? skill.description
                      : undefined,
                  path: typeof skill.path === "string" ? skill.path : "",
                  scope:
                    typeof skill.scope === "string" && skill.scope
                      ? skill.scope
                      : "user",
                  enabled:
                    typeof skill.enabled === "boolean" ? skill.enabled : true,
                }))
                .filter(
                  (skill: CodexSkill) =>
                    skill.name.length > 0 && skill.path.length > 0,
                )
            : [];
          return {
            cwd: typeof payload.cwd === "string" ? payload.cwd : options.cwd,
            skills,
          };
        },
      },
    );
  }

  getSkillsInventory(
    serverId: string,
    options: { cwd?: string; generation: number },
  ) {
    return this.request<{ generation: number; inventory: SkillsInventory }>(
      serverId,
      "skills_inventory",
      { generation: options.generation, cwd: options.cwd },
      {
        responseType: "skills_inventory",
        errorTypes: ["skills_inventory_error"],
        timeoutMs: 15000,
        timeout: "Timed out while loading installed Skills.",
        error: "Failed to load installed Skills.",
        parse: (payload) => {
          if (payload.generation !== options.generation) {
            throw new Error(
              "Daemon returned a stale Skills inventory generation.",
            );
          }
          return {
            generation: options.generation,
            inventory: normalizeSkillsInventory(payload.inventory),
          };
        },
      },
    );
  }

  buildSkillsCommand(serverId: string, options: SkillDeleteIdentity) {
    return this.request<SkillsMutationCommand>(
      serverId,
      "skills_command",
      skillIdentityFields(options),
      {
        responseType: "skills_command",
        errorTypes: ["skills_command_error"],
        timeoutMs: 15000,
        timeout: "Timed out while validating the Skills command.",
        error: "Skills command was rejected.",
        parse: (payload) => {
          const command = normalizeSkillsMutationCommand(payload.command);
          assertSkillsCommandMatchesRequest(command, options);
          return command;
        },
      },
    );
  }

  executeSkillsMutation(serverId: string, options: SkillDeleteIdentity) {
    return this.request<SkillsMutationResult>(
      serverId,
      "skills_mutation",
      skillIdentityFields(options),
      {
        responseType: "skills_mutation_result",
        errorTypes: ["skills_mutation_error", "error"],
        timeoutMs: SKILLS_MUTATION_TIMEOUT_MS,
        timeout: "Timed out while running the Skills mutation.",
        error: (payload) =>
          daemonRequestError(
            payload.message || "The Skills mutation failed.",
            payload.code,
          ),
        parse: (payload) => {
          const result = normalizeSkillsMutationResult(payload);
          assertSkillsMutationMatchesRequest(result, options);
          return result;
        },
      },
    );
  }

  getSkillsInspect(
    serverId: string,
    options: {
      skillName: string;
      skillId?: string;
      generation: number;
      cwd?: string;
      path?: string;
    },
  ) {
    return this.request<{ generation: number; detail: PackageDetail }>(
      serverId,
      "skills_inspect",
      {
        generation: options.generation,
        skill_name: options.skillName,
        skill_id: options.skillId,
        cwd: options.cwd,
        path: options.path,
      },
      {
        responseType: "skills_inspect_result",
        errorTypes: ["skills_inspect_error"],
        timeoutMs: 15000,
        timeout: "Timed out while inspecting the Skill.",
        error: (payload) =>
          daemonRequestError(
            payload.message || "Could not inspect this Skill.",
            payload.code,
          ),
        parse: (payload) => {
          if (payload.generation !== options.generation) {
            throw new Error(
              "Daemon returned a stale Skills inspect generation.",
            );
          }
          const detail = normalizeSkillsInspectDetail(payload.detail);
          if (
            detail.skillName !== options.skillName ||
            (options.skillId != null && detail.copyId !== options.skillId)
          ) {
            throw new Error(
              "Daemon returned details for a different Skill copy.",
            );
          }
          return { generation: options.generation, detail };
        },
      },
    );
  }

  getPluginsInventory(serverId: string, options: { generation: number }) {
    return this.request<{ generation: number; inventory: PluginInventory }>(
      serverId,
      "plugins_inventory",
      { generation: options.generation },
      {
        responseType: "plugins_inventory",
        // Unknown request types arrive on the generic error channel; keep
        // their code so the caller can expose the daemon capability error.
        errorTypes: ["plugins_inventory_error", "error"],
        timeoutMs: PLUGINS_INVENTORY_TIMEOUT_MS,
        timeout: () =>
          daemonRequestError("Timed out while loading Plugins.", "timeout"),
        error: (payload) =>
          daemonRequestError(
            payload.message || "Failed to load Plugins.",
            payload.code,
          ),
        parse: (payload) => {
          if (payload.generation !== options.generation) {
            throw new Error(
              "Daemon returned a stale Plugins inventory generation.",
            );
          }
          return {
            generation: options.generation,
            inventory: normalizePluginsInventory(payload.inventory),
          };
        },
      },
    );
  }

  buildPluginCommand(serverId: string, options: PluginMutationInput) {
    return this.request<PluginMutationCommand>(
      serverId,
      "plugin_command",
      pluginMutationFields(options),
      {
        responseType: "plugin_command",
        errorTypes: ["plugin_command_error"],
        timeoutMs: PLUGIN_COMMAND_TIMEOUT_MS,
        timeout: () =>
          daemonRequestError(
            "Timed out while validating the plugin command.",
            "timeout",
          ),
        error: (payload) =>
          daemonRequestError(
            payload.message || "Plugin command was rejected.",
            payload.code,
          ),
        parse: (payload) => {
          const command = normalizePluginMutationCommand(payload.command);
          assertPluginCommandMatchesRequest(command, options);
          return command;
        },
      },
    );
  }

  executePluginMutation(serverId: string, options: PluginMutationInput) {
    return this.request<PluginMutationResult>(
      serverId,
      "plugin_mutation",
      pluginMutationFields(options),
      {
        responseType: "plugin_mutation_result",
        errorTypes: ["plugin_mutation_error", "error"],
        timeoutMs: PLUGIN_MUTATION_TIMEOUT_MS,
        timeout: "Timed out while running the Plugin mutation.",
        error: (payload) =>
          daemonRequestError(
            payload.message || "The Plugin mutation failed.",
            payload.code,
          ),
        parse: (payload) => {
          const result = normalizePluginMutationResult(payload);
          assertPluginMutationMatchesRequest(result, options);
          return result;
        },
      },
    );
  }

  getCodexTerminalSnapshot(serverId: string, targetId: string) {
    return this.request<string>(
      serverId,
      "codex_terminal_snapshot",
      { target_id: targetId },
      {
        responseType: "codex_terminal_snapshot",
        timeoutMs: 10000,
        timeout: "Timed out while loading Codex terminal output.",
        error: "Failed to load Codex terminal output.",
        parse: (payload) =>
          typeof payload.text === "string" ? payload.text : "",
      },
    );
  }

  getCodexAsset(
    serverId: string,
    options: {
      path: string;
      cwd?: string;
    },
  ) {
    return this.request<CodexAssetPreview>(
      serverId,
      "codex_asset",
      { path: options.path, cwd: options.cwd },
      {
        responseType: "codex_asset",
        timeoutMs: 10000,
        timeout: "Timed out while loading Codex asset.",
        error: "Failed to load Codex asset.",
        parse: (payload) => ({
          path: typeof payload.path === "string" ? payload.path : options.path,
          content_type:
            typeof payload.content_type === "string"
              ? payload.content_type
              : "image/*",
          data_url:
            typeof payload.data_url === "string" ? payload.data_url : "",
        }),
      },
    );
  }

  dshInteraction(
    serverId: string,
    request: SessionFileRequest,
    answer?: import("./dshInteractions").DSHAnswer,
    afterRev?: number,
  ): Promise<
    | import("./dshInteractions").DSHInteractionSnapshot
    | { accepted: boolean }
  > {
    return this.request(
      serverId,
      "dsh_interaction",
      {
        worker_id: request.workerId,
        process_id: request.processId,
        started_at: request.startedAt,
        dsh_answer: answer,
        dsh_after_rev: afterRev,
      },
      {
        requestId: newProviderRequestId(),
        responseType: "dsh_interaction",
        timeoutMs: 15000,
        timeout: "DSH interaction timed out",
        error: "DSH interaction unavailable",
        parse: (payload) => {
          const result = payload.result;
          const valid = answer
            ? result?.accepted === true
            : typeof result?.epoch === "string" &&
              typeof result.connected === "boolean" &&
              Array.isArray(result.items);
          if (!valid) {
            throw new Error("Invalid DSH interaction response");
          }
          return result;
        },
      },
    );
  }

  getSessionImage(
    serverId: string,
    request: SessionFileRequest,
  ): Promise<string> {
    return this.request(
      serverId,
      "session_image",
      {
        worker_id: request.workerId,
        process_id: request.processId,
        started_at: request.startedAt,
        path: request.path,
      },
      {
        requestId: newProviderRequestId(),
        responseType: "session_image",
        timeoutMs: 15000,
        timeout: "Image request timed out",
        error: "Image unavailable",
        parse: (payload) => {
          if (
            typeof payload.data_url !== "string" ||
            !payload.data_url.startsWith("data:image/")
          ) {
            throw new Error("Invalid Session image");
          }
          return payload.data_url;
        },
      },
    );
  }

  getSessionFileMetadata(
    serverId: string,
    request: SessionFileRequest,
  ): Promise<SessionFileMetadata> {
    return this.request(
      serverId,
      "session_file_metadata",
      {
        worker_id: request.workerId,
        process_id: request.processId,
        started_at: request.startedAt,
        path: request.path,
      },
      {
        responseType: "session_file_metadata",
        timeoutMs: 10000,
        timeout: "Timed out while inspecting the Session file.",
        error: errorWithCode("Failed to inspect the Session file."),
        parse: (payload) => normalizeSessionFileMetadata(payload.metadata),
      },
    );
  }

  getSessionFileText(
    serverId: string,
    request: SessionFileRequest & { generation: string },
  ): Promise<SessionFileTextPreview> {
    return this.request(
      serverId,
      "session_file_text",
      {
        worker_id: request.workerId,
        process_id: request.processId,
        started_at: request.startedAt,
        path: request.path,
        file_generation: request.generation,
      },
      {
        responseType: "session_file_text",
        timeoutMs: 10000,
        timeout: "Timed out while reading the Session file.",
        error: errorWithCode("Failed to read the Session file."),
        parse: (payload) => normalizeSessionFileText(payload.text),
      },
    );
  }

  openTerminal(
    serverId: string,
    targetId: string,
    backend: string = "tmux",
    cols?: number,
    rows?: number,
  ) {
    this.send(serverId, {
      type: "terminal_open",
      target_id: targetId,
      backend,
      cols,
      rows,
    });
  }

  sendTerminalInput(serverId: string, sessionId: string, data: string) {
    this.send(serverId, {
      type: "terminal_input",
      session_id: sessionId,
      data,
    });
  }

  resizeTerminal(
    serverId: string,
    sessionId: string,
    cols: number,
    rows: number,
  ) {
    this.send(serverId, {
      type: "terminal_resize",
      session_id: sessionId,
      cols,
      rows,
    });
  }

  scrollTerminal(serverId: string, sessionId: string, lines: number) {
    this.send(serverId, {
      type: "terminal_scroll",
      session_id: sessionId,
      lines,
    });
  }

  cancelTerminalScroll(serverId: string, sessionId: string) {
    this.send(serverId, {
      type: "terminal_scroll_cancel",
      session_id: sessionId,
    });
  }

  focusTerminalPane(
    serverId: string,
    sessionId: string,
    col: number,
    row: number,
  ) {
    this.send(serverId, {
      type: "terminal_focus_pane",
      session_id: sessionId,
      col,
      row,
    });
  }

  closeTerminal(serverId: string, sessionId: string) {
    this.send(serverId, { type: "terminal_close", session_id: sessionId });
  }

  sendAction(
    serverId: string,
    workerId: string,
    action: WorkerAction,
  ): StructuredCommandReceipt {
    const socket = this.liveSocket(serverId);
    if (!socket?.isConnected) {
      throw new Error("Daemon is not connected.");
    }
    const requestId = newRequestId();
    return dispatchStructuredCommand({
      requestId,
      eventSource: this,
      sentType: "action_sent",
      failedType: "action_failed",
      matches: (payload) =>
        payload.serverId === serverId && payload.request_id === requestId,
      matchesConnection: (payload) => payload.serverId === serverId,
      sendNow: () => {
        socket.sendNow(
          structuredActionMessage({
            requestId,
            workerId,
            action,
          }),
        );
      },
    });
  }

  sendInput(
    serverId: string,
    workerId: string,
    text: string,
    options?: {
      attachments?: SendAttachment[];
      conversationScopeKey?: string;
      requestId?: string;
    },
  ): StructuredCommandReceipt {
    const socket = this.liveSocket(serverId);
    if (!socket?.isConnected) {
      throw new Error("Daemon is not connected.");
    }

    // Retries of the exact same logical input reuse its stable request id so
    // the daemon's durable receipt ledger stays idempotent; a new or edited
    // input omits requestId and receives a fresh identity.
    const requestId = options?.requestId || newRequestId();
    return dispatchStructuredCommand({
      requestId,
      eventSource: this,
      sentType: "input_sent",
      failedType: "input_failed",
      pendingType: "input_pending",
      matches: (payload) =>
        payload.serverId === serverId && payload.request_id === requestId,
      matchesConnection: (payload) => payload.serverId === serverId,
      sendNow: () => {
        socket.sendNow(
          structuredInputMessage({
            requestId,
            workerId,
            text,
            attachments: options?.attachments,
            conversationScopeKey: options?.conversationScopeKey,
          }),
        );
      },
    });
  }


  getTerminalSnapshot(serverId: string, targetId: string) {
    return this.request<{ text: string; target_id?: string }>(
      serverId,
      "terminal_snapshot",
      { target_id: targetId },
      {
        responseType: "terminal_snapshot",
        timeoutMs: 10000,
        timeout: "Timed out while loading terminal snapshot.",
        error: "Failed to load terminal snapshot.",
        parse: (payload) => ({
          text: typeof payload.text === "string" ? payload.text : "",
          target_id:
            typeof payload.target_id === "string"
              ? payload.target_id
              : undefined,
        }),
      },
    );
  }

  getTerminalHistory(serverId: string, sessionId: string) {
    return this.request<import("./terminalHistory").TerminalHistorySnapshot>(
      serverId,
      "terminal_history",
      { session_id: sessionId },
      {
        responseType: "terminal_history",
        timeoutMs: 10000,
        timeout: "Timed out while loading terminal snapshot.",
        error: "Failed to load terminal snapshot.",
        parse: (payload) => {
          if (payload.session_id !== sessionId || !payload.history) {
            throw new Error("Invalid terminal history response.");
          }
          return payload.history;
        },
      },
    );
  }

  /**
   * Answer a pending provider choice (Claude AskUserQuestion). The daemon
   * re-reads the questions from the transcript, drives the live prompt with
   * verified keys and resolves only after Claude records these answers.
   */
  answerChoice(
    serverId: string,
    workerId: string,
    answer: {
      call_id: string;
      answers: Array<{ selected: number[]; other?: string }>;
    },
  ) {
    return this.request<string[]>(
      serverId,
      "answer_choice",
      { worker_id: workerId, choice_answer: answer },
      {
        responseType: "choice_answered",
        timeoutMs: 30000,
        timeout: "Timed out while answering. Check the Terminal.",
        error: "Could not send the answer.",
        parse: (payload) =>
          Array.isArray(payload.answers) ? payload.answers : [],
      },
    );
  }

  /**
   * Step a Session's CLI back to one of the user's earlier prompts so an
   * edited message continues from there. The daemon proves the CLI is idle and
   * on that prompt before pressing anything; files on disk are never touched.
   */
  rewindMessage(serverId: string, workerId: string, eventId: string) {
    return this.request<{ composerClean: boolean }>(
      serverId,
      "rewind_message",
      { worker_id: workerId, message_rewind: { event_id: eventId } },
      {
        responseType: "message_rewound",
        timeoutMs: 30000,
        timeout: "Timed out while rewinding. Check the Terminal.",
        error: "Could not rewind.",
        // False: the agent stepped back but its input box still holds the old prompt.
        parse: (payload) => ({ composerClean: payload.composer_clean !== false }),
      },
    );
  }

  /**
   * One line in the daemon log about a moment only this app can see (a stream
   * gap, a snapshot that never came, a send nobody confirmed). The daemon
   * keeps no record of those, so a stuck screen leaves nothing to inspect
   * without it. Best effort and rate-limited per event; never throws.
   */
  reportDiagnostic(
    serverId: string,
    event: string,
    detail: string,
    streamId?: string,
  ) {
    const now = Date.now();
    const last = this.diagnosticSentAt.get(event);
    if (last !== undefined && now - last < DIAGNOSTIC_MIN_GAP_MS) {
      return;
    }
    const sent = this.trySendNow(serverId, {
      type: "client_diagnostic",
      name: event,
      request_id: streamId,
      body: detail,
    });
    if (sent) {
      this.diagnosticSentAt.set(event, now);
    }
  }

  sendKey(serverId: string, workerId: string, key: string) {
    return this.request<void>(
      serverId,
      "send_key",
      { worker_id: workerId, key },
      {
        responseType: "key_sent",
        timeoutMs: 5000,
        timeout: "Timed out while sending terminal key.",
        error: "Failed to send terminal key.",
        parse: () => undefined,
      },
    );
  }

  /** Answers a decision the agent reported with one of the options it offered. */
  decideApproval(serverId: string, workerId: string, approvalId: string, optionId: string) {
    return this.request<void>(
      serverId,
      "approval_decision",
      { worker_id: workerId, id: approvalId, option_id: optionId },
      {
        responseType: "approval_decided",
        timeoutMs: 8000,
        timeout: "Timed out while sending the decision.",
        error: "Failed to send the decision.",
        parse: () => undefined,
      },
    );
  }

  setActiveWorker(serverId: string, workerId: string | null) {
    this.trySendNow(serverId, {
      type: "set_active_worker",
      worker_id: workerId ?? "",
    });
  }

  /** Tells the live server which of its workers is on screen, if any. */
  clearActiveWorkersExcept(
    selected: { serverId: string; workerId: string } | null,
  ) {
    const serverId = this.liveServerId();
    if (!serverId) return;
    this.setActiveWorker(
      serverId,
      selected?.serverId === serverId ? selected.workerId : null,
    );
  }

  getStats(serverId: string): Promise<StatsPayload> {
    return this.request(
      serverId,
      "get_stats",
      {},
      {
        responseType: "stats_data",
        errorTypes: [],
        timeoutMs: 15000,
        timeout: "Stats request timed out.",
        parse: (payload) => payload,
      },
    );
  }

  getResourceTelemetry(
    serverId: string,
    signal?: AbortSignal,
  ): Promise<ResourceTelemetry> {
    return this.request(
      serverId,
      "get_resource_telemetry",
      {},
      {
        responseType: "resource_telemetry",
        timeoutMs: 10000,
        timeout: "Resource telemetry timed out.",
        error: (payload) =>
          new Error(
            typeof payload.message === "string" && payload.message
              ? payload.message
              : "Resource telemetry failed.",
          ),
        disconnectMessage: "Daemon is not connected.",
        signal,
        abortMessage: "Resource telemetry cancelled.",
        parse: (payload) => {
          const telemetry = normalizeResourceTelemetry(payload);
          if (!telemetry) {
            throw new Error("Invalid resource telemetry.");
          }
          return telemetry;
        },
      },
    );
  }

  /**
   * Fire-and-forget terminate: the daemon tears the Session down and replies
   * `worker_killed` once it is confirmed gone, or `error` when it is not; both
   * echo the optional request_id. The Session also leaves the list through a
   * `worker_session_archived` change.
   */
  killWorker(serverId: string, workerId: string, requestId?: string) {
    this.send(serverId, {
      type: "kill_worker",
      worker_id: workerId,
      ...(requestId ? { request_id: requestId } : {}),
    });
  }

  listWorkerSessions(serverId: string) {
    this.send(serverId, { type: "list_worker_sessions" });
  }

  requestBrainSnapshot(serverId: string) {
    this.send(serverId, { type: "brain_snapshot" });
  }

  markBrainWorkRead(serverId: string, workId: string) {
    this.send(serverId, {
      type: "brain_work_read",
      id: workId,
    });
  }

  /**
   * One user action on a Work slip. Resolves with Brain's admission for a
   * reply ("accepted" or "uncertain"); rejects with the daemon's reason.
   */
  actOnBrainWork(
    serverId: string,
    workId: string,
    action: BrainWorkUserActionKind,
    options: { text?: string; snoozeUntil?: string } = {},
  ): Promise<{ status?: string; admission?: string }> {
    return this.request(
      serverId,
      "brain_work_action",
      {
        id: workId,
        action,
        ...(options.text ? { text: options.text } : {}),
        ...(options.snoozeUntil ? { snooze_until: options.snoozeUntil } : {}),
      },
      {
        requestId: newRequestId("work_"),
        responseType: "brain_work_action",
        timeoutMs: 30000,
        timeout: "Timed out. The action may still have reached Brain.",
        error: "Brain could not take that action.",
        parse: (payload) => ({
          status: payload.status,
          admission: payload.admission || undefined,
        }),
      },
    );
  }

  /** The daemon's current live_state, so a reconnecting phone catches up. */
  getLiveState(serverId: string): Promise<unknown> {
    return this.request(
      serverId,
      "live_state_get",
      {},
      {
        responseType: "live_state",
        timeoutMs: 10000,
        timeout: "Timed out while loading live state.",
        error: "Could not load live state.",
        parse: (payload) => payload.live,
      },
    );
  }

  /** A read-only, revocable token for the home-screen widgets; shown once. */
  issueWidgetToken(serverId: string): Promise<{ token: string; id: string }> {
    return this.request(
      serverId,
      "widget_token_issue",
      {},
      {
        responseType: "widget_token",
        timeoutMs: 10000,
        timeout: "Timed out while preparing the widget.",
        error: "Could not prepare the widget.",
        parse: (payload) => ({ token: String(payload.token || ""), id: String(payload.id || "") }),
      },
    );
  }

  revokeWidgetToken(serverId: string): Promise<number> {
    return this.request(
      serverId,
      "widget_token_revoke",
      {},
      {
        responseType: "widget_token_revoked",
        timeoutMs: 10000,
        timeout: "Timed out while revoking the widget.",
        error: "Could not revoke the widget.",
        parse: (payload) => Number(payload.revoked || 0),
      },
    );
  }

  /**
   * Answers the question on a live notification. The daemon checks that the question is still
   * the current one; a Work question is answered there, a chat chip comes back as text to send.
   */
  answerLive(
    serverId: string,
    answer: { questionId: string; choiceIndex: number; requestId: string },
  ): Promise<{ kind: string; text: string; duplicate: boolean }> {
    return this.request(
      serverId,
      "live_answer",
      { question_id: answer.questionId, choice_index: answer.choiceIndex },
      {
        requestId: answer.requestId,
        responseType: "live_answer",
        timeoutMs: 30000,
        timeout: "Timed out. The answer may still have reached Brain.",
        error: "That answer could not be sent.",
        parse: (payload) => ({
          kind: String(payload.kind || ""),
          text: String(payload.text || ""),
          duplicate: Boolean(payload.duplicate),
        }),
      },
    );
  }

  getBrainContext(serverId: string): Promise<BrainContextPayload> {
    return this.request(
      serverId,
      "brain_context",
      {},
      {
        responseType: "brain_context",
        timeoutMs: 15000,
        timeout: "Timed out while loading Brain context.",
        error: "Failed to load Brain context.",
        parse: (payload) => (payload.context || {}) as BrainContextPayload,
      },
    );
  }

  runBrainGC(serverId: string): Promise<BrainHousekeepingPayload> {
    return this.request(
      serverId,
      "brain_gc",
      {},
      {
        responseType: "brain_gc",
        timeoutMs: 15000,
        timeout: "Timed out while running Brain housekeeping.",
        error: "Failed to run Brain housekeeping.",
        parse: (payload) =>
          (payload.housekeeping || {}) as BrainHousekeepingPayload,
      },
    );
  }

  startNewBrainChat(serverId: string) {
    return this.request<any>(
      serverId,
      "brain_chat_new",
      {},
      {
        responseType: "brain_snapshot",
        timeoutMs: 30000,
        timeout: "Timed out while starting a new Brain chat.",
        error: "Failed to start a new Brain chat.",
        parse: (payload) => payload.brain || {},
      },
    );
  }

  requestConnections(
    serverId: string,
    request: ConnectionRequest,
  ): Promise<ConnectionResponse> {
    return this.request(
      serverId,
      "connections",
      { connection_request: request },
      {
        requestId: `plugins_${Date.now().toString(36)}_${Math.random().toString(36).slice(2)}`,
        responseType: "connections_result",
        timeoutMs: 25000,
        timeout: "Plugin request timed out. Refresh to check the result.",
        // The code tells a refusal by the server from a request that never
        // got there.
        error: (payload) =>
          Object.assign(new Error(payload.message || "Plugin request failed."), {
            code: payload.code as string | undefined,
          }),
        disconnectMessage: "Server disconnected. Reconnect to check the result.",
        parse: (payload) => {
          if (!payload.connections || typeof payload.connections !== "object") {
            throw new Error("Invalid plugin response.");
          }
          return payload.connections as ConnectionResponse;
        },
      },
    );
  }

  getTelegramConnectionStatus(serverId: string) {
    return this.requestTelegramStatus(serverId, "telegram_connection_status");
  }

  configureTelegramConnection(serverId: string, credential: string) {
    return this.requestTelegramStatus(serverId, "telegram_connection_configure", {
      credential,
    });
  }

  disableTelegramConnection(serverId: string) {
    return this.requestTelegramStatus(serverId, "telegram_connection_disable");
  }

  enableTelegramConnection(serverId: string) {
    return this.requestTelegramStatus(serverId, "telegram_connection_enable");
  }

  revokeTelegramOwner(serverId: string) {
    return this.requestTelegramStatus(serverId, "telegram_connection_revoke");
  }

  removeTelegramConnection(serverId: string) {
    return this.requestTelegramStatus(serverId, "telegram_connection_remove");
  }

  beginTelegramBinding(serverId: string): Promise<TelegramBindingChallenge> {
    return this.request(
      serverId,
      "telegram_connection_bind",
      {},
      {
        requestId: newProviderRequestId(),
        responseType: "telegram_binding_challenge",
        timeoutMs: 20000,
        timeout: "Telegram owner binding timed out.",
        error: "Could not start Telegram owner binding.",
        parse: (payload) => {
          const challenge = payload.challenge;
          if (
            !challenge ||
            typeof challenge.url !== "string" ||
            typeof challenge.expires_at !== "string"
          ) {
            throw new Error(
              "The daemon returned an invalid Telegram binding challenge.",
            );
          }
          return challenge as TelegramBindingChallenge;
        },
      },
    );
  }

  private requestTelegramStatus(
    serverId: string,
    type:
      | "telegram_connection_status"
      | "telegram_connection_configure"
      | "telegram_connection_enable"
      | "telegram_connection_disable"
      | "telegram_connection_revoke"
      | "telegram_connection_remove",
    fields: Record<string, unknown> = {},
  ): Promise<TelegramConnectionStatus> {
    return this.request(serverId, type, fields, {
      requestId: newProviderRequestId(),
      responseType: "telegram_connection_status",
      timeoutMs: 20000,
      timeout: "Telegram connection request timed out.",
      error: "Telegram connection request failed.",
      parse: (payload) => {
        const connection = payload.connection;
        if (
          !connection ||
          typeof connection.state !== "string" ||
          typeof connection.enabled !== "boolean"
        ) {
          throw new Error(
            "The daemon returned an invalid Telegram connection status.",
          );
        }
        return connection as TelegramConnectionStatus;
      },
    });
  }

  setBrainExecutor(serverId: string, executorId: string) {
    return this.request<any>(
      serverId,
      "brain_set_executor",
      { executor_id: executorId, adapter_id: executorId },
      {
        responseType: "brain_snapshot",
        timeoutMs: 15000,
        timeout: "Timed out while switching Brain executor.",
        error: "Failed to switch Brain executor.",
        parse: (payload) => payload.brain || {},
      },
    );
  }

  getBrainWorkspaceTree(
    serverId: string,
    path = "",
  ): Promise<BrainWorkspaceTree> {
    return this.request(
      serverId,
      "brain_workspace_tree",
      { path },
      {
        responseType: "brain_workspace_tree",
        timeoutMs: 15000,
        timeout: "Timed out while loading Brain workspace.",
        error: "Failed to load Brain workspace.",
        parse: (payload) => normalizeBrainWorkspaceTree(payload.workspace_tree),
      },
    );
  }

  getBrainWorkspaceFile(
    serverId: string,
    path: string,
  ): Promise<BrainWorkspaceFile> {
    return this.request(
      serverId,
      "brain_workspace_file",
      { path },
      {
        responseType: "brain_workspace_file",
        timeoutMs: 15000,
        timeout: "Timed out while loading Brain workspace file.",
        error: "Failed to load Brain workspace file.",
        parse: (payload) => normalizeBrainWorkspaceFile(payload.file),
      },
    );
  }

  serviceTunnel(
    serverId: string,
    serviceId: string,
    generation: string,
    action: "start" | "stop" | "status",
  ): Promise<import("./sessionServices").ServiceTunnel> {
    return this.request(
      serverId,
      "service_tunnel",
      {
        service_id: serviceId,
        service_generation: generation,
        tunnel_action: action,
      },
      {
        requestId: newProviderRequestId(),
        responseType: "service_tunnel",
        matchesReply: (payload) => payload.service_id === serviceId,
        timeoutMs: 10000,
        timeout: "Tunnel request timed out",
        error: "Tunnel request failed",
        parse: (payload) => {
          if (!payload.tunnel || payload.tunnel.generation !== generation) {
            throw new Error("Service changed. Refresh Services.");
          }
          return payload.tunnel;
        },
      },
    );
  }

  listSessionServices(serverId: string): Promise<SessionServiceSnapshot> {
    return this.request(
      serverId,
      "list_session_services",
      {},
      {
        responseType: "session_service_list",
        timeoutMs: 10000,
        timeout: "Timed out while loading session services.",
        error: "Failed to load session services.",
        parse: (payload) => ({
          generated_at: payload.generated_at,
          interfaces: Array.isArray(payload.interfaces)
            ? payload.interfaces
            : [],
          services: Array.isArray(payload.services)
            ? payload.services.map(normalizeSessionService)
            : [],
        }),
      },
    );
  }

  // ── Calendar ─────────────────────────────────────────────────────────────

  listCalendarItems(serverId: string) {
    this.send(serverId, { type: "list_calendar_items" });
  }

  getCalendarItem(serverId: string, id: string) {
    return this.calendarAction(
      serverId,
      "get_calendar_item",
      "calendar_item",
      { id },
      "Failed to load calendar item.",
    );
  }

  createCalendarItem(serverId: string, item: Partial<CalendarItem>) {
    return this.calendarAction(
      serverId,
      "create_calendar_item",
      "calendar_item_created",
      { calendar_item: item },
      "Failed to create calendar item.",
    );
  }

  updateCalendarItem(serverId: string, item: CalendarItem) {
    return this.calendarAction(
      serverId,
      "update_calendar_item",
      "calendar_item_updated",
      { calendar_item: item, revision: item.revision },
      "Failed to update calendar item.",
    );
  }

  cancelCalendarItem(serverId: string, id: string, revision: number) {
    return this.calendarAction(
      serverId,
      "cancel_calendar_item",
      "calendar_item_cancelled",
      { id, revision },
      "Failed to cancel calendar item.",
    );
  }

  runCalendarItem(serverId: string, id: string) {
    return this.calendarAction(
      serverId,
      "run_calendar_item",
      "calendar_item_running",
      { id },
      "Failed to run calendar action.",
    );
  }

  private calendarAction(
    serverId: string,
    type: string,
    responseType: string,
    payload: Record<string, unknown>,
    fallback: string,
  ): Promise<CalendarItem> {
    return this.request(serverId, type, payload, {
      responseType,
      timeoutMs: 15000,
      timeout: "Calendar request timed out.",
      error: errorWithCode(fallback),
      parse: (data) => data.calendar_item as CalendarItem,
    });
  }

  // ── Work items ───────────────────────────────────────────────────────────

  listWorkItems(serverId: string) {
    this.send(serverId, { type: "list_work_items" });
  }

  writeWorkItem(
    serverId: string,
    options: {
      id?: string;
      project: string;
      path?: string;
      body: string;
      frontmatter?: Record<string, unknown>;
      baseMtime?: string;
    },
  ) {
    return this.request<any>(
      serverId,
      "write_work_item",
      {
        id: options.id ?? "",
        project: options.project,
        path: options.path ?? "",
        body: options.body,
        frontmatter: options.frontmatter ?? {},
        base_mtime: options.baseMtime ?? "",
      },
      {
        responseType: "work_item_written",
        timeoutMs: 10000,
        timeout: "Timed out while writing work item.",
        error: (payload) =>
          Object.assign(
            new Error(payload.message || "Failed to write work item."),
            { code: payload.code, current: payload.current },
          ),
        parse: (payload) => payload.work_item,
      },
    );
  }

  deleteWorkItem(serverId: string, id: string) {
    return this.request<void>(
      serverId,
      "delete_work_item",
      { id },
      {
        responseType: "work_item_deleted_ack",
        timeoutMs: 10000,
        timeout: "Timed out while deleting work item.",
        error: "Failed to delete work item.",
        parse: () => undefined,
      },
    );
  }

  /** Records the app's foreground state so a closed socket can say what the app was doing. */
  noteAppState(state: string) {
    appState = state;
  }

  /** Replaces a live connection that is not answering; it reconnects at once. */
  dropConnection(serverId: string, reason: string) {
    this.liveSocket(serverId)?.abandon(reason);
  }

  /** When the live connection to `serverId` last delivered any frame. */
  lastFrameAt(serverId: string) {
    return this.liveSocket(serverId)?.lastFrameAt;
  }

  isConnected(serverId: string) {
    return this.liveSocket(serverId)?.isConnected ?? false;
  }

  private emit(type: string, serverId: string, payload: any) {
    const meta =
      this.live?.meta.serverId === serverId ? this.live.meta : undefined;
    const data = {
      ...payload,
      serverId,
      serverName: meta?.serverName || serverId,
      serverUrl: meta?.serverUrl || "",
      daemonId: meta?.daemonId || "",
      daemonPublicKey: meta?.daemonPublicKey || "",
    };
    const handlers = this.handlers.get(type) || [];
    handlers.forEach((handler) => handler(data));
  }
}

function newRequestId(prefix = ""): string {
  return `${prefix}${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
}

function providerTimeout(message: string): () => ProviderError {
  return () =>
    new ProviderError(PROVIDER_ERROR_CODES.timeout, message, "timeout", true);
}

/** Provider replies reject with a ProviderError, never a raw parse failure. */
function parseProviderReply<T>(parse: () => T, fallbackMessage: string): T {
  try {
    return parse();
  } catch (error) {
    throw error instanceof ProviderError
      ? error
      : invalidProviderReply(
          error instanceof Error ? error.message : fallbackMessage,
        );
  }
}

/** An error reply that keeps the daemon's code for the caller. */
function errorWithCode(fallback: string): (payload: any) => Error {
  return (payload) =>
    Object.assign(new Error(payload.message || fallback), {
      code: payload.code as string | undefined,
    });
}

function skillIdentityFields(options: SkillDeleteIdentity) {
  return {
    operation: options.operation,
    cwd: options.cwd,
    skill_id: options.skillId,
    skill_name: options.skillName,
    root_path: options.rootPath,
    canonical_path: options.canonicalPath,
    allowed_root: options.allowedRoot,
  };
}

function pluginMutationFields(options: PluginMutationInput) {
  const uninstall = options.operation === "uninstall";
  return {
    operation: options.operation,
    plugin_id: options.pluginId,
    plugin_host: options.host,
    plugin_source: uninstall ? options.source : undefined,
    plugin_version: uninstall ? options.version : undefined,
    agents: uninstall ? options.agents : undefined,
    scope: options.scope,
    plugin_copy_id: uninstall ? options.copyId : undefined,
    plugin_name: uninstall ? options.name : undefined,
    root_path: uninstall ? options.rootPath : undefined,
    canonical_path: uninstall ? options.canonicalPath : undefined,
    allowed_root: uninstall ? options.allowedRoot : undefined,
    plugin_revision: uninstall ? options.revision : undefined,
  };
}

function normalizeSessionService(value: any): SessionService {
  const service = value && typeof value === "object" ? value : {};
  return {
    ...service,
    id: typeof service.id === "string" ? service.id : "",
    worker_id: typeof service.worker_id === "string" ? service.worker_id : "",
    worker_name:
      typeof service.worker_name === "string" ? service.worker_name : "",
    project: typeof service.project === "string" ? service.project : undefined,
    cwd: typeof service.cwd === "string" ? service.cwd : undefined,
    command: typeof service.command === "string" ? service.command : undefined,
    process: typeof service.process === "string" ? service.process : undefined,
    pid: typeof service.pid === "number" ? service.pid : 0,
    port: typeof service.port === "number" ? service.port : 0,
    protocol: typeof service.protocol === "string" ? service.protocol : "tcp",
    binds: Array.isArray(service.binds) ? service.binds : [],
    urls: Array.isArray(service.urls) ? service.urls : [],
    local_only: Boolean(service.local_only),
    source: typeof service.source === "string" ? service.source : undefined,
    unit: typeof service.unit === "string" ? service.unit : undefined,
    state: typeof service.state === "string" ? service.state : undefined,
    status_detail:
      typeof service.status_detail === "string" ? service.status_detail : undefined,
  };
}

function normalizeBrainWorkspaceTree(raw: any): BrainWorkspaceTree {
  const source = raw && typeof raw === "object" ? raw : {};
  return {
    workspace:
      typeof source.workspace === "string" ? source.workspace : undefined,
    path: typeof source.path === "string" ? source.path : undefined,
    generated_at:
      typeof source.generated_at === "string" ? source.generated_at : undefined,
    entries: Array.isArray(source.entries)
      ? source.entries
          .map(normalizeBrainWorkspaceEntry)
          .filter((entry: BrainWorkspaceEntry) => entry.name)
      : [],
  };
}

function normalizeBrainWorkspaceEntry(raw: any): BrainWorkspaceEntry {
  const source = raw && typeof raw === "object" ? raw : {};
  return {
    name: typeof source.name === "string" ? source.name : "",
    path: typeof source.path === "string" ? source.path : "",
    kind: typeof source.kind === "string" ? source.kind : "file",
    size: typeof source.size === "number" ? source.size : undefined,
    modified_at:
      typeof source.modified_at === "string" ? source.modified_at : undefined,
    children: Array.isArray(source.children)
      ? source.children
          .map(normalizeBrainWorkspaceEntry)
          .filter((entry: BrainWorkspaceEntry) => entry.name)
      : [],
  };
}

function normalizeBrainWorkspaceFile(raw: any): BrainWorkspaceFile {
  const source = raw && typeof raw === "object" ? raw : {};
  return {
    data_url: typeof source.data_url === "string" ? source.data_url : undefined,
    name: typeof source.name === "string" ? source.name : "",
    path: typeof source.path === "string" ? source.path : "",
    kind: typeof source.kind === "string" ? source.kind : "file",
    language: typeof source.language === "string" ? source.language : "text",
    content: typeof source.content === "string" ? source.content : "",
    size: typeof source.size === "number" ? source.size : undefined,
    modified_at:
      typeof source.modified_at === "string" ? source.modified_at : undefined,
  };
}

function toConnectionMeta(server: StoredServer): ConnectionMeta {
  return {
    serverId: server.id,
    serverName: server.name,
    serverUrl: server.url,
    daemonId: server.daemonId,
    daemonPublicKey: server.daemonPublicKey,
    server,
  };
}

function normalizeCodexConversationSnapshotPayload(
  payload: any,
): CodexConversationSnapshotPayload {
  return {
    request_id:
      typeof payload.request_id === "string" ? payload.request_id : undefined,
    worker_id:
      typeof payload.worker_id === "string" ? payload.worker_id : undefined,
    conversation_id:
      typeof payload.conversation_id === "string"
        ? payload.conversation_id
        : undefined,
    revision:
      typeof payload.revision === "number" && Number.isFinite(payload.revision)
        ? payload.revision
        : 0,
    server_generation:
      typeof payload.generation === "string" ? payload.generation : undefined,
    conversation: normalizeCodexConversation(payload.conversation),
  };
}

function normalizeCodexConversationDeltaPayload(
  payload: any,
): CodexConversationDeltaPayload {
  const normalizedDelta = normalizeCodexConversation({
    available: true,
    updated_at: payload.updated_at,
    activity: payload.activity,
    events: payload.upserts,
  });
  return {
    request_id:
      typeof payload.request_id === "string" ? payload.request_id : undefined,
    worker_id:
      typeof payload.worker_id === "string" ? payload.worker_id : undefined,
    conversation_id:
      typeof payload.conversation_id === "string"
        ? payload.conversation_id
        : undefined,
    revision:
      typeof payload.revision === "number" && Number.isFinite(payload.revision)
        ? payload.revision
        : 0,
    base_revision:
      typeof payload.base_revision === "number" &&
      Number.isFinite(payload.base_revision)
        ? payload.base_revision
        : 0,
    server_generation:
      typeof payload.generation === "string" ? payload.generation : undefined,
    available:
      typeof payload.available === "boolean" ? payload.available : undefined,
    reason: typeof payload.reason === "string" ? payload.reason : undefined,
    source: typeof payload.source === "string" ? payload.source : undefined,
    path: typeof payload.path === "string" ? payload.path : undefined,
    session_id:
      typeof payload.session_id === "string" ? payload.session_id : undefined,
    cwd: typeof payload.cwd === "string" ? payload.cwd : undefined,
    updated_at:
      typeof payload.updated_at === "string" ? payload.updated_at : undefined,
    activity: Object.prototype.hasOwnProperty.call(payload, "activity")
      ? (normalizedDelta.activity ?? null)
      : undefined,
    upserts: normalizedDelta.events,
    deletes: Array.isArray(payload.deletes)
      ? payload.deletes.filter(
          (id: unknown): id is string => typeof id === "string",
        )
      : [],
  };
}

function normalizeCodexConversationSyncStatusPayload(
  payload: any,
): CodexConversationSyncStatusPayload {
  return {
    request_id:
      typeof payload.request_id === "string" ? payload.request_id : undefined,
    worker_id:
      typeof payload.worker_id === "string" ? payload.worker_id : undefined,
    conversation_id:
      typeof payload.conversation_id === "string"
        ? payload.conversation_id
        : undefined,
    revision:
      typeof payload.revision === "number" && Number.isFinite(payload.revision)
        ? payload.revision
        : 0,
    server_generation:
      typeof payload.generation === "string" ? payload.generation : undefined,
    state: typeof payload.state === "string" ? payload.state : "syncing",
    reason: typeof payload.reason === "string" ? payload.reason : undefined,
  };
}

export const wsClient = new WebSocketClient();
