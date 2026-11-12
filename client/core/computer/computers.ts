/**
 * The other computers, as the current computer sees them (daemon WIRE §5, the `computers` message):
 * each paired computer's reachability, last seen and open requests, this computer's own count, and
 * its remote access connection. One source: the current computer's message. The phone does not ask
 * each computer for its state. Pure: ui/computer/computers.ts feeds it the frames.
 */
import type { ComputerCheck } from "../settings/computer";
import type { StoredServer } from "../storedServerContract";

export interface PairedComputer {
  daemonId?: string;
  name: string;
  /** This computer's channel to it is up now. */
  online: boolean;
  /** The newest frame on that channel (ms); absent: never seen. */
  lastSeen?: number;
  /** Its own count, only while online; absent means unknown, never 0. */
  openRequests?: number;
  version?: string;
}

export interface ComputerConnection {
  phase: "connecting" | "connected" | "offline";
  relay?: string;
  cause?: string;
  connectedAt?: number;
  /** The newest cause of the newest failed stretch, and when that stretch began. */
  lastFailure?: string;
  lastFailureAt?: number;
}

export interface ComputersView {
  computers: PairedComputer[];
  /** The current computer's own count. */
  openRequests: number;
  /** Absent while remote access is off, or with a self-hosted relay. */
  connection?: ComputerConnection;
}

const str = (value: unknown): string | undefined => (typeof value === "string" && value.trim() ? value.trim() : undefined);
const count = (value: unknown): number | undefined => (typeof value === "number" && Number.isFinite(value) && value >= 0 ? Math.floor(value) : undefined);
const time = (value: unknown): number | undefined => {
  const at = typeof value === "string" ? Date.parse(value) : NaN;
  return Number.isFinite(at) ? at : undefined;
};

export function decodeConnection(raw: unknown): ComputerConnection | undefined {
  if (!raw || typeof raw !== "object") return undefined;
  const c = raw as Record<string, unknown>;
  const phase = c.phase === "connecting" || c.phase === "connected" || c.phase === "offline" ? c.phase : undefined;
  if (!phase) return undefined;
  return {
    phase,
    relay: str(c.relay),
    cause: str(c.cause),
    connectedAt: time(c.connected_at),
    lastFailure: str(c.last_failure),
    lastFailureAt: time(c.last_failure_at),
  };
}

/** A `computers` frame, or null when it is not one this client can read. */
export function decodeComputers(frame: Record<string, unknown>): ComputersView | null {
  if (!Array.isArray(frame.computers)) return null;
  const computers: PairedComputer[] = [];
  for (const raw of frame.computers) {
    if (!raw || typeof raw !== "object") continue;
    const c = raw as Record<string, unknown>;
    const online = c.online === true;
    computers.push({
      daemonId: str(c.daemon_id)?.toLowerCase(),
      name: str(c.name) ?? "",
      online,
      lastSeen: time(c.last_seen),
      // Offline is unknown: a count from before it went away says nothing now.
      openRequests: online ? count(c.open_requests) : undefined,
      version: str(c.version),
    });
  }
  return { computers, openRequests: count(frame.open_requests) ?? 0, connection: decodeConnection(frame.connection) };
}

/** The paired computer that is this stored one, by its daemon id. */
export function peerOf(view: ComputersView | null, server: Pick<StoredServer, "daemonId">): PairedComputer | undefined {
  const id = server.daemonId?.toLowerCase();
  return id ? view?.computers.find((peer) => peer.daemonId === id) : undefined;
}

export interface OtherComputerState {
  /** The words under its name: "Online · 1 needs you", "Offline · last seen 13:40", "Pair again". */
  word: string;
  /** Its row's red dot (and the cat's): it forgot this phone, or something there waits on the person. */
  needsYou: boolean;
  dim: boolean;
}

/**
 * Another computer's row (SPEC §1.3 "Computers"). The phone's own record says when it must be paired
 * again; the rest is the current computer's view of it. A computer the current one does not know (not
 * joined yet) says what the phone found when it last looked, or nothing.
 */
export function otherComputerState(input: { check?: ComputerCheck; peer?: PairedComputer; when: (at: number) => string }): OtherComputerState {
  if (input.check?.state === "unpaired") return { word: "Pair again", needsYou: true, dim: false };
  const peer = input.peer;
  if (peer) {
    if (peer.online) {
      const n = peer.openRequests ?? 0;
      return n > 0 ? { word: `Online · ${n === 1 ? "1 needs you" : `${n} need you`}`, needsYou: true, dim: false } : { word: "Online", needsYou: false, dim: false };
    }
    return { word: peer.lastSeen ? `Offline · last seen ${input.when(peer.lastSeen)}` : "Offline", needsYou: false, dim: true };
  }
  if (input.check?.state === "online") return { word: "Online", needsYou: false, dim: false };
  if (input.check?.state === "offline") return { word: "Offline", needsYou: false, dim: true };
  return { word: "", needsYou: false, dim: false };
}

/** Another paired computer has something open for the person (offline is unknown: no dot). */
export function othersNeedYou(view: ComputersView | null): boolean {
  return Boolean(view?.computers.some((peer) => peer.online && (peer.openRequests ?? 0) > 0));
}

/**
 * The current computer's own word on why it cannot be reached through Mewla's relay, with when that
 * began: "Relay unreachable · 13:40". None while its relay connection is up, or when it said no cause.
 */
export function relayDiagnosis(connection: ComputerConnection | undefined, when: (at: number) => string): string | null {
  if (!connection || connection.phase === "connected") return null;
  const cause = connection.lastFailure ?? connection.cause;
  if (!cause) return null;
  const words = cause[0]!.toUpperCase() + cause.slice(1);
  return connection.lastFailureAt ? `${words} · ${when(connection.lastFailureAt)}` : words;
}

/** "13:40" today, else "Nov 11": how a last-seen or a failure time reads. */
export function whenWords(at: number, now: number, format: { time: (at: number) => string; shortDate: (at: number) => string; sameDay: (a: number, b: number) => boolean }): string {
  return format.sameDay(at, now) ? format.time(at) : format.shortDate(at);
}
