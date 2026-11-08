import { useCallback, useEffect, useSyncExternalStore } from "react";
import { browserRequest } from "../services/browser";
import { browserIssue } from "../services/browserPageModel";
import { serverDisplayName } from "../services/serverDisplayName";
import type { StoredServer } from "../services/storage";
import { useCurrentServer } from "./currentServer";

/** How long a settings request may take before it counts as failed. */
let timeoutMs = 12_000;

/** Test seam: how long a request may take. */
export function setVirtualScreenTimeout(ms: number) {
  timeoutMs = ms;
}

interface Snapshot {
  /** The computer this answer belongs to; a server switch drops it. */
  server: string | null;
  /** Unknown until the computer has answered. */
  enabled: boolean | null;
  /** Why the last read or write failed, in a short sentence. */
  error: string | null;
  /** A write is in flight. */
  busy: boolean;
}

const EMPTY: Snapshot = { server: null, enabled: null, error: null, busy: false };

/**
 * One answer per app, shared by Settings → Labs and Screen so a change in one
 * shows in the other. It always belongs to the current computer: reading a
 * different one starts from nothing.
 */
let snapshot: Snapshot = EMPTY;
let reads = 0;
const listeners = new Set<() => void>();

function publish(next: Snapshot) {
  snapshot = next;
  listeners.forEach((listen) => listen());
}

function failure(error: unknown, server: StoredServer): string {
  const issue = browserIssue(error, serverDisplayName(server.name));
  return issue ? issue.title : "That didn't finish";
}

/** One settings request, ended with a network error when the computer does not answer. */
async function request(server: StoredServer, enabled?: boolean): Promise<boolean> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const result = await browserRequest(server, enabled === undefined ? { action: "settings" } : { action: "settings", enabled }, controller.signal);
    if (typeof result.virtual_screen !== "boolean") throw new Error("Browser request failed");
    return result.virtual_screen;
  } catch (error) {
    if (controller.signal.aborted) throw new TypeError("Network request failed");
    throw error;
  } finally {
    clearTimeout(timer);
  }
}

let reading: string | null = null;

export async function readVirtualScreenLabs(server: StoredServer) {
  // Screen and the switch on it mount together; one question is enough.
  if (reading === server.id) return;
  reading = server.id;
  const token = ++reads;
  if (snapshot.server !== server.id) publish({ ...EMPTY, server: server.id });
  try {
    const enabled = await request(server);
    if (token === reads) publish({ server: server.id, enabled, error: null, busy: false });
  } catch (error) {
    // A computer that cannot answer is not "off": say so and let the person retry.
    if (token === reads) publish({ server: server.id, enabled: snapshot.server === server.id ? snapshot.enabled : null, error: failure(error, server), busy: false });
  } finally {
    if (reading === server.id) reading = null;
  }
}

export async function writeVirtualScreenLabs(server: StoredServer, on: boolean, isCurrent: () => boolean) {
  const token = ++reads;
  reading = null;
  publish({ server: server.id, enabled: snapshot.server === server.id ? snapshot.enabled : null, error: null, busy: true });
  try {
    const enabled = await request(server, on);
    if (token === reads && isCurrent()) publish({ server: server.id, enabled, error: null, busy: false });
  } catch (error) {
    if (token === reads && isCurrent()) publish({ server: server.id, enabled: snapshot.enabled, error: failure(error, server), busy: false });
  }
}

/** What is known right now, for tests. */
export function virtualScreenSnapshot(): Readonly<Snapshot> {
  return snapshot;
}

/** Test seam: forget everything, as a fresh app start does. */
export function resetVirtualScreenLabs() {
  reads++;
  reading = null;
  publish(EMPTY);
}

/**
 * Labs → Virtual screen, read from and written to the current computer: it
 * decides whether that computer's Workers get a browser on its virtual screen
 * and whether Screen shows them. `refresh` reads it again, for a page that
 * comes back into view.
 */
export function useVirtualScreenLabs(): {
  enabled: boolean;
  /** The computer has answered, so the switch shows the truth. */
  ready: boolean;
  busy: boolean;
  error: string | null;
  set(on: boolean): Promise<void>;
  refresh(): void;
} {
  const { currentServer, isCurrentServer } = useCurrentServer();
  const state = useSyncExternalStore(
    (listen) => { listeners.add(listen); return () => { listeners.delete(listen); }; },
    () => snapshot,
  );
  const id = currentServer?.id;
  const mine = !!id && state.server === id;

  const refresh = useCallback(() => { if (currentServer) void readVirtualScreenLabs(currentServer); }, [currentServer]);
  useEffect(() => {
    if (!currentServer) return;
    void readVirtualScreenLabs(currentServer);
  }, [currentServer?.id]);

  const set = useCallback(async (on: boolean) => {
    if (!currentServer) return;
    await writeVirtualScreenLabs(currentServer, on, () => isCurrentServer(currentServer.id));
  }, [currentServer, isCurrentServer]);

  return {
    enabled: mine && state.enabled === true,
    ready: mine && state.enabled !== null,
    busy: mine && state.busy,
    error: mine ? state.error : null,
    set,
    refresh,
  };
}
