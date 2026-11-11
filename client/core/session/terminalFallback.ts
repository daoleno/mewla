import type { ConnectionState } from "../transport";

export interface TerminalFallback {
  busy: boolean;
  title: string;
  detail: string;
  hint: string;
}

/**
 * What a Session's terminal says while its computer can't be reached (Expo's
 * fallback card): reconnecting, or unavailable with the connection's own words.
 * Null while connected.
 */
export function terminalFallback(state: ConnectionState, problem: string): TerminalFallback | null {
  if (state === "connected") return null;
  const connecting = state === "connecting";
  return {
    busy: connecting,
    title: connecting ? "Reconnecting to daemon" : "Daemon unavailable",
    detail: connecting
      ? "Mewla is reconnecting before reopening this terminal."
      : problem || "Start mewla on that machine, or bring the network or tunnel back.",
    hint: "This terminal will reopen automatically once the daemon is reachable again.",
  };
}
