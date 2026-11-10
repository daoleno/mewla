import { platform } from "./platform";
import {
  mergeStoredServer,
  normalizeStoredServers,
  type StoredServer,
  type StoredServerInput,
  type StoredTransportCandidate,
} from "./storedServerContract";

export { serverAddressForDisplay, serverAddressProblem, serverNamedByAddress } from "./storedServerContract";
export type { ServerTransportKind, StoredServer, StoredTransportCandidate } from "./storedServerContract";

// The keys are the ones the previous app wrote; the host copies them over on upgrade.
export const SERVER_KEYS = {
  servers: "mewla:v3:servers",
  currentServer: "mewla:v1:current_server_id",
  disabledServers: "mewla:v1:disabled_servers",
  onboarded: "mewla:onboarded",
} as const;

let writeTail: Promise<unknown> = Promise.resolve();
function serialized<T>(action: () => Promise<T>): Promise<T> {
  const result = writeTail.then(action, action);
  writeTail = result.then(() => undefined, () => undefined);
  return result;
}

const store = () => platform().storage;

export async function getServers(): Promise<StoredServer[]> {
  const value = await store().getItem(SERVER_KEYS.servers);
  if (!value) return [];
  try {
    return normalizeStoredServers(JSON.parse(value) as unknown);
  } catch {
    return [];
  }
}

export function saveServer(input: StoredServerInput): Promise<StoredServer> {
  return serialized(async () => {
    const next = mergeStoredServer(input, await getServers(), createServerId);
    await store().setItem(SERVER_KEYS.servers, JSON.stringify(next.servers));
    return next.server;
  });
}

/** Renames a server or moves its address, keeping every other field. */
export async function updateServer(
  serverId: string,
  change: { name?: string; url?: string; transportCandidates?: StoredTransportCandidate[] },
): Promise<StoredServer | null> {
  const existing = (await getServers()).find((server) => server.id === serverId);
  return existing ? saveServer({ ...existing, ...change }) : null;
}

export function removeServer(serverId: string): Promise<void> {
  return serialized(async () => {
    const next = (await getServers()).filter((server) => server.id !== serverId);
    await store().setItem(SERVER_KEYS.servers, JSON.stringify(next));
    await setServerAutoConnect(serverId, true);
  });
}

export async function getCurrentServerId(): Promise<string | null> {
  return (await store().getItem(SERVER_KEYS.currentServer))?.trim() || null;
}

export async function setCurrentServerId(serverId: string | null): Promise<void> {
  const id = serverId?.trim();
  if (id) await store().setItem(SERVER_KEYS.currentServer, id);
  else await store().removeItem(SERVER_KEYS.currentServer);
}

export async function isOnboarded(): Promise<boolean> {
  return (await store().getItem(SERVER_KEYS.onboarded)) === "true";
}

export async function markOnboarded(): Promise<void> {
  await store().setItem(SERVER_KEYS.onboarded, "true");
}

export async function getDisabledServerIds(): Promise<string[]> {
  const value = await store().getItem(SERVER_KEYS.disabledServers);
  if (!value) return [];
  try {
    return uniqueIds(JSON.parse(value));
  } catch {
    return [];
  }
}

export async function setServerAutoConnect(serverId: string, enabled: boolean): Promise<void> {
  const id = serverId.trim();
  if (!id) return;
  const current = await getDisabledServerIds();
  const next = enabled ? current.filter((x) => x !== id) : uniqueIds([...current, id]);
  if (next.length === 0) await store().removeItem(SERVER_KEYS.disabledServers);
  else await store().setItem(SERVER_KEYS.disabledServers, JSON.stringify(next));
}

function uniqueIds(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return [...new Set(value.filter((x): x is string => typeof x === "string" && x.trim().length > 0))];
}

function createServerId(): string {
  return `server_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
}
