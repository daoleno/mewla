import type { JSX } from "nsh";
import { Canvas, cleanup, effect, lifecycle, signal, Text, untracked, View } from "nsh";
import { Button } from "nsh/base";
import { ResourcePoller, type ResourceReading } from "../../../core/resources/poll";
import {
  chartWindow,
  CONSUMER_OWNER_LABEL,
  consumerKey,
  consumerMeta,
  consumerTitle,
  cpuHistorySeries,
  diskThroughputSeries,
  formatBytes,
  formatPercent,
  formatPressurePercent,
  formatRate,
  groupConsumers,
  hasMachineReading,
  historyWindowLabel,
  kindChips,
  latestDiskThroughput,
  MEMORY_PRESSURE_LABEL,
  memoryHistorySeries,
  memoryPressureSeries,
  memoryUsedRatio,
  psiHistorySeries,
  sampledAgoLabel,
  sortConsumers,
  type ConsumerSort,
  type ResourceConsumer,
  type ResourceTelemetry,
  type SeriesPoint,
} from "../../../core/resources/resourceTelemetry";
import { color, withAlpha, type ColorName } from "../kit/colors";
import { Icon } from "../kit/Icon";
import { EmptyState, Meter, Notice, Page, Section, StatusMark, type Status } from "../kit/kit";
import { Segmented } from "../kit/Segmented";
import { connected, daemon, daemonState } from "../daemon";

const PAGE_SIZE = 30;

const reading = signal<ResourceReading>({ telemetry: null, error: "", reading: false });
const details = signal(false);
const sort = signal<ConsumerSort>("rss");
const owner = signal<string>("all");
const shownCount = signal(PAGE_SIZE);
const open = signal<ReadonlySet<string>>(new Set());
const now = signal(Date.now());

/** Resources: what the computer is doing now, read every five seconds while the page is in use. */
export function Resources() {
  let poller: ResourcePoller | null = null;
  let lastServer = "";
  // A new computer starts from nothing; reading runs while connected and the app is in front.
  effect(() => {
    const current = daemon();
    const live = current !== null && daemonState() === "connected" && lifecycle() === "active";
    poller?.stop();
    poller = null;
    if ((current?.server.id ?? "") !== lastServer) {
      lastServer = current?.server.id ?? "";
      reading.set({ telemetry: null, error: "", reading: false });
      open.set(new Set());
      owner.set("all");
      shownCount.set(PAGE_SIZE);
    }
    if (!live) return;
    poller = new ResourcePoller(current, (state) => {
      reading.set(state);
      now.set(Date.now());
    }, undefined, untracked(reading).telemetry);
    poller.start();
  });
  const tick = setInterval(() => now.set(Date.now()), 1_000);
  cleanup(() => {
    clearInterval(tick);
    poller?.stop();
  });
  const refresh = () => poller?.refresh();
  const telemetry = () => reading().telemetry;
  return (
    <Page title="Resources" testID="resources" refreshing={reading().reading && Boolean(telemetry())} onRefresh={refresh}>
      {telemetry() ? <Ready telemetry={telemetry()!} onRefresh={refresh} /> : <NotReady onRetry={refresh} />}
    </Page>
  );
}

function NotReady(props: { onRetry: () => void }) {
  const busy = () => reading().reading || daemonState() === "connecting";
  return !daemon() ? (
    <EmptyState testID="resources-empty" icon="pulse" title="No current server" detail="Pair a computer in Settings to see its resources." />
  ) : busy() ? (
    <EmptyState testID="resources-loading" busy title={connected() ? "Reading the machine" : "Connecting to server"} />
  ) : !connected() ? (
    <EmptyState testID="resources-offline" icon="pulse" title="Server offline" detail="Resources appear once the server reconnects." />
  ) : (
    <EmptyState testID="resources-failed" icon="pulse" title="Resources unavailable" detail={reading().error || "This server did not report resources."} action="Try again" onAction={props.onRetry} />
  );
}

function headline(t: ResourceTelemetry): { status: Status; title: string } {
  if (!hasMachineReading(t)) return { status: "neutral", title: "No reading" };
  if (t.state === "critical") return { status: "failed", title: "Critical pressure" };
  if (t.state === "elevated") return { status: "warning", title: "Elevated pressure" };
  return { status: "ready", title: "Normal" };
}

function Ready(props: { telemetry: ResourceTelemetry; onRefresh: () => void }) {
  const t = () => props.telemetry;
  const caption = () => {
    const status = !connected() ? "Last sample · offline" : reading().error ? "Last sample" : sampledAgoLabel(t().sampledAt, now());
    return [status, historyWindowLabel(t())].filter(Boolean).join(" · ");
  };
  return (
    <View>
      {connected() && reading().error ? <Notice testID="resources-refresh-failed" title="Refresh failed" detail={reading().error} action="Try again" onAction={props.onRefresh} /> : null}
      <View class="mt-4 flex-row items-center gap-3 px-1">
        <View class="flex-1 gap-1">
          <View class="flex-row items-center gap-2">
            <StatusMark status={headline(t()).status} />
            <Text testID="resources-headline" class="text-2xl font-bold">{headline(t()).title}</Text>
          </View>
          <Text testID="resources-caption" class="text-sm text-muted-foreground">{caption()}</Text>
        </View>
        <Button testID="resources-details" accessibilityLabel={details() ? "Hide machine details" : "Show machine details"} onPress={() => details.set(!details())} class="rounded-full bg-secondary p-2 active:opacity-60">
          <Icon name={details() ? "chevron-up" : "chevron-down"} size={18} />
        </Button>
      </View>
      {t().signals.map((s) => (
        <View class="mt-2 flex-row items-center gap-2 px-1">
          <StatusMark status={s.state === "critical" ? "failed" : "warning"} />
          <Text class="text-sm">{`${s.name} · ${trim2(s.value)} / threshold ${trim2(s.threshold)}`}</Text>
        </View>
      ))}
      <Cpu t={t()} />
      <Memory t={t()} />
      <Disks t={t()} />
      <Pressure t={t()} />
      <Consumers t={t()} />
    </View>
  );
}

function trim2(value: number): string {
  return value.toFixed(2).replace(/\.?0+$/, "");
}

/** An area chart of `series` (0..1) over the page's time window. */
function Chart(props: { series: SeriesPoint[]; window: { start: number; end: number }; tone?: ColorName; second?: SeriesPoint[]; height?: number }) {
  const height = () => props.height ?? 44;
  return (
    <Canvas
      style={{ width: "100%", height: height() }}
      accessibilityElementsHidden
      draw={(cx, size) => {
        const span = Math.max(1, props.window.end - props.window.start);
        const draw = (points: SeriesPoint[], ink: string, fill: boolean) => {
          if (points.length === 0) return;
          const xy = points.map((p) => [((p.at - props.window.start) / span) * size.width, size.height - 1 - p.value * (size.height - 2)] as const);
          cx.beginPath();
          cx.moveTo(xy[0][0], xy[0][1]);
          for (const [x, y] of xy.slice(1)) cx.lineTo(x, y);
          cx.strokeStyle = ink;
          cx.lineWidth = 1.5;
          cx.lineJoin = "round";
          cx.stroke();
          if (!fill) return;
          cx.lineTo(xy[xy.length - 1][0], size.height);
          cx.lineTo(xy[0][0], size.height);
          cx.closePath();
          cx.fillStyle = withAlpha(ink, 0.12);
          cx.fill();
        };
        cx.fillStyle = color("hairline");
        cx.fillRect(0, size.height - 1, size.width, 1);
        draw(props.series, color(props.tone ?? "ink"), true);
        if (props.second) draw(props.second, color("soft"), false);
      }}
    />
  );
}

function Tile(props: { title: string; value: string; testID: string; children?: JSX.Element; caption?: string; detail?: JSX.Element }) {
  return (
    <Section testID={props.testID}>
      <View class="gap-2 px-4 py-3">
        <View class="flex-row items-baseline justify-between">
          <Text class="text-sm font-medium text-muted-foreground">{props.title}</Text>
          <Text class="text-2xl font-bold" style={{ fontVariant: ["tabular-nums"] }}>{props.value}</Text>
        </View>
        {props.children}
        {props.caption ? <Text class="text-xs text-muted-foreground">{props.caption}</Text> : null}
        {details() ? props.detail ?? null : null}
      </View>
    </Section>
  );
}

function Fact(props: { label: string; value: string }) {
  return (
    <View class="flex-row justify-between">
      <Text class="text-sm text-muted-foreground">{props.label}</Text>
      <Text class="font-mono text-sm" style={{ fontVariant: ["tabular-nums"] }}>{props.value}</Text>
    </View>
  );
}

function Cpu(props: { t: ResourceTelemetry }) {
  const cores = () => props.t.cpu.perCorePercent;
  const load = (v?: number) => (v === undefined ? "—" : v.toFixed(2));
  return (
    <Tile
      testID="tile-cpu"
      title="CPU"
      value={formatPercent(props.t.cpu.utilizationPercent)}
      caption={cores().length ? `${cores().length} cores · peak ${formatPercent(Math.max(...cores()))}` : "Core data unavailable"}
      detail={
        <View class="gap-2 pt-1">
          <View class="flex-row flex-wrap gap-1">
            {cores().map((c) => <View class="w-[22px]"><Meter share={c / 100} height={18} /></View>)}
          </View>
          <Fact label="Load · 1 / 5 / 15 min" value={`${load(props.t.cpu.load1)} / ${load(props.t.cpu.load5)} / ${load(props.t.cpu.load15)}`} />
        </View>
      }
    >
      <Chart series={cpuHistorySeries(props.t)} window={chartWindow(props.t)} tone={props.t.state === "critical" ? "failed" : props.t.state === "elevated" ? "warning" : "ink"} />
    </Tile>
  );
}

function Memory(props: { t: ResourceTelemetry }) {
  const used = () => memoryUsedRatio(props.t);
  const m = () => props.t.memory;
  const swapShare = () => (m().swapTotalBytes ? (m().swapUsedBytes ?? 0) / m().swapTotalBytes! : 0);
  return (
    <Tile
      testID="tile-memory"
      title="Memory"
      value={used() === undefined ? "—" : formatPercent(used()! * 100)}
      caption={`${formatBytes(m().usedBytes ?? (m().totalBytes !== undefined && m().availableBytes !== undefined ? m().totalBytes! - m().availableBytes! : undefined))} / ${formatBytes(m().totalBytes)}`}
      detail={
        <View class="gap-1.5 pt-1">
          <Fact label="Available" value={formatBytes(m().availableBytes)} />
          <Fact label={props.t.unavailable.includes("memory.shared") ? "Cache" : "Cache · Shared"} value={props.t.unavailable.includes("memory.shared") ? formatBytes(m().cacheBytes) : `${formatBytes(m().cacheBytes)} · ${formatBytes(m().sharedBytes)}`} />
          <Fact label="Swap used / total" value={`${formatBytes(m().swapUsedBytes)} / ${formatBytes(m().swapTotalBytes)}`} />
          <Meter share={swapShare()} tone={swapShare() >= 0.9 ? "warning" : "ink"} height={4} />
        </View>
      }
    >
      <Chart series={memoryHistorySeries(props.t)} window={chartWindow(props.t)} tone={(used() ?? 0) >= 0.9 ? "warning" : "ink"} />
      <Meter share={used() ?? 0} tone={(used() ?? 0) >= 0.9 ? "warning" : "ink"} height={4} />
    </Tile>
  );
}

function Disks(props: { t: ResourceTelemetry }) {
  const latest = () => latestDiskThroughput(props.t);
  const series = () => diskThroughputSeries(props.t);
  const total = () => (latest().read === undefined && latest().write === undefined ? undefined : (latest().read ?? 0) + (latest().write ?? 0));
  return (
    <Tile
      testID="tile-disk"
      title="Disk I/O"
      value={formatRate(total())}
      caption={`R ${formatRate(latest().read)} · W ${formatRate(latest().write)}`}
      detail={
        <View class="gap-2 pt-1">
          <Fact label="Chart scale" value={formatRate(series().peak)} />
          {props.t.disks.length === 0 ? <Text class="text-sm text-muted-foreground">Mount data unavailable</Text> : null}
          {props.t.disks.map((d) => (
            <View class="gap-1">
              <Fact label={d.mount} value={`${formatBytes(d.freeBytes)} free / ${formatBytes(d.totalBytes)}`} />
              <Meter share={d.totalBytes ? d.usedBytes / d.totalBytes : 0} tone={d.totalBytes && d.freeBytes / d.totalBytes < 0.1 ? "warning" : "ink"} height={4} />
              <Text class="text-xs text-muted-foreground">{`R ${formatRate(d.readBytesPerSecond)} · W ${formatRate(d.writeBytesPerSecond)}`}</Text>
            </View>
          ))}
        </View>
      }
    >
      <Chart series={series().read} second={series().write} window={chartWindow(props.t)} />
    </Tile>
  );
}

function Pressure(props: { t: ResourceTelemetry }) {
  const psi = () => props.t.psi;
  const some = (key: "cpu" | "memory" | "io") => psi()[key]?.some?.avg10;
  const has = () => some("cpu") !== undefined || some("memory") !== undefined || some("io") !== undefined;
  const worst = (): "cpu" | "memory" | "io" => (["cpu", "memory", "io"] as const).reduce((a, b) => ((some(b) ?? -1) > (some(a) ?? -1) ? b : a));
  const label = { cpu: "CPU", memory: "Mem", io: "I/O" } as const;
  const tone = (): ColorName => (props.t.state === "critical" ? "failed" : props.t.state === "elevated" ? "warning" : "ink");
  const triple = (a?: { avg10: number; avg60: number; avg300: number }) => (a ? `${formatPressurePercent(a.avg10)} / ${formatPressurePercent(a.avg60)} / ${formatPressurePercent(a.avg300)}` : "—");
  const mac = () => !has() && props.t.memoryPressure !== undefined;
  if (mac()) {
    const mp = () => props.t.memoryPressure!;
    const parts = () => [mp().freePercent !== undefined ? `${Math.round(mp().freePercent!)}% free` : "", props.t.cpu.load1 !== undefined ? `Load ${props.t.cpu.load1.toFixed(1)}` : "", props.t.cpu.perCorePercent.length ? `${props.t.cpu.perCorePercent.length} cores` : ""].filter(Boolean);
    return (
      <Tile testID="tile-pressure" title="Pressure" value={MEMORY_PRESSURE_LABEL[mp().level]} caption={`Memory pressure · ${parts().join(" · ") || "—"}`}>
        <Chart series={memoryPressureSeries(props.t)} window={chartWindow(props.t)} tone={tone()} />
      </Tile>
    );
  }
  return (
    <Tile
      testID="tile-pressure"
      title="Pressure"
      value={has() ? formatPressurePercent(some(worst())) : "—"}
      caption={has() ? `${label[worst()]} · max some 10s · CPU ${formatPressurePercent(some("cpu"))} · Mem ${formatPressurePercent(some("memory"))} · I/O ${formatPressurePercent(some("io"))}` : "PSI unavailable"}
      detail={
        <View class="gap-1.5 pt-1">
          <Text class="text-xs text-muted-foreground">some / full · 10s / 1m / 5m</Text>
          {(["cpu", "memory", "io"] as const).map((key) => (
            <View class="gap-0.5">
              <Fact label={`${label[key]} some`} value={triple(psi()[key]?.some)} />
              <Fact label={`${label[key]} full`} value={triple(psi()[key]?.full)} />
            </View>
          ))}
        </View>
      }
    >
      {has() ? <Chart series={psiHistorySeries(props.t, worst())} window={chartWindow(props.t)} tone={tone()} /> : null}
    </Tile>
  );
}

const OWNERS = [
  { key: "worker", label: "Workers" },
  { key: "orphaned_worker", label: "Orphaned Workers" },
  { key: "brain", label: "Brain" },
  { key: "docker", label: "Docker" },
  { key: "user", label: "User processes" },
] as const;

function Consumers(props: { t: ResourceTelemetry }) {
  const all = () => sortConsumers(props.t.consumers, sort());
  const filtered = () => (owner() === "all" ? all() : all().filter((c) => c.owner === owner()));
  const counts = () => new Map(groupConsumers(props.t.consumers).map((g) => [g.consumers[0]?.owner, g.consumers.length]));
  const choose = (key: string) => {
    owner.set(key);
    shownCount.set(PAGE_SIZE);
  };
  return (
    <View class="mt-8">
      <View class="flex-row items-center justify-between px-1">
        <Text class="text-lg font-semibold">{`Using this machine ${props.t.consumers.length}`}</Text>
        <View class="w-[150px]">
          <Segmented testID="consumer-sort" options={[{ key: "rss", label: "Memory" }, { key: "cpu", label: "CPU" }]} value={sort()} onChange={(key) => sort.set(key as ConsumerSort)} />
        </View>
      </View>
      <View class="mt-3 flex-row flex-wrap gap-2">
        <Chip testID="owner-all" label={`All ${props.t.consumers.length}`} selected={owner() === "all"} onPress={() => choose("all")} />
        {OWNERS.filter((o) => counts().has(o.key)).map((o) => (
          <Chip testID={`owner-${o.key}`} label={`${o.label} ${counts().get(o.key)}`} selected={owner() === o.key} warning={o.key === "orphaned_worker"} onPress={() => choose(o.key)} />
        ))}
      </View>
      <Section testID="consumers">
        {filtered().length === 0 ? (
          <Text class="px-4 py-4 text-sm text-muted-foreground">{owner() === "all" ? "No consumers reported." : "No consumers reported for this owner."}</Text>
        ) : (
          filtered().slice(0, shownCount()).map((c) => <ConsumerRow consumer={c} />)
        )}
        {filtered().length > shownCount() ? (
          <Button testID="consumers-more" accessibilityLabel={`Show ${Math.min(PAGE_SIZE, filtered().length - shownCount())} more`} onPress={() => shownCount.set(shownCount() + PAGE_SIZE)} class="px-4 py-3 active:bg-accent">
            <Text class="text-sm font-medium text-muted-foreground">{`${filtered().length - shownCount()} more`}</Text>
          </Button>
        ) : null}
      </Section>
      <Text class="mt-1.5 px-4 text-xs text-muted-foreground">CPU 100% is one core. Memory is resident size and may count shared pages twice.</Text>
    </View>
  );
}

function Chip(props: { label: string; selected: boolean; warning?: boolean; testID?: string; onPress: () => void }) {
  return (
    <Button testID={props.testID} selected={props.selected} onPress={props.onPress} class={props.selected ? "rounded-full bg-primary px-3 py-1.5" : "rounded-full border border-border bg-card px-3 py-1.5 active:bg-accent"}>
      <Text class={props.selected ? "text-sm font-medium text-primary-foreground" : props.warning ? "text-sm font-medium text-warning" : "text-sm font-medium"}>{props.label}</Text>
    </Button>
  );
}

function ConsumerRow(props: { consumer: ResourceConsumer }) {
  const c = () => props.consumer;
  const key = () => consumerKey(c());
  const expandedNow = () => open().has(key());
  const toggle = () => {
    const next = new Set(open());
    if (next.has(key())) next.delete(key());
    else next.add(key());
    open.set(next);
  };
  return (
    <View>
      <Button testID={`consumer-${key()}`} onPress={toggle} class="active:bg-accent">
        <View class="flex-row items-center gap-3 px-4 py-3">
          <View class="min-w-0 flex-1">
            <Text class="text-base line-clamp-1">{consumerTitle(c())}</Text>
            <Text class="text-sm text-muted-foreground line-clamp-1">{consumerMeta(c()) || CONSUMER_OWNER_LABEL[c().owner]}</Text>
          </View>
          <View class="items-end">
            <Text class="text-sm" style={{ fontVariant: ["tabular-nums"] }}>{formatBytes(c().rssBytes)}</Text>
            <Text class="text-xs text-muted-foreground" style={{ fontVariant: ["tabular-nums"] }}>{formatPercent(c().cpuPercent)}</Text>
          </View>
          <Icon name={expandedNow() ? "chevron-up" : "chevron-down"} size={14} tone="faint" />
        </View>
      </Button>
      {expandedNow() ? (
        <View class="gap-1 bg-secondary px-4 py-3">
          <Text class="text-sm">{`${CONSUMER_OWNER_LABEL[c().owner]} · ${c().processCount} ${c().processCount === 1 ? "process" : "processes"}`}</Text>
          {c().id ? <Fact label="ID" value={c().id!} /> : null}
          {c().workerId ? <Fact label="Worker" value={c().workerId!} /> : null}
          {c().workId ? <Fact label="Work" value={c().workId!} /> : null}
          {c().commands.length ? <Text class="text-sm text-muted-foreground">{`Commands · ${c().commands.join(", ")}`}</Text> : null}
          {c().kinds.length ? <Text class="text-sm text-muted-foreground">{`Kinds · ${kindChips(c().kinds).map((k) => k.label).join(" · ")}`}</Text> : null}
          {c().processes.length ? (
            <View class="gap-0.5 pt-1">
              <Text class="text-xs text-muted-foreground">{`${c().processes.length} / ${c().processCount} processes reported · largest memory`}</Text>
              {[...c().processes].sort((a, b) => (b.rssBytes ?? 0) - (a.rssBytes ?? 0)).map((p) => (
                <View class="flex-row gap-2">
                  <Text class="w-[64px] font-mono text-xs">{p.pid ? String(p.pid) : "—"}</Text>
                  <Text class="flex-1 font-mono text-xs line-clamp-1">{p.command || (p.start ? `start ${p.start}` : "Unknown process")}</Text>
                  <Text class="font-mono text-xs">{formatBytes(p.rssBytes)}</Text>
                </View>
              ))}
            </View>
          ) : (
            <Text class="text-sm text-muted-foreground">Process details unavailable in this sample.</Text>
          )}
        </View>
      ) : null}
      <View class="absolute bottom-0 left-4 right-0 h-px bg-border" />
    </View>
  );
}
