import { signal } from "nsh";
import { getCurrentServerId, getServers, markOnboarded, saveServer, setCurrentServerId, type StoredServer } from "../core/servers";
import { Transport, type ConnectionState } from "../core/transport";

/**
 * Everything that belongs to the current computer. There is exactly one; switching
 * computers closes this one's transport and starts a fresh session, so nothing of the
 * previous computer's state survives into the next.
 */
export const servers = signal<StoredServer[]>([]);
export const current = signal<StoredServer | null>(null);
export const connection = signal<ConnectionState>("idle");
export const problem = signal("");
export const ready = signal(false);

let transport: Transport | null = null;
let stopState: (() => void) | null = null;

export function currentTransport(): Transport | null {
  return transport;
}

export async function loadSession(): Promise<void> {
  const list = await getServers();
  servers.set(list);
  const id = await getCurrentServerId();
  switchTo(list.find((s) => s.id === id) ?? list[0] ?? null);
  ready.set(true);
}

export function switchTo(server: StoredServer | null): void {
  stopState?.();
  transport?.close();
  transport = null;
  current.set(server);
  connection.set("idle");
  problem.set("");
  if (!server) return;
  void setCurrentServerId(server.id);
  const next = new Transport(server);
  transport = next;
  stopState = next.onState((state) => {
    connection.set(state);
    problem.set(next.problem);
  });
  next.connect();
}

export async function addPairedServer(server: Parameters<typeof saveServer>[0]): Promise<void> {
  const saved = await saveServer(server);
  await markOnboarded();
  servers.set(await getServers());
  switchTo(saved);
}

export function resumeConnection(): void {
  transport?.resume();
}
