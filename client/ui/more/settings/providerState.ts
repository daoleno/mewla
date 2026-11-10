import { signal } from "nsh";
import { ProviderError } from "../../../core/providers/errors";
import { durabilityWarningMessage } from "../../../core/providers/persistence";
import { listProviders } from "../../../core/providers/requests";
import type { ProvidersMutationResult, ProvidersSnapshot } from "../../../core/providers/types";
import { daemon } from "../daemon";

/**
 * The current computer's Providers, as the Providers pages share them. One change
 * at a time; a reply older than the catalog held is dropped; a change whose outcome
 * is unknown locks changes until a refresh succeeds. A switch drops it all.
 */
export const providers = signal<{ serverId: string; snapshot: ProvidersSnapshot | null; loading: boolean; error: ProviderError | null }>({
  serverId: "",
  snapshot: null,
  loading: false,
  error: null,
});
/** What is being changed now ("" for nothing): only that control spins, the rest wait. */
export const changing = signal("");
/** Changes wait for a refresh: the last one may or may not have applied. */
export const locked = signal(false);
/** A change saved with a warning (not durable). */
export const warning = signal("");

let epoch = 0;

export function snapshot(): ProvidersSnapshot | null {
  const p = providers();
  return p.serverId === daemon()?.server.id ? p.snapshot : null;
}

export function refreshProviders(): void {
  const current = daemon();
  if (!current || current.state !== "connected") return;
  const mine = ++epoch;
  const serverId = current.server.id;
  const held = providers().serverId === serverId ? providers().snapshot : null;
  providers.set({ serverId, snapshot: held, loading: true, error: null });
  listProviders(current)
    .then((next) => {
      if (mine !== epoch) return;
      providers.set({ serverId, snapshot: next, loading: false, error: null });
      locked.set(false);
    })
    .catch((error: unknown) => {
      if (mine !== epoch) return;
      providers.set({ serverId, snapshot: held, loading: false, error: error instanceof ProviderError ? error : new ProviderError("unknown", String(error), "unknown", true) });
    });
}

/** Forgets the last computer's Providers. */
export function forgetProviders(): void {
  epoch++;
  providers.set({ serverId: daemon()?.server.id ?? "", snapshot: null, loading: false, error: null });
  locked.set(false);
  warning.set("");
  changing.set("");
}

/**
 * Runs one change. It is refused while another runs or changes are locked; its reply
 * replaces the catalog only when it is not older than the one held.
 */
export async function change(name: string, work: () => Promise<ProvidersMutationResult>): Promise<ProvidersMutationResult> {
  if (changing()) throw new ProviderError("busy", "Another Provider change is still running.", "busy", false);
  if (locked()) throw new ProviderError("locked", "Changes are paused until this list is refreshed.", "conflict", true);
  const current = daemon();
  if (!current || current.state !== "connected") throw new ProviderError("offline", "Connect the current server first.", "offline", true);
  const serverId = current.server.id;
  changing.set(name);
  try {
    const result = await work();
    if (daemon()?.server.id !== serverId) return result;
    const held = snapshot();
    if (!held || result.snapshot.revision >= held.revision) providers.set({ serverId, snapshot: result.snapshot, loading: false, error: null });
    if (result.persistence.ambiguous) locked.set(true);
    warning.set(result.persistence.durable ? "" : durabilityWarningMessage(result.persistence));
    return result;
  } catch (error) {
    if (error instanceof ProviderError && error.code === "providers_invalid_reply") locked.set(true);
    throw error;
  } finally {
    changing.set("");
  }
}

/** "Models synced 5m ago": how fresh a connection's model list is. */
export function modelsAge(fetchedAt: string | undefined, stale: boolean | undefined, now = Date.now()): string {
  if (!fetchedAt) return "Models not synced";
  if (stale) return "Models stale";
  const at = Date.parse(fetchedAt);
  if (!Number.isFinite(at)) return "Models synced";
  const minutes = Math.floor((now - at) / 60_000);
  if (minutes < 1) return "Models just now";
  if (minutes < 60) return `Models ${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `Models ${hours}h ago`;
  return `Models ${Math.floor(hours / 24)}d ago`;
}
