// Machine resource telemetry (docs/resource-telemetry.md, version 2).
// The daemon omits unsupported measurements; the normalized view keeps that
// distinction as `undefined` instead of inventing zeros.

import { normalizeHolds, type ResourceHolds } from "./holds";

export type ResourcePressureState = "normal" | "elevated" | "critical";

export interface PsiAverages {
  avg10: number;
  avg60: number;
  avg300: number;
}

export interface PsiResource {
  some?: PsiAverages;
  full?: PsiAverages;
}

/**
 * The kernel's own memory pressure verdict where there is no PSI (macOS).
 * It is an ordinal level, never a stall percentage.
 */
export type MemoryPressureLevel = "normal" | "warn" | "critical";

export interface MemoryPressure {
  level: MemoryPressureLevel;
  /** The kernel's free-memory percentage, 0–100, when it reports one. */
  freePercent?: number;
}

export interface ResourceDisk {
  mount: string;
  totalBytes: number;
  usedBytes: number;
  freeBytes: number;
  readBytesPerSecond?: number;
  writeBytesPerSecond?: number;
}

export type ResourceConsumerOwner =
  | "worker"
  | "orphaned_worker"
  | "brain"
  | "docker"
  | "user";

export interface ResourceProcess {
  pid?: number;
  /** Opaque kernel generation token, not a timestamp. */
  start?: string;
  command?: string;
  rssBytes?: number;
}

export interface ResourceConsumer {
  id?: string;
  commands: string[];
  processes: ResourceProcess[];
  owner: ResourceConsumerOwner;
  workerId?: string;
  workId?: string;
  title?: string;
  status?: string;
  executor?: string;
  cwd?: string;
  ageSeconds?: number;
  rssBytes: number;
  cpuPercent?: number;
  processCount: number;
  kinds: string[];
}

export interface ResourceHistorySample {
  sampledAt: number;
  state: ResourcePressureState;
  memoryAvailableBytes?: number;
  memoryUsedBytes?: number;
  swapUsedBytes?: number;
  load15?: number;
  cpuPercent?: number;
  /** PSI "some" avg10 per resource at that sample. */
  psiCpu?: number;
  psiMemory?: number;
  psiIo?: number;
  memoryPressure?: MemoryPressureLevel;
  diskReadBytesPerSecond?: number;
  diskWriteBytesPerSecond?: number;
}

export interface ResourceSignal {
  name: string;
  value: number;
  threshold: number;
  state: ResourcePressureState;
}

export interface ResourceTelemetry {
  version: number;
  sampledAt: number;
  state: ResourcePressureState;
  cpu: {
    load1?: number;
    load5?: number;
    load15?: number;
    utilizationPercent?: number;
    perCorePercent: number[];
  };
  memory: {
    totalBytes?: number;
    availableBytes?: number;
    usedBytes?: number;
    cacheBytes?: number;
    sharedBytes?: number;
    swapTotalBytes?: number;
    swapUsedBytes?: number;
  };
  psi: {
    cpu?: PsiResource;
    memory?: PsiResource;
    io?: PsiResource;
  };
  /** Present only where the platform reports it instead of PSI (macOS). */
  memoryPressure?: MemoryPressure;
  /** Measurements this machine cannot report, by the daemon's names. */
  unavailable: string[];
  disks: ResourceDisk[];
  consumers: ResourceConsumer[];
  history: ResourceHistorySample[];
  signals: ResourceSignal[];
  /** What the collector holds and did; absent from daemons before it. */
  holds?: ResourceHolds;
}

type Raw = Record<string, unknown>;

function record(value: unknown): Raw | null {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Raw)
    : null;
}

function num(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function nonNegative(value: unknown): number | undefined {
  const n = num(value);
  return n === undefined ? undefined : Math.max(0, n);
}

function str(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

function time(value: unknown): number | undefined {
  if (typeof value !== "string") return undefined;
  const at = Date.parse(value);
  return Number.isFinite(at) ? at : undefined;
}

export function normalizePressureState(value: unknown): ResourcePressureState {
  return value === "elevated" || value === "critical" ? value : "normal";
}

function normalizeAverages(value: unknown): PsiAverages | undefined {
  const source = record(value);
  if (!source) return undefined;
  const avg10 = nonNegative(source.avg10);
  const avg60 = nonNegative(source.avg60);
  const avg300 = nonNegative(source.avg300);
  if (avg10 === undefined && avg60 === undefined && avg300 === undefined) {
    return undefined;
  }
  return { avg10: avg10 ?? 0, avg60: avg60 ?? 0, avg300: avg300 ?? 0 };
}

function normalizePsiResource(value: unknown): PsiResource | undefined {
  const source = record(value);
  if (!source) return undefined;
  const some = normalizeAverages(source.some);
  const full = normalizeAverages(source.full);
  return some || full ? { some, full } : undefined;
}

const CONSUMER_OWNERS: readonly ResourceConsumerOwner[] = [
  "worker",
  "orphaned_worker",
  "brain",
  "docker",
  "user",
];

function stringList(value: unknown): string[] {
  return Array.isArray(value) ? [...new Set(value.map(str).filter((v): v is string => v !== undefined))] : [];
}

function normalizeMemoryPressureLevel(value: unknown): MemoryPressureLevel | undefined {
  return value === "normal" || value === "warn" || value === "critical" ? value : undefined;
}

function normalizeMemoryPressure(value: unknown): MemoryPressure | undefined {
  const source = record(value);
  const level = normalizeMemoryPressureLevel(source?.level);
  if (!source || !level) return undefined;
  const free = num(source.free_percent);
  return { level, freePercent: free === undefined ? undefined : Math.min(100, Math.max(0, free)) };
}

function normalizeProcess(value: unknown): ResourceProcess | null {
  const source = record(value);
  if (!source) return null;
  const pid = num(source.pid);
  return {
    pid: pid !== undefined && Number.isSafeInteger(pid) && pid > 0 ? pid : undefined,
    start: str(source.start),
    command: str(source.command),
    rssBytes: nonNegative(source.rss_bytes),
  };
}

function normalizeConsumer(value: unknown): ResourceConsumer | null {
  const source = record(value);
  if (!source) return null;
  const owner = CONSUMER_OWNERS.includes(source.owner as ResourceConsumerOwner)
    ? (source.owner as ResourceConsumerOwner)
    : "user";
  const processes = compact((Array.isArray(source.processes) ? source.processes : []).map(normalizeProcess));
  return {
    id: str(source.id),
    commands: stringList(source.commands),
    processes,
    owner,
    workerId: str(source.worker_id),
    workId: str(source.work_id),
    title: str(source.title) ?? str(source.work_title) ?? str(source.worker_title),
    status: str(source.status),
    executor: str(source.executor),
    cwd: str(source.cwd),
    ageSeconds: nonNegative(source.age_seconds),
    rssBytes: nonNegative(source.rss_bytes) ?? 0,
    cpuPercent: nonNegative(source.cpu_percent),
    processCount: Math.max(processes.length, Math.floor(nonNegative(source.process_count) ?? 0)),
    kinds: stringList(source.kinds),
  };
}

function normalizeDisk(value: unknown): ResourceDisk | null {
  const source = record(value);
  const mount = str(source?.mount);
  if (!source || !mount) return null;
  const totalBytes = nonNegative(source.total_bytes) ?? 0;
  const freeBytes = nonNegative(source.free_bytes) ?? 0;
  return {
    mount,
    totalBytes,
    freeBytes,
    usedBytes: nonNegative(source.used_bytes) ?? Math.max(0, totalBytes - freeBytes),
    readBytesPerSecond: nonNegative(source.read_bytes_per_second),
    writeBytesPerSecond: nonNegative(source.write_bytes_per_second),
  };
}

function normalizeHistorySample(value: unknown): ResourceHistorySample | null {
  const source = record(value);
  const sampledAt = time(source?.sampled_at);
  if (!source || sampledAt === undefined) return null;
  return {
    sampledAt,
    state: normalizePressureState(source.state),
    memoryAvailableBytes: nonNegative(source.memory_available_bytes),
    memoryUsedBytes: nonNegative(source.memory_used_bytes),
    swapUsedBytes: nonNegative(source.swap_used_bytes),
    load15: nonNegative(source.load15),
    cpuPercent: nonNegative(source.cpu_percent),
    psiCpu: nonNegative(source.psi_cpu_some_avg10),
    psiMemory: nonNegative(source.psi_memory_some_avg10),
    psiIo: nonNegative(source.psi_io_some_avg10),
    memoryPressure: normalizeMemoryPressureLevel(source.memory_pressure_level),
    diskReadBytesPerSecond: nonNegative(source.disk_read_bytes_per_second),
    diskWriteBytesPerSecond: nonNegative(source.disk_write_bytes_per_second),
  };
}

function normalizeSignal(value: unknown): ResourceSignal | null {
  const source = record(value);
  const name = str(source?.name);
  if (!source || !name) return null;
  return {
    name,
    value: num(source.value) ?? 0,
    threshold: num(source.threshold) ?? 0,
    state: normalizePressureState(source.state),
  };
}

function compact<T>(values: (T | null)[]): T[] {
  return values.filter((value): value is T => value !== null);
}

export function normalizeResourceTelemetry(payload: unknown): ResourceTelemetry | null {
  const source = record(payload);
  if (!source) return null;
  const sampledAt = time(source.sampled_at);
  if (sampledAt === undefined) return null;
  const cpu = record(source.cpu) ?? {};
  const memory = record(source.memory) ?? {};
  const psi = record(source.psi) ?? {};
  const history = compact((Array.isArray(source.history) ? source.history : []).map(normalizeHistorySample))
    .sort((a, b) => a.sampledAt - b.sampledAt);
  return {
    version: num(source.version) ?? 1,
    sampledAt,
    state: normalizePressureState(source.state),
    cpu: {
      load1: nonNegative(cpu.load1),
      load5: nonNegative(cpu.load5),
      load15: nonNegative(cpu.load15),
      utilizationPercent: nonNegative(cpu.utilization_percent),
      perCorePercent: Array.isArray(cpu.per_core_percent)
        ? cpu.per_core_percent.map((value) => nonNegative(value) ?? 0)
        : [],
    },
    memory: {
      totalBytes: nonNegative(memory.total_bytes),
      availableBytes: nonNegative(memory.available_bytes),
      usedBytes: nonNegative(memory.used_bytes),
      cacheBytes: nonNegative(memory.cache_bytes),
      sharedBytes: nonNegative(memory.shared_bytes),
      swapTotalBytes: nonNegative(memory.swap_total_bytes),
      swapUsedBytes: nonNegative(memory.swap_used_bytes),
    },
    psi: {
      cpu: normalizePsiResource(psi.cpu),
      memory: normalizePsiResource(psi.memory),
      io: normalizePsiResource(psi.io),
    },
    memoryPressure: normalizeMemoryPressure(source.memory_pressure),
    unavailable: stringList(source.unavailable),
    disks: compact((Array.isArray(source.disks) ? source.disks : []).map(normalizeDisk)),
    consumers: compact((Array.isArray(source.consumers) ? source.consumers : []).map(normalizeConsumer))
      .sort((a, b) => b.rssBytes - a.rssBytes),
    history,
    signals: compact((Array.isArray(source.signals) ? source.signals : []).map(normalizeSignal)),
    holds: normalizeHolds(source.holds),
  };
}

// ── Presentation ───────────────────────────────────────────

const BYTE_UNITS = ["B", "KB", "MB", "GB", "TB"] as const;

export function formatBytes(bytes: number | undefined): string {
  if (bytes === undefined) return "—";
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < BYTE_UNITS.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const digits = unit === 0 || value >= 100 ? 0 : 1;
  return `${value.toFixed(digits).replace(/\.0$/, "")} ${BYTE_UNITS[unit]}`;
}

export function formatRate(bytesPerSecond: number | undefined): string {
  if (bytesPerSecond === undefined) return "—";
  if (bytesPerSecond < 1024) return "idle";
  return `${formatBytes(bytesPerSecond)}/s`;
}

export function formatPercent(value: number | undefined): string {
  if (value === undefined) return "—";
  if (value > 0 && value < 1) return "<1%";
  return `${Math.round(value)}%`;
}

/** Keep small PSI percentages readable against sub-percent stall thresholds. */
export function formatPressurePercent(value: number | undefined): string {
  return value === undefined ? "—" : `${Number(value.toFixed(2))}%`;
}

export function formatAge(seconds: number | undefined): string | undefined {
  if (seconds === undefined) return undefined;
  if (seconds < 60) return "just now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 48) return `${hours}h ${minutes % 60}m`;
  return `${Math.floor(hours / 24)}d`;
}

export function memoryUsedRatio(telemetry: ResourceTelemetry): number | undefined {
  const { totalBytes, availableBytes, usedBytes } = telemetry.memory;
  if (!totalBytes) return undefined;
  if (availableBytes !== undefined) return clampRatio(1 - availableBytes / totalBytes);
  if (usedBytes !== undefined) return clampRatio(usedBytes / totalBytes);
  return undefined;
}

export function clampRatio(value: number): number {
  return Math.min(1, Math.max(0, value));
}

// Process kinds the daemon reports, mapped to the words people use. Heavy
// kinds are the ones that froze the machine before; the UI calls them out.
const KIND_LABELS: Record<string, { label: string; heavy: boolean }> = {
  qemu: { label: "Emulator", heavy: true },
  emulator: { label: "Emulator", heavy: true },
  gradle: { label: "Gradle", heavy: true },
  java: { label: "Java", heavy: false },
  chrome: { label: "Browser", heavy: true },
  chromium: { label: "Browser", heavy: true },
  firefox: { label: "Browser", heavy: true },
  browser: { label: "Browser", heavy: true },
  node: { label: "Node", heavy: false },
  bun: { label: "Bun", heavy: false },
  go: { label: "Go", heavy: false },
  python: { label: "Python", heavy: false },
  docker: { label: "Container", heavy: false },
};

export interface KindChip {
  label: string;
  heavy: boolean;
}

export function kindChips(kinds: readonly string[]): KindChip[] {
  const seen = new Set<string>();
  const chips: KindChip[] = [];
  for (const raw of kinds) {
    const key = raw.trim().toLowerCase();
    const known = KIND_LABELS[key];
    const chip = known ?? { label: raw.trim(), heavy: false };
    if (seen.has(chip.label)) continue;
    seen.add(chip.label);
    chips.push(chip);
  }
  return chips.sort((a, b) => Number(b.heavy) - Number(a.heavy));
}

export interface ConsumerGroup {
  key: "workers" | "orphaned" | "brain" | "docker" | "user";
  title: string;
  hint?: string;
  rssBytes: number;
  cpuPercent?: number;
  consumers: ResourceConsumer[];
}

const GROUPS: readonly {
  key: ConsumerGroup["key"];
  owner: ResourceConsumerOwner;
  title: string;
  hint?: string;
}[] = [
  { key: "workers", owner: "worker", title: "Workers" },
  {
    key: "orphaned",
    owner: "orphaned_worker",
    title: "Orphaned Workers",
  },
  { key: "brain", owner: "brain", title: "Brain" },
  { key: "docker", owner: "docker", title: "Docker" },
  { key: "user", owner: "user", title: "User processes" },
];

export function groupConsumers(consumers: readonly ResourceConsumer[]): ConsumerGroup[] {
  return GROUPS.map((group) => {
    const members = consumers
      .filter((consumer) => consumer.owner === group.owner)
      .sort((a, b) => b.rssBytes - a.rssBytes);
    return {
      key: group.key,
      title: group.title,
      hint: group.hint,
      consumers: members,
      rssBytes: members.reduce((sum, consumer) => sum + consumer.rssBytes, 0),
      cpuPercent: members.every((consumer) => consumer.cpuPercent !== undefined)
        ? members.reduce((sum, consumer) => sum + consumer.cpuPercent!, 0)
        : undefined,
    };
  }).filter((group) => group.consumers.length > 0);
}

export const CONSUMER_OWNER_LABEL: Record<ResourceConsumerOwner, string> = {
  worker: "Worker",
  orphaned_worker: "Orphaned Worker",
  brain: "Brain",
  docker: "Docker",
  user: "User process",
};

export function consumerTitle(consumer: ResourceConsumer): string {
  if (consumer.title) return consumer.title;
  const command = consumer.commands[0] ?? consumer.processes.find((p) => p.command)?.command;
  const id = consumer.id?.replace(/^(docker|user|session):/, "");
  if (consumer.owner === "docker" && id) {
    return `${id.length > 12 ? id.slice(0, 12) : id}${command ? ` · ${command}` : ""}`;
  }
  if (command) return command;
  if (id) return id;
  if (consumer.workId) return `Work ${consumer.workId}`;
  if (consumer.workerId) return `Worker ${consumer.workerId}`;
  if (consumer.executor) return `${capitalize(consumer.executor)} · ${CONSUMER_OWNER_LABEL[consumer.owner]}`;
  const pid = consumer.processes.find((p) => p.pid !== undefined)?.pid;
  return `Unknown process${pid !== undefined ? ` · PID ${pid}` : ""}`;
}

/** Stable identity keeps expansion attached to its owner across sorting and polling. */
export function consumerKey(consumer: ResourceConsumer): string {
  return `${consumer.owner}:${consumer.id ?? consumer.workerId ?? consumer.workId ??
    (consumer.processes.length ? consumer.processes.map((p) => `${p.pid ?? "?"}:${p.start ?? "?"}`).join(",") :
      consumer.title ?? consumer.commands.join(","))}`;
}

export type ConsumerSort = "rss" | "cpu";
export function sortConsumers(consumers: readonly ResourceConsumer[], sort: ConsumerSort): ResourceConsumer[] {
  return [...consumers].sort((a, b) =>
    (sort === "cpu" ? (b.cpuPercent ?? -1) - (a.cpuPercent ?? -1) : b.rssBytes - a.rssBytes) ||
    b.rssBytes - a.rssBytes || consumerKey(a).localeCompare(consumerKey(b)));
}

export function consumerMeta(consumer: ResourceConsumer): string {
  return [consumer.executor, consumer.cwd, formatAge(consumer.ageSeconds)].filter(Boolean).join(" · ");
}

function capitalize(value: string): string {
  return value ? value[0].toUpperCase() + value.slice(1) : value;
}

export const MEMORY_PRESSURE_LABEL: Record<MemoryPressureLevel, string> = {
  normal: "Normal",
  warn: "Warning",
  critical: "Critical",
};

/**
 * Did the machine report any core measurement. A daemon that cannot sample
 * this machine (macOS before this reading existed) sends none, and a pressure
 * state over no data would only say "Normal".
 */
export function hasMachineReading(telemetry: ResourceTelemetry): boolean {
  return telemetry.memory.totalBytes !== undefined
    || telemetry.cpu.utilizationPercent !== undefined
    || telemetry.cpu.load1 !== undefined
    || telemetry.memoryPressure !== undefined
    || Object.values(telemetry.psi).some((resource) => resource !== undefined);
}

export function latestDiskThroughput(telemetry: ResourceTelemetry): { read?: number; write?: number } {
  for (let index = telemetry.history.length - 1; index >= 0; index -= 1) {
    const sample = telemetry.history[index];
    if (sample.sampledAt === telemetry.sampledAt && (sample.diskReadBytesPerSecond !== undefined || sample.diskWriteBytesPerSecond !== undefined)) {
      return { read: sample.diskReadBytesPerSecond, write: sample.diskWriteBytesPerSecond };
    }
  }
  const read = telemetry.disks.reduce<number | undefined>(
    (sum, disk) => (disk.readBytesPerSecond === undefined ? sum : (sum ?? 0) + disk.readBytesPerSecond), undefined);
  const write = telemetry.disks.reduce<number | undefined>(
    (sum, disk) => (disk.writeBytesPerSecond === undefined ? sum : (sum ?? 0) + disk.writeBytesPerSecond), undefined);
  return { read, write };
}
