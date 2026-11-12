import { cleanup, computed, effect, signal, Storage, untracked } from "nsh";
import {
  decodeComputers, otherComputerState, othersNeedYou, peerOf, relayDiagnosis, whenWords,
  type ComputerConnection, type ComputersView, type OtherComputerState,
} from "../../core/computer/computers";
import type { StoredServer } from "../../core/servers";
import { clock } from "../calendar/clock";
import { daemon } from "../more/daemon";
import { checks, currentConnectionIssue } from "../more/settings/computers";
import { current, problem } from "../session";

/**
 * The other computers as the current computer sees them, and its relay connection (daemon WIRE §5):
 * asked each time the phone connects, then kept as the computer sends it again unasked. Only the
 * current computer's message counts; a switch drops it before the next one's arrives.
 */
export const computersView = signal<ComputersView | null>(null);

/**
 * The current computer's last relay connection, kept across a restart: a phone that reaches it only
 * through the relay hears nothing while the relay is down, so it shows the last word it had.
 */
const lastConnection = signal<{ serverId: string; connection: ComputerConnection } | null>(null);
const CONNECTION_KEY = (serverId: string) => `mewla:computer-connection:v1:${serverId}`;

function keepConnection(serverId: string, connection: ComputerConnection | undefined): void {
  lastConnection.set(connection ? { serverId, connection } : null);
  const key = CONNECTION_KEY(serverId);
  void (connection ? Storage.setItem(key, JSON.stringify(connection)) : Storage.removeItem(key)).catch(() => {});
}

effect(() => {
  const bound = daemon();
  untracked(() => {
    computersView.set(null);
    lastConnection.set(null);
  });
  if (!bound) return;
  const serverId = bound.server.id;
  void Storage.getItem(CONNECTION_KEY(serverId)).then((raw) => {
    if (!raw || untracked(lastConnection) || untracked(daemon) !== bound) return;
    try {
      lastConnection.set({ serverId, connection: JSON.parse(raw) as ComputerConnection });
    } catch {
      // A record this version cannot read: the next message replaces it.
    }
  }).catch(() => {});
  const take = (frame: Record<string, unknown>) => {
    const view = decodeComputers(frame);
    if (!view || untracked(daemon) !== bound) return;
    computersView.set(view);
    keepConnection(serverId, view.connection);
  };
  // A computer too old for the request answers with an error: it has no view, and that is all.
  const ask = () => void bound.request("computers", {}, { reply: "computers", timeoutMs: 15_000 }).then(take, () => {});
  const stops = [bound.on("computers", take), bound.on("connected", ask)];
  if (bound.state === "connected") ask();
  cleanup(() => stops.forEach((stop) => stop()));
});

/** "13:40" today, else "Nov 11", in the phone's zone. */
export function when(at: number): string {
  const c = clock();
  const zone = c.zones.device;
  return whenWords(at, c.now, {
    time: (t) => c.format.time(t, zone),
    shortDate: (t) => c.format.shortDate(t, zone),
    sameDay: (a, b) => c.zones.dateKey(a, zone) === c.zones.dateKey(b, zone),
  });
}

/** Another computer's row: its state, its dot. */
export function otherState(server: StoredServer): OtherComputerState {
  return otherComputerState({ check: checks()[server.id], peer: peerOf(computersView(), server), when });
}

/** Another paired computer has requests open (the cat's dot, SPEC §1.3). */
export const otherRequests = computed(() => othersNeedYou(computersView()));

/** The current computer's own dated word on its relay ("Relay unreachable · 13:40"), for a relay-paired phone. */
export const currentRelayDiagnosis = computed((): string | null => {
  const server = current();
  const last = lastConnection();
  if (!server || server.transportKind !== "link" || !last || last.serverId !== server.id) return null;
  return relayDiagnosis(last.connection, when);
});

/**
 * Why the current computer cannot be reached, as the tray's offline notice says it: what the phone
 * found when it is the phone's to know (not paired, the wrong computer, a proxy), else the computer's
 * own dated word on its relay, else the phone's finding or the socket's reason.
 */
export function currentDiagnosis(): string | undefined {
  const issue = currentConnectionIssue();
  if (issue && issue.code !== "network_unreachable") return issue.title;
  return currentRelayDiagnosis() || issue?.title || problem() || undefined;
}
