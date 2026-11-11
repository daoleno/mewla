import { signal } from "nsh";
import type { Daemon } from "../../core/more/daemon";
import type { ConnectionState } from "../../core/transport";
import { current } from "../session";

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

/**
 * The current computer's name as stored now. The connection keeps the record it was made with,
 * so a computer that took its reported name in Settings (paired by its address) stayed "this
 * computer" on these pages until the app started again.
 */
export function daemonName(): string | undefined {
  const bound = daemon()?.server;
  if (!bound) return undefined;
  const stored = current();
  return stored && stored.id === bound.id ? stored.name : bound.name;
}
