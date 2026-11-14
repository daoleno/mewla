/**
 * The Provider selection a new Session launches with, from the Expo app's
 * services/providers/presentation.ts (New Terminal from a Session sends it).
 */
import type { Daemon } from "../more/daemon";
import { listProviders } from "../providers/requests";
import {
  normalizeProviderClient,
  type ProviderClient,
  type ProvidersSnapshot,
} from "../sessions/providerTypes";

/**
 * The client the launch command targets, when it is a managed Provider client.
 * Shells and other executors return null (no Provider selection applies).
 */
export function providerClientForCommand(command: string): ProviderClient | null {
  const fields = command.trim().split(/\s+/).filter(Boolean);
  for (const field of fields) {
    if (field.includes("=")) continue;
    const bin = field.split("/").pop() ?? "";
    if (normalizeProviderClient(bin) === "codex") return "codex";
    if (normalizeProviderClient(bin) === "claude") return "claude";
  }
  return null;
}

/**
 * The authoritative connection selection for a new Session. Both Agents
 * inherit their local model configuration unless the Session requests one.
 */
export function launchSelectionFromSnapshot(
  snapshot: ProvidersSnapshot | null | undefined,
  client: ProviderClient | null,
): { connectionId: string; modelId: string } | null {
  if (!snapshot || !client) return null;
  const entry = snapshot.defaults[client];
  if (!entry?.connection_id) return null;
  const connection = snapshot.connections.find(
    (item) => item.id === entry.connection_id,
  );
  if (!connection) return null;
  return {
    connectionId: connection.id,
    modelId: "",
  };
}

/**
 * The Provider selection a New Terminal launches with (the Expo app's
 * resolveLaunchSelection): the default connection of the command's agent.
 * Best effort: a failure to read Providers returns null, and the computer
 * resolves the connection itself.
 */
export async function resolveLaunchSelection(daemon: Daemon, command: string): Promise<{ connectionId: string; modelId: string } | null> {
  const client = providerClientForCommand(command);
  if (!client) return null;
  try {
    return launchSelectionFromSnapshot(await listProviders(daemon), client);
  } catch {
    return null;
  }
}
