import { signal } from "nsh";
import { failureText } from "../../../core/more/daemon";
import type { StateTone } from "../../../core/more/colour";
import { telegramLabel, telegramLabelTone, telegramRequest, type TelegramConnectionStatus } from "../../../core/settings/telegram";
import { connected, daemon } from "../daemon";

/** The current computer's Telegram connection as Settings last read it. */
export const telegram = signal<{ serverId: string; status: TelegramConnectionStatus | null; loading: boolean; error: string }>({
  serverId: "",
  status: null,
  loading: false,
  error: "",
});

let epoch = 0;

export function loadTelegram(): void {
  const current = daemon();
  if (!current || current.state !== "connected") return;
  const mine = ++epoch;
  const serverId = current.server.id;
  const held = telegram().serverId === serverId ? telegram().status : null;
  telegram.set({ serverId, status: held, loading: true, error: "" });
  telegramRequest(current, "status")
    .then((status) => mine === epoch && telegram.set({ serverId, status, loading: false, error: "" }))
    .catch((error: unknown) => mine === epoch && telegram.set({ serverId, status: held, loading: false, error: failureText(error, "Telegram status could not be loaded.") }));
}

/** A newer answer (after an action) replaces what was read. */
export function setTelegram(status: TelegramConnectionStatus): void {
  epoch++;
  telegram.set({ serverId: daemon()?.server.id ?? "", status, loading: false, error: "" });
}

/**
 * The Settings row's second line and its state (SPEC §10.4): a spinner while first read, the state words with their
 * dot once known, and a Connect button while no bot is set up.
 */
export function telegramRow(): { subtitle?: string; tone?: StateTone; label?: string; loading?: boolean; connect?: boolean } {
  const t = telegram();
  const status = t.serverId === daemon()?.server.id ? t.status : null;
  // Away from the computer nothing it said last is current.
  if (!connected()) return { subtitle: "Unavailable" };
  if (t.loading && !status) return { loading: true, label: "Loading" };
  if (t.error && !status) return { tone: "warn", label: "Unavailable" };
  if (!status) return {};
  if (!status.bot_username) return { connect: true, label: "Not connected" };
  const label = telegramLabel(status, true, false);
  return { subtitle: `@${status.bot_username}`, tone: telegramLabelTone(label), label };
}

/** What a screen reader says for the row: "Telegram, @bot, Connected". */
export function telegramRowLabel(): string {
  const t = telegram();
  const status = t.serverId === daemon()?.server.id ? t.status : null;
  const row = telegramRow();
  return ["Telegram", status?.bot_username && connected() ? `@${status.bot_username}` : "", row.label ?? row.subtitle ?? ""].filter(Boolean).join(", ");
}
