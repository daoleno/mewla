import { computed, effect, signal, untracked } from "nsh";
import { needsFixing, catNeedsYouOf, type FixRow } from "../../core/computer/needsFixing";
import { getAgentUse, getDaemonUpdate, MIN_COMPUTER_VERSION, type AgentUseStatus, type DaemonUpdateStatus } from "../../core/settings/computer";
import { effectiveMinComputerVersion } from "../../core/update/version";
import manifest from "../nsh.json";
import { checks, currentConnectionIssue } from "../more/settings/computers";
import { brain, connection, current, problem, servers } from "../session";

/**
 * What the computer page and home's cat know about the current computer beyond its connection:
 * its Mewla version and update, and what agents need there. Read once each time the phone
 * connects to it (so the cat's dot is right before the page is ever opened); the page reads
 * again while what it shows can change.
 */
export const computerUpdate = signal<DaemonUpdateStatus | null>(null);
export const computerAgentUse = signal<AgentUseStatus | null>(null);

export const minComputerVersion = () => effectiveMinComputerVersion(MIN_COMPUTER_VERSION, manifest.version);

/** Asks the current computer for its update and agent use; a miss keeps what was known. */
export function readComputer(): void {
  const server = untracked(current);
  if (!server) return;
  void getDaemonUpdate(server).then((next) => current()?.id === server.id && computerUpdate.set(next)).catch(() => {});
  void getAgentUse(server).then((next) => current()?.id === server.id && computerAgentUse.set(next)).catch(() => {});
}

let readId = "";
let readUp = false;
let following = false;

function followComputer(): void {
  if (following) return;
  following = true;
  effect(() => {
    const id = current()?.id ?? "";
    const up = connection() === "connected";
    untracked(() => {
      if (id !== readId) {
        // Another computer (or none): nothing of the last one stays.
        computerUpdate.set(null);
        computerAgentUse.set(null);
      }
      const read = id && up && (id !== readId || !readUp);
      readId = id;
      readUp = up;
      if (read) readComputer();
    });
  });
}

followComputer();

/** The current computer's Needs fixing rows (SPEC §3.6). */
export const fixRows = computed<FixRow[]>(() => {
  const server = current();
  if (!server) return [];
  const host = brain()?.host_problem;
  return needsFixing({
    connection: connection(),
    problem: problem(),
    issue: currentConnectionIssue(),
    check: checks()[server.id],
    relay: server.transportKind === "link",
    hostProblem: host ? { code: host.code, executor: host.executor } : undefined,
    agentUse: computerAgentUse(),
    update: computerUpdate(),
    minVersion: minComputerVersion(),
  });
});

/**
 * The red dot on home's cat (SPEC §1.3): this computer must be paired again or updated to work
 * with this app, or another computer must be paired again. Another computer's open requests are
 * not known to the phone while it is not connected to it.
 */
export const catNeedsYou = computed(() => {
  const id = current()?.id;
  return catNeedsYouOf({ rows: fixRows(), otherChecks: servers().filter((s) => s.id !== id).map((s) => checks()[s.id]) });
});
