import { signal } from "nsh";
import type { Daemon } from "../../core/more/daemon";
import type { ConnectionState } from "../../core/transport";

/**
 * The current computer as the More pages see it. `Pages.tsx` binds it to the
 * session (`onTransport`); a page test binds a fake. A switch rebinds it, and
 * every page drops what it held for the previous computer when `daemon()` changes.
 */
export const daemon = signal<Daemon | null>(null);
export const daemonState = signal<ConnectionState>("idle");

let stopState: (() => void) | null = null;

export function bindDaemon(next: Daemon | null): void {
  stopState?.();
  stopState = null;
  daemon.set(next);
  daemonState.set(next?.state ?? "idle");
  if (next) stopState = next.onState((state) => daemonState.set(state));
}

/** Whether the current computer answers now. */
export function connected(): boolean {
  return daemonState() === "connected";
}
