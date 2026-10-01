// Machine resource telemetry (docs/resource-telemetry.md, version 2).
// The daemon omits unsupported measurements; the normalized view keeps that
// distinction as `undefined` instead of inventing zeros.

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

export interface ResourceConsumer {
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
  disks: ResourceDisk[];
  consumers: ResourceConsumer[];
  history: ResourceHistorySample[];
  signals: ResourceSignal[];
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

function normalizeConsumer(value: unknown): ResourceConsumer | null {
  const source = record(value);
  if (!source) return null;
  const owner = CONSUMER_OWNERS.includes(source.owner as ResourceConsumerOwner)
    ? (source.owner as ResourceConsumerOwner)
    : "user";
  const kinds = Array.isArray(source.kinds)
    ? source.kinds.filter((kind): kind is string => typeof kind === "string" && kind.trim() !== "")
    : [];
  return {
    owner,
    workerId: str(source.worker_id),
    workId: str(source.work_id),
    title: str(source.title),
    status: str(source.status),
    executor: str(source.executor),
    cwd: str(source.cwd),
    ageSeconds: nonNegative(source.age_seconds),
    rssBytes: nonNegative(source.rss_bytes) ?? 0,
    cpuPercent: nonNegative(source.cpu_percent),
    processCount: nonNegative(source.process_count) ?? 0,
    kinds,
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
    disks: compact((Array.isArray(source.disks) ? source.disks : []).map(normalizeDisk)),
    consumers: compact((Array.isArray(source.consumers) ? source.consumers : []).map(normalizeConsumer))
      .sort((a, b) => b.rssBytes - a.rssBytes),
    history,
    signals: compact((Array.isArray(source.signals) ? source.signals : []).map(normalizeSignal)),
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

export const PRESSURE_COPY: Record<ResourcePressureState, { title: string; detail: string }> = {
  normal: {
    title: "Running comfortably",
    detail: "Plenty of headroom for new work.",
  },
  elevated: {
    title: "Under pressure",
    detail: "The machine is working hard. New heavy work may slow things down.",
  },
  critical: {
    title: "Critically loaded",
    detail: "The machine is close to stalling. Consider closing heavy work.",
  },
};

export type PsiTone = "calm" | "some" | "heavy";

export function psiTone(avg10: number): PsiTone {
  if (avg10 >= 20) return "heavy";
  if (avg10 >= 5) return "some";
  return "calm";
}

export interface PsiLine {
  key: "cpu" | "memory" | "io";
  label: string;
  /** Share of the last 10 s some task was stalled. */
  avg10: number;
  avg60: number;
  avg300: number;
  /** Share of the last 10 s every task was stalled, when reported. */
  full10?: number;
  tone: PsiTone;
  sentence: string;
}

const PSI_SUBJECT: Record<PsiLine["key"], { label: string; waiting: string }> = {
  cpu: { label: "CPU", waiting: "waiting for a free core" },
  memory: { label: "Memory", waiting: "waiting for memory to be freed" },
  io: { label: "Disk", waiting: "waiting on disk reads and writes" },
};

function psiSentence(key: PsiLine["key"], avg10: number, full10: number | undefined): string {
  const subject = PSI_SUBJECT[key];
  if (avg10 < 0.5) return `Nothing is ${subject.waiting}.`;
  const share = formatPercent(avg10);
  const base = `Work spent ${share} of the last 10 seconds ${subject.waiting}.`;
  if (full10 !== undefined && full10 >= 1) {
    return `${base} Everything stalled ${formatPercent(full10)} of the time.`;
  }
  return base;
}

export function psiLines(telemetry: ResourceTelemetry): PsiLine[] {
  const out: PsiLine[] = [];
  for (const key of ["cpu", "memory", "io"] as const) {
    const resource = telemetry.psi[key];
    const some = resource?.some;
    if (!some) continue;
    const full10 = resource.full?.avg10;
    out.push({
      key,
      label: PSI_SUBJECT[key].label,
      avg10: some.avg10,
      avg60: some.avg60,
      avg300: some.avg300,
      full10,
      tone: psiTone(Math.max(some.avg10, full10 ?? 0)),
      sentence: psiSentence(key, some.avg10, full10),
    });
  }
  return out;
}

export type PsiTrend = "rising" | "falling" | "steady";

/** Compare the 10 s window to the 5 min window. */
export function psiTrend(line: Pick<PsiLine, "avg10" | "avg300">): PsiTrend {
  const delta = line.avg10 - line.avg300;
  if (Math.abs(delta) < 2) return "steady";
  return delta > 0 ? "rising" : "falling";
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
    title: "Left behind",
    hint: "Still running after their Worker finished or closed.",
  },
  { key: "brain", owner: "brain", title: "Brain" },
  { key: "docker", owner: "docker", title: "Containers" },
  { key: "user", owner: "user", title: "Everything else" },
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

const OWNER_FALLBACK_TITLE: Record<ResourceConsumerOwner, string> = {
  worker: "Worker",
  orphaned_worker: "Finished Worker",
  brain: "Brain",
  docker: "Container",
  user: "Other processes",
};

export function consumerTitle(consumer: ResourceConsumer): string {
  return consumer.title ?? (consumer.executor ? `${capitalize(consumer.executor)} Worker` : OWNER_FALLBACK_TITLE[consumer.owner]);
}

export function consumerMeta(consumer: ResourceConsumer): string {
  const parts: string[] = [];
  if (consumer.title && consumer.executor) parts.push(capitalize(consumer.executor));
  if (consumer.status && consumer.status !== "unknown") parts.push(capitalize(consumer.status));
  const age = formatAge(consumer.ageSeconds);
  if (age) parts.push(age === "just now" ? "started just now" : `${age} old`);
  return parts.join(" · ");
}

function capitalize(value: string): string {
  return value ? value[0].toUpperCase() + value.slice(1) : value;
}

export interface SeriesPoint {
  at: number;
  value: number;
}

/** Memory used share over the daemon history ring, 0…1. */
export function memoryHistorySeries(telemetry: ResourceTelemetry): SeriesPoint[] {
  const total = telemetry.memory.totalBytes;
  if (!total) return [];
  const points: SeriesPoint[] = [];
  for (const sample of telemetry.history) {
    const used = sample.memoryAvailableBytes !== undefined
      ? total - sample.memoryAvailableBytes
      : sample.memoryUsedBytes;
    if (used !== undefined) points.push({ at: sample.sampledAt, value: clampRatio(used / total) });
  }
  const current = memoryUsedRatio(telemetry);
  if (current !== undefined && (points.length === 0 || points[points.length - 1].at < telemetry.sampledAt)) {
    points.push({ at: telemetry.sampledAt, value: current });
  }
  return points;
}

/** CPU utilization share over the daemon history ring, 0…1. */
export function cpuHistorySeries(telemetry: ResourceTelemetry): SeriesPoint[] {
  return withCurrent(
    historySeries(telemetry, (sample) => sample.cpuPercent, 100),
    telemetry,
    telemetry.cpu.utilizationPercent === undefined ? undefined : telemetry.cpu.utilizationPercent / 100,
  );
}

/** PSI "some" avg10 history for one resource, 0…1 of time stalled. */
export function psiHistorySeries(telemetry: ResourceTelemetry, key: PsiLine["key"]): SeriesPoint[] {
  const pick = key === "cpu"
    ? (sample: ResourceHistorySample) => sample.psiCpu
    : key === "memory"
      ? (sample: ResourceHistorySample) => sample.psiMemory
      : (sample: ResourceHistorySample) => sample.psiIo;
  const now = telemetry.psi[key]?.some?.avg10;
  return withCurrent(historySeries(telemetry, pick, 100), telemetry, now === undefined ? undefined : now / 100);
}

export interface ThroughputSeries {
  read: SeriesPoint[];
  write: SeriesPoint[];
  /** Bytes/s that maps to the top of the chart. */
  peak: number;
}

/** Disk read/write history scaled to a shared peak, so both lines compare. */
export function diskThroughputSeries(telemetry: ResourceTelemetry): ThroughputSeries {
  const samples = telemetry.history.filter(
    (sample) => sample.diskReadBytesPerSecond !== undefined || sample.diskWriteBytesPerSecond !== undefined,
  );
  const peak = Math.max(
    1024 * 1024,
    ...samples.map((sample) => Math.max(sample.diskReadBytesPerSecond ?? 0, sample.diskWriteBytesPerSecond ?? 0)),
  );
  return {
    peak,
    read: historySeries(telemetry, (sample) => sample.diskReadBytesPerSecond, peak),
    write: historySeries(telemetry, (sample) => sample.diskWriteBytesPerSecond, peak),
  };
}

export function latestDiskThroughput(telemetry: ResourceTelemetry): { read?: number; write?: number } {
  for (let index = telemetry.history.length - 1; index >= 0; index -= 1) {
    const sample = telemetry.history[index];
    if (sample.diskReadBytesPerSecond !== undefined || sample.diskWriteBytesPerSecond !== undefined) {
      return { read: sample.diskReadBytesPerSecond, write: sample.diskWriteBytesPerSecond };
    }
  }
  const read = telemetry.disks.reduce<number | undefined>(
    (sum, disk) => (disk.readBytesPerSecond === undefined ? sum : (sum ?? 0) + disk.readBytesPerSecond), undefined);
  const write = telemetry.disks.reduce<number | undefined>(
    (sum, disk) => (disk.writeBytesPerSecond === undefined ? sum : (sum ?? 0) + disk.writeBytesPerSecond), undefined);
  return { read, write };
}

function historySeries(
  telemetry: ResourceTelemetry,
  pick: (sample: ResourceHistorySample) => number | undefined,
  scale: number,
): SeriesPoint[] {
  const points: SeriesPoint[] = [];
  for (const sample of telemetry.history) {
    const value = pick(sample);
    if (value !== undefined) points.push({ at: sample.sampledAt, value: clampRatio(value / scale) });
  }
  return points;
}

function withCurrent(points: SeriesPoint[], telemetry: ResourceTelemetry, current: number | undefined): SeriesPoint[] {
  if (current !== undefined && (points.length === 0 || points[points.length - 1].at < telemetry.sampledAt)) {
    points.push({ at: telemetry.sampledAt, value: clampRatio(current) });
  }
  return points;
}

/** Chart window: the history span, never narrower than five minutes. */
export function chartWindow(telemetry: ResourceTelemetry): { start: number; end: number } {
  const end = telemetry.sampledAt;
  const first = telemetry.history[0]?.sampledAt ?? end;
  return { start: Math.min(first, end - 5 * 60_000), end };
}

/** Consumer CPU is per-core (100% = one core); people read cores. */
export function formatCores(cpuPercent: number | undefined): string {
  if (cpuPercent === undefined) return "—";
  const cores = cpuPercent / 100;
  if (cores < 0.05) return "idle";
  return `${cores < 10 ? cores.toFixed(1).replace(/\.0$/, "") : Math.round(cores)} ${cores >= 0.95 && cores < 1.05 ? "core" : "cores"}`;
}

export interface StateSpan {
  start: number;
  end: number;
  state: ResourcePressureState;
}

/** Collapse history into contiguous pressure-state spans for the timeline. */
export function pressureSpans(telemetry: ResourceTelemetry): StateSpan[] {
  const samples = [...telemetry.history.map((sample) => ({ at: sample.sampledAt, state: sample.state }))];
  if (samples.length === 0 || samples[samples.length - 1].at < telemetry.sampledAt) {
    samples.push({ at: telemetry.sampledAt, state: telemetry.state });
  }
  const spans: StateSpan[] = [];
  for (let index = 0; index < samples.length; index += 1) {
    const sample = samples[index];
    const end = samples[index + 1]?.at ?? sample.at;
    const last = spans[spans.length - 1];
    if (last && last.state === sample.state) last.end = end;
    else spans.push({ start: sample.at, end, state: sample.state });
  }
  return spans;
}

export function historyWindowLabel(telemetry: ResourceTelemetry): string | undefined {
  const first = telemetry.history[0]?.sampledAt;
  if (first === undefined) return undefined;
  const minutes = Math.round((telemetry.sampledAt - first) / 60_000);
  return minutes >= 1 ? `Last ${minutes} min` : undefined;
}

export function headlineSummary(telemetry: ResourceTelemetry): string {
  const parts: string[] = [];
  if (telemetry.cpu.utilizationPercent !== undefined) {
    parts.push(`CPU ${formatPercent(telemetry.cpu.utilizationPercent)}`);
  }
  const memory = memoryUsedRatio(telemetry);
  if (memory !== undefined) parts.push(`Memory ${formatPercent(memory * 100)}`);
  const { swapTotalBytes, swapUsedBytes } = telemetry.memory;
  if (swapTotalBytes && swapUsedBytes !== undefined && swapUsedBytes / swapTotalBytes >= 0.1) {
    parts.push(`Swap ${formatPercent((swapUsedBytes / swapTotalBytes) * 100)}`);
  }
  return parts.join(" · ");
}

/** Load average relative to cores: 1.0 means every core is busy. */
export function loadPerCore(load: number | undefined, cores: number): number | undefined {
  if (load === undefined || cores <= 0) return undefined;
  return load / cores;
}

export function sampledAgoLabel(sampledAt: number, now: number): string {
  const seconds = Math.max(0, Math.round((now - sampledAt) / 1000));
  if (seconds < 5) return "Live";
  if (seconds < 60) return `Updated ${seconds}s ago`;
  return `Updated ${Math.round(seconds / 60)} min ago`;
}
