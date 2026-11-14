import type { ConnectionState } from "../transport";
import type { AgentUseFix, AgentUseStatus, ComputerCheck, DaemonUpdateStatus } from "../settings/computer";
import type { ConnectionIssue } from "../settings/connectionIssue";
import { compareVersions } from "../update/version";

/**
 * The computer page's "Needs fixing" (soft-final SPEC §1.3, §3.6): one row per thing on this
 * computer the person can fix, named by its diagnosis, not the socket's reason, each with its
 * one fix. Pure: the page feeds it what the client knows and does the fix a row names.
 */

/** What a row's button does. */
export type FixAction =
  | { kind: "retry" }
  | { kind: "pair" }
  | { kind: "switch_executor" }
  | { kind: "agent_use"; fix: AgentUseFix }
  | { kind: "update" };

export interface FixRow {
  key: string;
  /** The diagnosis, as the row says it. */
  title: string;
  /** The button's word; none while it is under way (Updating…). */
  button?: string;
  action?: FixAction;
  /** Red on the cat: the computer cannot work for this phone until it is fixed (pair again, a required update). */
  blocking: boolean;
}

export interface NeedsFixingInput {
  connection: ConnectionState;
  /** The socket's last failure (session `problem()`), empty when none. */
  problem: string;
  /** What the phone found when it looked into the failure, if it has. */
  issue: ConnectionIssue | null;
  /** The current computer's last check (its `/auth-check`), if Settings or this page asked. */
  check: ComputerCheck | undefined;
  /** Paired through Mewla Relay. */
  relay: boolean;
  /** The computer's own last word on its relay connection, dated ("Relay unreachable · 13:40"; core/computer/computers `relayDiagnosis`). */
  relayDiagnosis?: string | null;
  /** Brain's host problem from the snapshot (`brain.host_problem`). */
  hostProblem: { code: string; executor: string } | undefined;
  agentUse: AgentUseStatus | null;
  update: DaemonUpdateStatus | null;
  /** The oldest computer version this app works with. */
  minVersion: string;
}

const EXECUTOR_NAMES: Record<string, string> = { claude: "Claude", codex: "Codex", agent: "Cursor", grok: "Grok", gemini: "Gemini" };

function executorName(id: string): string {
  return EXECUTOR_NAMES[id] ?? (id ? id[0]!.toUpperCase() + id.slice(1) : "The agent");
}

/** The connection's one problem, while the phone is not connected. */
function connectionRow(input: NeedsFixingInput): FixRow | null {
  if (input.connection === "connected" || input.connection === "connecting") return null;
  if (input.issue?.code === "device_not_paired" || input.check?.state === "unpaired") {
    return { key: "pair", title: "Device not paired", button: "Pair", action: { kind: "pair" }, blocking: true };
  }
  // Through the relay, the computer's own dated word on it says more than "unreachable"; any other
  // finding of the phone's (the wrong computer, a proxy, a refused upgrade) is the phone's to name.
  const relayWords = input.relay ? input.relayDiagnosis || "Relay unreachable" : null;
  if (input.issue) {
    const title = input.issue.code === "network_unreachable" ? relayWords ?? "Computer unreachable" : input.issue.title;
    return { key: "connection", title, button: "Try again", action: { kind: "retry" }, blocking: false };
  }
  if (input.connection === "waiting" && input.problem) {
    return { key: "connection", title: relayWords ?? "Can't connect", button: "Try again", action: { kind: "retry" }, blocking: false };
  }
  if (input.connection === "idle") return { key: "connection", title: "Not connected", button: "Try again", action: { kind: "retry" }, blocking: false };
  return null;
}

/** Brain's agent cannot start here for a reason the person fixes by choosing another (or signing in). */
function hostRow(input: NeedsFixingInput): FixRow | null {
  const problem = input.hostProblem;
  if (!problem || input.connection !== "connected") return null;
  const who = executorName(problem.executor);
  if (problem.code === "not_signed_in") return { key: "host", title: `${who} is not signed in`, button: "Switch", action: { kind: "switch_executor" }, blocking: false };
  if (problem.code === "not_installed") return { key: "host", title: `${who} is not installed`, button: "Switch", action: { kind: "switch_executor" }, blocking: false };
  return null;
}

/** What agents need on this computer: the Chrome extension, the screen permission. */
function agentUseRows(status: AgentUseStatus | null): FixRow[] {
  if (!status?.enabled) return [];
  const rows: FixRow[] = [];
  const browser = status.browser;
  // No extension at all is not a problem for someone who never started browser setup (the details page offers it).
  if (browser.state === "no_key" && browser.fix) {
    rows.push({ key: "browser", title: "Chrome extension missing", button: "Fix", action: { kind: "agent_use", fix: browser.fix }, blocking: false });
  } else if (browser.state === "failed") {
    rows.push({ key: "browser", title: "Browser tools didn't install", button: "Try again", action: { kind: "agent_use", fix: browser.fix ?? "retry" }, blocking: false });
  }
  const computer = status.computer;
  if (computer?.state === "permission" && computer.fix) {
    rows.push({ key: "screen", title: computer.permission ? `${computer.permission} access not allowed` : "Screen access not allowed", button: "Fix", action: { kind: "agent_use", fix: computer.fix }, blocking: false });
  }
  return rows;
}

/** The computer's Mewla: required (red), under way, or failed. An optional update is not here: see `optionalUpdate`. */
function updateRow(update: DaemonUpdateStatus | null, minVersion: string): FixRow | null {
  if (!update) return null;
  if (update.updating) return { key: "update", title: "Updating…", blocking: false };
  if (compareVersions(update.version, minVersion) === -1) {
    return { key: "update", title: "Update required", ...(update.supported ? { button: "Update", action: { kind: "update" } as FixAction } : {}), blocking: true };
  }
  if (update.error) return { key: "update", title: update.error, ...(update.supported && update.latest ? { button: "Try again", action: { kind: "update" } as FixAction } : {}), blocking: false };
  return null;
}

export function needsFixing(input: NeedsFixingInput): FixRow[] {
  const connection = connectionRow(input);
  // Offline, only the connection can be fixed: what the computer reported before is stale.
  if (connection) return [connection];
  // Connecting: wait for the computer before naming what it reported last time.
  if (input.connection !== "connected") return [];
  return [hostRow(input), ...agentUseRows(input.agentUse), updateRow(input.update, input.minVersion)].filter((row): row is FixRow => Boolean(row));
}

/** A newer Mewla the computer can install, when nothing requires it: a quiet row, no dot. */
export function optionalUpdate(update: DaemonUpdateStatus | null, minVersion: string): string | null {
  if (!update || update.updating || !update.supported || !update.latest || update.error) return null;
  if (compareVersions(update.version, minVersion) === -1) return null;
  return update.latest;
}

/** The button of a row is ink when it is the page's one fix, ghost otherwise (SPEC §11). */
export function fixButtonKind(rows: readonly FixRow[]): "ink" | "ghost" {
  return rows.filter((row) => row.action).length === 1 ? "ink" : "ghost";
}

/**
 * The red dot on home's cat: something behind the cat needs the person. This computer must be paired
 * again or updated to work with this app, or another computer must be paired again or has requests
 * open (its row's dot: core/computer/computers `otherComputerState`).
 */
export function catNeedsYouOf(input: { rows: readonly FixRow[]; others: ReadonlyArray<{ needsYou: boolean }> }): boolean {
  return input.rows.some((row) => row.blocking) || input.others.some((other) => other.needsYou);
}
