import { getOrCreateLocalDeviceIdentity } from "../auth";
import { verifyDaemonAssertion } from "../deviceAuthContract";
import { bodyPurpose, sha256Hex, signedFetch, signedJSON } from "../more/daemon";
import type { StoredServer } from "../storedServerContract";

/** One computer as Settings shows it: whether it answers, what agents may use there, its Mewla. */

export type AgentUseFix = "install_extension" | "open_extension" | "retry" | "open_settings";

/** What agents can use on one computer, as that computer reports it. */
export interface AgentUseStatus {
  /** The one switch: agents can use this computer. On by default. */
  enabled: boolean;
  browser: {
    state: "ready" | "off" | "no_chrome" | "no_extension" | "no_key" | "no_node" | "no_desktop" | "preparing" | "failed";
    /** One line to show as is. */
    summary: string;
    fix?: AgentUseFix;
    profile?: string;
    key_pasted?: boolean;
  };
  /** The screen, keyboard and pointer; reported by computers with Screen. */
  computer?: {
    state: "ready" | "off" | "unsupported" | "screen_off" | "permission" | "locked" | "unavailable";
    summary: string;
    fix?: "open_settings";
    permission?: string;
    active?: { worker: string; name?: string; since: string };
    agents?: { worker: string; name?: string; since: string }[];
  };
}

export type AgentUseChange = { enabled: boolean } | { fix: AgentUseFix } | { key: string } | { stop_agent: true };

/** What a computer reports about its own Mewla. */
export interface DaemonUpdateStatus {
  version: string;
  /** False for builds that do not update themselves (development builds, betas). */
  supported: boolean;
  latest?: string;
  updating?: boolean;
  /** Why the last update did not take; the previous version kept running. */
  error?: string;
  /** Sessions an earlier Mewla started in tmux that could not move to the session host. */
  sessions_in_tmux?: number;
}

export type ComputerCheck = { state: "online"; name?: string } | { state: "unpaired" } | { state: "offline" };

/** The oldest computer this app works with. */
export const MIN_COMPUTER_VERSION = "0.2.88";

export function getAgentUse(server: StoredServer): Promise<AgentUseStatus> {
  return signedJSON<AgentUseStatus>(server, "GET", "/agent-use", "mewla-agent-use:GET:/agent-use");
}

/** Applies one change. The signature covers the body, which may carry a key. */
export function changeAgentUse(server: StoredServer, change: AgentUseChange): Promise<AgentUseStatus> {
  return signedJSON<AgentUseStatus>(server, "POST", "/agent-use", bodyPurpose("mewla-agent-use:POST:/agent-use"), { body: change });
}

/** The computer's version and update, or null when it is too old to report one. */
export async function getDaemonUpdate(server: StoredServer): Promise<DaemonUpdateStatus | null> {
  const response = await signedFetch(server, "GET", "/daemon-update", "mewla-daemon-update:GET:/daemon-update");
  if (response.status === 404 || response.status === 503) return null;
  if (!response.ok) throw new Error((await response.text().catch(() => "")).trim() || `${server.name} answered ${response.status}`);
  return (await response.json()) as DaemonUpdateStatus;
}

/** Updates the computer now: it downloads, verifies, restarts, and keeps the old version if the new one will not start. */
export function installDaemonUpdate(server: StoredServer): Promise<DaemonUpdateStatus> {
  return signedJSON<DaemonUpdateStatus>(server, "POST", "/daemon-update", "mewla-daemon-update:POST:/daemon-update");
}

/**
 * Whether a computer, current or not, is up and still trusts this phone, and what it
 * calls itself. Something else answering at the address is not this computer.
 */
export async function checkComputer(server: StoredServer): Promise<ComputerCheck> {
  try {
    const response = await signedFetch(server, "GET", "/auth-check", "mewla-probe", { timeoutMs: 8_000 });
    if (response.status === 401) return { state: "unpaired" };
    if (!response.ok) return { state: "offline" };
    const body = (await response.json()) as { name?: unknown; assertion_timestamp?: string; assertion_nonce?: string; assertion_signature?: string };
    const genuine = verifyDaemonAssertion({
      purpose: "mewla-probe",
      daemonId: server.daemonId,
      daemonPublicKey: server.daemonPublicKey,
      timestamp: body.assertion_timestamp,
      nonceHex: body.assertion_nonce,
      signatureHex: body.assertion_signature,
    });
    if (!genuine) return { state: "offline" };
    const name = typeof body.name === "string" ? body.name.trim() : "";
    return name ? { state: "online", name } : { state: "online" };
  } catch {
    return { state: "offline" };
  }
}

/**
 * Asks the computer to forget this phone before the phone forgets it. Best effort and
 * bounded (3 s): an offline computer never blocks the removal.
 */
export async function revokeThisDevice(server: StoredServer): Promise<boolean> {
  try {
    const identity = await getOrCreateLocalDeviceIdentity();
    const target = await sha256Hex(identity.deviceId);
    const response = await signedFetch(server, "DELETE", "/devices", `mewla-device-admin:revoke:DELETE:/devices:sha256=${target}`, {
      body: { device_id: identity.deviceId },
      timeoutMs: 3_000,
    });
    return response.ok;
  } catch {
    return false;
  }
}

interface SemVersion {
  core: [number, number, number];
  prerelease: string[];
}

function parseVersion(raw: string): SemVersion | null {
  const value = raw.trim().replace(/^v/, "").split("+", 1)[0];
  const match = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-(.+))?$/.exec(value);
  if (!match) return null;
  const prerelease = match[4] === undefined ? [] : match[4].split(".");
  if (prerelease.some((part) => !/^[0-9A-Za-z-]+$/.test(part) || (/^\d+$/.test(part) && part.length > 1 && part[0] === "0"))) return null;
  const core = [Number(match[1]), Number(match[2]), Number(match[3])] as [number, number, number];
  return core.every(Number.isSafeInteger) ? { core, prerelease } : null;
}

/** SemVer precedence, as the computer's self-update compares; null when either is no version. */
export function compareVersions(left: string, right: string): number | null {
  const a = parseVersion(left);
  const b = parseVersion(right);
  if (!a || !b) return null;
  for (let index = 0; index < 3; index += 1) {
    if (a.core[index] !== b.core[index]) return a.core[index] < b.core[index] ? -1 : 1;
  }
  if (!a.prerelease.length || !b.prerelease.length) return a.prerelease.length === b.prerelease.length ? 0 : a.prerelease.length ? -1 : 1;
  for (let index = 0; index < Math.min(a.prerelease.length, b.prerelease.length); index += 1) {
    const x = a.prerelease[index];
    const y = b.prerelease[index];
    if (x === y) continue;
    const xn = /^\d+$/.test(x);
    const yn = /^\d+$/.test(y);
    if (xn && yn) return Number(x) < Number(y) ? -1 : 1;
    if (xn !== yn) return xn ? -1 : 1;
    return x < y ? -1 : 1;
  }
  return a.prerelease.length === b.prerelease.length ? 0 : a.prerelease.length < b.prerelease.length ? -1 : 1;
}
