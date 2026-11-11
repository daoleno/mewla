import {
  CONSUMER_OWNER_LABEL,
  consumerKey,
  consumerMeta,
  formatBytes,
  formatPercent,
  MEMORY_PRESSURE_LABEL,
  type MemoryPressureLevel,
  type ResourceConsumer,
  type ResourceProcess,
  type ResourceTelemetry,
} from "./resourceTelemetry";

/** How Resources lays out and words what the telemetry says: tiles, PSI, consumer rows and what can be copied from them. */

/** Tiles four across from 1040 points, one column below 340, else two across. */
export function tileBasis(width: number): `${number}%` {
  return width >= 1040 ? "23%" : width < 340 ? "100%" : "48%";
}

/** The consumers table shows the process count as a column from 760 points. */
export function consumersWide(width: number): boolean {
  return width >= 760;
}

/** Memory in use: total less available when both are known, else what the computer called used. */
export function memoryUsedBytes(telemetry: ResourceTelemetry): number | undefined {
  const { totalBytes, availableBytes, usedBytes } = telemetry.memory;
  return totalBytes !== undefined && availableBytes !== undefined ? Math.max(0, totalBytes - availableBytes) : usedBytes;
}

export type PsiKey = "cpu" | "memory" | "io";
export const PSI_KEYS: readonly PsiKey[] = ["cpu", "memory", "io"];
export const PSI_LABEL: Record<PsiKey, string> = { cpu: "CPU", memory: "Mem", io: "I/O" };

/** The resource under the most pressure (its "some" 10 s average), the first on a tie; none without PSI. */
export function psiPeak(telemetry: ResourceTelemetry): { key: PsiKey; value: number } | undefined {
  let best: { key: PsiKey; value: number } | undefined;
  for (const key of PSI_KEYS) {
    const value = telemetry.psi[key]?.some?.avg10;
    if (value !== undefined && (best === undefined || value > best.value)) best = { key, value };
  }
  return best;
}

/** Where the kernel reports a memory pressure level instead of PSI (macOS). */
export function usesMemoryPressure(telemetry: ResourceTelemetry): boolean {
  return psiPeak(telemetry) === undefined && telemetry.memoryPressure !== undefined;
}

/** How many of the meter's three segments a level lights. */
export const MEMORY_PRESSURE_STEPS: Record<MemoryPressureLevel, number> = { normal: 1, warn: 2, critical: 3 };

export function memoryPressureMeterLabel(level: MemoryPressureLevel): string {
  return `Memory pressure ${MEMORY_PRESSURE_LABEL[level]}`;
}

/** "42% free · Load 2.1 · 8 cores", "—" when the computer said none of it. */
export function memoryPressureMicro(telemetry: ResourceTelemetry): string {
  const free = telemetry.memoryPressure?.freePercent;
  const load1 = telemetry.cpu.load1;
  const cores = telemetry.cpu.perCorePercent.length;
  return [
    free === undefined ? null : `${Math.round(free)}% free`,
    load1 !== undefined && cores > 0 ? `Load ${load1.toFixed(1)} · ${cores} cores` : null,
  ].filter(Boolean).join(" · ") || "—";
}

export function psiChartLabel(key: PsiKey | undefined): string {
  return `PSI ${key ? PSI_LABEL[key] : "unavailable"} some history, scale 0–100%`;
}

export function psiResourceChartLabel(key: PsiKey): string {
  return `${PSI_LABEL[key]} PSI some history, scale 0–100%`;
}

/** A Worker's status is news on its row; an orphan's is residue; an unknown status says nothing. */
export function consumerStatus(consumer: ResourceConsumer): string | null {
  if (consumer.owner === "orphaned_worker") return `Residual · ${consumer.status ?? "unknown"}`;
  return consumer.status && consumer.status !== "unknown" ? consumer.status : null;
}

/** The row's second line: its status, then executor, folder and age. */
export function consumerRowMeta(consumer: ResourceConsumer): string {
  return [consumerStatus(consumer), consumerMeta(consumer)].filter(Boolean).join(" · ");
}

/** "Expand mewla, Worker, running, CPU 12%, RSS 1.5 GB, 3 processes" */
export function consumerAccessibilityLabel(consumer: ResourceConsumer, title: string, expanded: boolean): string {
  return `${expanded ? "Collapse" : "Expand"} ${title}, ${CONSUMER_OWNER_LABEL[consumer.owner]}, ${consumer.status ?? "unknown"}, CPU ${formatPercent(consumer.cpuPercent)}, RSS ${formatBytes(consumer.rssBytes)}, ${consumer.processCount} processes`;
}

/** Each row's own key: its identity, and which of the rows with that identity it is (malformed snapshots repeat one). */
export function consumerRowKeys(consumers: readonly ResourceConsumer[]): string[] {
  const seen = new Map<string, number>();
  return consumers.map((consumer) => {
    const key = consumerKey(consumer);
    const occurrence = seen.get(key) ?? 0;
    seen.set(key, occurrence + 1);
    return `${key}:${occurrence}`;
  });
}

/** The consumer's commands and its processes' commands, each once. */
export function consumerCommands(consumer: ResourceConsumer): string[] {
  return [...new Set([...consumer.commands, ...consumer.processes.flatMap((p) => (p.command ? [p.command] : []))])];
}

/** The processes, largest memory first; one with no reading last. */
export function processesByMemory(consumer: ResourceConsumer): ResourceProcess[] {
  return [...consumer.processes].sort((a, b) => (b.rssBytes ?? -1) - (a.rssBytes ?? -1));
}

export interface CopyChoice {
  label: string;
  value: string;
}

/** What a long press on a consumer's details offers to copy. */
export function consumerCopyChoices(consumer: ResourceConsumer, title: string): CopyChoice[] {
  const commands = consumerCommands(consumer);
  return [
    { label: "Copy title", value: title },
    ...(consumer.id ? [{ label: "Copy ID", value: consumer.id }] : []),
    ...(consumer.workerId ? [{ label: "Copy Worker ID", value: consumer.workerId }] : []),
    ...(consumer.workId ? [{ label: "Copy Work ID", value: consumer.workId }] : []),
    ...(commands.length ? [{ label: "Copy commands", value: commands.join(", ") }] : []),
  ];
}

/** What a long press on one process offers to copy. */
export function processCopyChoices(process: ResourceProcess): CopyChoice[] {
  return [
    ...(process.pid ? [{ label: "Copy PID", value: String(process.pid) }] : []),
    ...(process.command ? [{ label: "Copy command", value: process.command }] : []),
    ...(process.start ? [{ label: "Copy start token", value: process.start }] : []),
  ];
}
