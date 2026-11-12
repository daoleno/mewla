import { signal } from "nsh";
import { failureText } from "../../../core/more/daemon";
import { telegramLabel, telegramRequest, type TelegramConnectionStatus } from "../../../core/settings/telegram";
import { connected, daemon } from "../daemon";
import type { Status } from "../../kit/kit";

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

/** The row's second line and status mark. */
export function telegramRow(): { subtitle?: string; status?: Status; label?: string } {
  const t = telegram();
  const status = t.serverId === daemon()?.server.id ? t.status : null;
  // Away from the computer nothing it said last is current.
  if (!connected()) return { subtitle: "Unavailable" };
  if (t.loading && !status) return { status: "running", label: "Loading" };
  if (t.error && !status) return { status: "failed", label: "Unavailable" };
  if (!status) return {};
  const word = { connected: "Connected", setup_pending: "Setup pending", degraded: "Degraded", disabled: "Disabled" }[status.state];
  const tone: Status = status.state === "connected" ? "ready" : status.state === "setup_pending" ? "warning" : status.state === "degraded" ? "failed" : "neutral";
  return {
    subtitle: status.bot_username ? `@${status.bot_username}` : telegramLabel(status, connected(), false),
    status: status.bot_username || status.state !== "disabled" ? tone : undefined,
    label: word,
  };
}

/** What a screen reader says for the row: "Telegram, @bot, Connected". */
export function telegramRowLabel(): string {
  const t = telegram();
  const status = t.serverId === daemon()?.server.id ? t.status : null;
  const row = telegramRow();
  return ["Telegram", status?.bot_username && connected() ? `@${status.bot_username}` : "", row.label ?? row.subtitle ?? ""].filter(Boolean).join(", ");
}
