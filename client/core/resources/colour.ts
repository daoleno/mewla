import { meterTone, rowLeading, type RowLeading, type StateTone } from "../more/colour";
import { agentKindFromProvider, type AgentKind } from "../sessions/workerPresentation";
import type { ResourceConsumer, ResourcePressureState, ResourceTelemetry } from "./resourceTelemetry";

/**
 * Resources' colour (soft-final SPEC §10.4): the state the daemon reports, never one the client makes up. A meter
 * turns `warn` or red on the daemon's own thresholds (the signals it sends once one is crossed); a metric the
 * daemon has no threshold for (swap, a single core) uses the defaults, 80 % and 95 %.
 */

/** The page's state: the daemon's pressure. Comfortable is ok, elevated warn, critical red. */
export function pressureTone(state: ResourcePressureState): StateTone {
  return state === "critical" ? "error" : state === "elevated" ? "warn" : "ok";
}

export type ResourceMeter = "cpu" | "memory" | "disk";

/** Which meter a daemon signal is about ("memory.available_percent", "psi.cpu.some.avg10", "disk.free_percent:/"). */
function signalMeter(name: string): ResourceMeter | null {
  const about = name.replace(/^psi\./, "").split(/[.:]/)[0];
  return about === "memory" || about === "cpu" ? about : about === "disk" ? "disk" : null;
}

/** The mount a disk signal is about, or null for any other signal. */
function signalMount(name: string): string | null {
  const at = name.indexOf("disk.free_percent:");
  return at === 0 ? name.slice("disk.free_percent:".length) : null;
}

const RANK: Record<ResourcePressureState, number> = { normal: 0, elevated: 1, critical: 2 };

/** The worst state the daemon reports for `meter` (for a disk, for its `mount`), or normal. */
export function meterState(t: ResourceTelemetry, meter: ResourceMeter, mount?: string): ResourcePressureState {
  let worst: ResourcePressureState = "normal";
  for (const signal of t.signals) {
    if (signalMeter(signal.name) !== meter) continue;
    if (meter === "disk" && mount !== undefined && signalMount(signal.name) !== mount) continue;
    if (RANK[signal.state] > RANK[worst]) worst = signal.state;
  }
  if (meter === "memory" && t.memoryPressure) {
    const level = t.memoryPressure.level === "critical" ? "critical" : t.memoryPressure.level === "warn" ? "elevated" : "normal";
    if (RANK[level] > RANK[worst]) worst = level;
  }
  return worst;
}

/** A meter's fill: the daemon's state for a metric it judges, else the default thresholds on its share. */
export function resourceMeterTone(t: ResourceTelemetry, meter: ResourceMeter | null, share: number, mount?: string): "fill" | "warn" | "error" {
  if (meter === null) return meterTone(share);
  const state = meterState(t, meter, mount);
  return state === "critical" ? "error" : state === "elevated" ? "warn" : "fill";
}

/** The status word a tile's line carries beside its number ("High · of 32 GB"); none while it is fine. */
export function meterWord(tone: "fill" | "warn" | "error"): string | null {
  return tone === "error" ? "Critical" : tone === "warn" ? "High" : null;
}

/**
 * A "Using this machine" row's leading icon: an agent's logo for a Worker or Brain run by a known agent, Mewla's
 * own for Brain, a process's mark when the client has it (node, go, docker…), else `cube` on the neutral tile.
 */
export type ConsumerLead = { kind: "agent"; agent: AgentKind } | { kind: "mewla" } | RowLeading<"cube">;

export function consumerLead(c: ResourceConsumer): ConsumerLead {
  const agent = agentKindFromProvider(c.executor);
  if ((c.owner === "worker" || c.owner === "orphaned_worker") && agent !== "terminal") return { kind: "agent", agent };
  if (c.owner === "brain") return agent !== "terminal" ? { kind: "agent", agent } : { kind: "mewla" };
  if (c.owner === "docker") return { kind: "mark", mark: "docker" };
  const program = programName(c.commands[0] ?? c.processes.find((p) => p.command)?.command);
  if (program?.toLowerCase() === "mewla") return { kind: "mewla" };
  return rowLeading(program, { glyph: "cube" });
}

/** The program of a command line: its first word without its path ("/usr/bin/node server.js" → "node"). */
export function programName(command: string | undefined): string | undefined {
  const first = command?.trim().split(/\s+/)[0];
  if (!first) return undefined;
  return first.split(/[\\/]/).pop() || undefined;
}
