import type { Daemon } from "../more/daemon";

/** The current computer's Telegram bot: Brain answers you in a private chat with it. */

export type TelegramConnectionState = "disabled" | "setup_pending" | "connected" | "degraded";

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

export type TelegramAction = "status" | "enable" | "disable" | "revoke" | "remove";

async function statusRequest(daemon: Daemon, type: string, body: Record<string, unknown> = {}): Promise<TelegramConnectionStatus> {
  const frame = await daemon
    .request<{ connection?: TelegramConnectionStatus }>(type, body, { reply: "telegram_connection_status", timeoutMs: 20_000 })
    .catch((error: unknown) => {
      // The computer's own words (a bad token, no network there) are what the person can act on.
      const message = error instanceof Error ? error.message.trim() : "";
      if (/ timed out$/.test(message)) throw new Error("Telegram connection request timed out.");
      throw new Error(message && !message.endsWith(" failed") ? message : "Telegram connection request failed.");
    });
  const connection = frame.connection;
  if (!connection || typeof connection.state !== "string" || typeof connection.enabled !== "boolean") throw new Error("Telegram connection request failed.");
  return connection;
}

export function telegramRequest(daemon: Daemon, action: TelegramAction): Promise<TelegramConnectionStatus> {
  return statusRequest(daemon, action === "status" ? "telegram_connection_status" : `telegram_connection_${action}`);
}

/** Verifies and stores the BotFather token on the computer; it is sent once and never read back. */
export function configureTelegram(daemon: Daemon, token: string): Promise<TelegramConnectionStatus> {
  return statusRequest(daemon, "telegram_connection_configure", { credential: token.trim() });
}

/** The link that opens the private chat which makes you the bot's owner. */
export async function beginTelegramBinding(daemon: Daemon): Promise<TelegramBindingChallenge> {
  const frame = await daemon
    .request<{ challenge?: TelegramBindingChallenge }>("telegram_connection_bind", {}, { reply: "telegram_binding_challenge", timeoutMs: 20_000 })
    .catch(() => {
      throw new Error("Could not start Telegram owner binding.");
    });
  if (!frame.challenge?.url) throw new Error("Could not start Telegram owner binding.");
  return frame.challenge;
}

/** The status word of a connection, as the row and the page say it. */
export function telegramLabel(status: TelegramConnectionStatus | null, connected: boolean, loading: boolean): string {
  if (!connected) return "Server offline";
  if (loading && !status) return "Loading";
  if (!status?.bot_username) return "Not connected";
  if (!status.enabled) return "Disconnected";
  if (!status.owner_hint) return "Awaiting connection";
  if (status.state === "degraded") return "Needs attention";
  return "Connected";
}

/** The status word's colour: a warning while offline or degraded, ready once connected. */
export function telegramLabelTone(label: string): "warning" | "ready" | "soft" {
  if (label === "Server offline" || label === "Needs attention") return "warning";
  return label === "Connected" ? "ready" : "soft";
}

/** Who Brain's Telegram messages go to. */
export function telegramRecipient(status: TelegramConnectionStatus): string {
  if (status.topics_available) return "Brain";
  if (status.recipient_label) return status.recipient_label;
  if (status.recipient_id) return "Session unavailable";
  return "Brain";
}

/** What the primary button does next. */
export type TelegramStep = "token" | "connect" | "open-link" | "open-chat" | "reconnect";

export function telegramStep(status: TelegramConnectionStatus | null): TelegramStep {
  if (!status?.bot_username) return "token";
  if (!status.owner_hint) return status.binding_pending ? "open-link" : "connect";
  return status.enabled ? "open-chat" : "reconnect";
}

/**
 * What the Telegram page shows below its identity. Until the computer has answered there
 * is nothing to decide ("loading": no token form that would invite replacing a working
 * bot); a first read that failed offers Retry. Only an answer decides the step.
 */
export function telegramView(input: { status: TelegramConnectionStatus | null; error: string; replacing: boolean }): TelegramStep | "loading" | "retry" {
  if (input.replacing) return "token";
  if (!input.status) return input.error ? "retry" : "loading";
  return telegramStep(input.status);
}
