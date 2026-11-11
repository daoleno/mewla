import type { JSX } from "nsh";
import { Canvas, cleanup, effect, lifecycle, signal, Text, untracked, View } from "nsh";
import { Button } from "nsh/base";
import { ResourcePoller, type ResourceReading } from "../../../core/resources/poll";
import {
  consumerAccessibilityLabel,
  consumerCommands,
  consumerCopyChoices,
  consumerRowKeys,
  consumerRowMeta,
  consumersWide,
  MEMORY_PRESSURE_STEPS,
  memoryPressureMeterLabel,
  memoryPressureMicro,
  memoryUsedBytes,
  processCopyChoices,
  processesByMemory,
  PSI_KEYS,
  PSI_LABEL,
  psiChartLabel,
  psiPeak,
  psiResourceChartLabel,
  tileBasis,
  usesMemoryPressure,
} from "../../../core/resources/presentation";
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
  pressureSpans,
  psiHistorySeries,
  sampledAgoLabel,
  sortConsumers,
  type ConsumerSort,
  type MemoryPressureLevel,
  type ResourceConsumer,
  type ResourceConsumerOwner,
  type ResourceTelemetry,
  type SeriesPoint,
  type StateSpan,
} from "../../../core/resources/resourceTelemetry";
import { color, withAlpha, type ColorName } from "../kit/colors";
import { Icon, type IconName } from "../kit/Icon";
import { copyMenu, EmptyState, Meter, Notice, Page, Section, StatusMark, type Status } from "../kit/kit";
import { Segmented } from "../kit/Segmented";
import { connected, daemon, daemonState } from "../daemon";
import type { PageNav } from "../routes";

const PAGE_SIZE = 30;
/** As wide as the old dashboard grew: four tiles across on a wide screen. */
const CONTENT_MAX_WIDTH = 1200;

const reading = signal<ResourceReading>({ telemetry: null, error: "", reading: false });
const details = signal(false);
const sort = signal<ConsumerSort>("rss");
const owner = signal<string>("all");
const shownCount = signal(PAGE_SIZE);
const open = signal<ReadonlySet<string>>(new Set());
const now = signal(Date.now());
const width = signal(360);

/** Resources: what the computer is doing now, read every five seconds while the page is in use. */
export function Resources(props: { nav: PageNav }) {
  let poller: ResourcePoller | null = null;
  let lastServer = "";
  // Reading stops while another page covers this one (the stack's top is not Resources).
  const onTop = () => {
    const routes = props.nav.routes?.();
    return !routes || routes.length === 0 || routes[routes.length - 1]?.name === "resources";
  };
  // A new computer starts from nothing; reading runs while connected, on top and the app in front.
  effect(() => {
    const current = daemon();
    const live = current !== null && daemonState() === "connected" && lifecycle() === "active" && onTop();
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
    <Page title="Resources" testID="resources" column="wide" maxWidth={CONTENT_MAX_WIDTH} refreshing={reading().reading && Boolean(telemetry())} onRefresh={refresh}>
      {telemetry() ? <Ready telemetry={telemetry()!} onRefresh={refresh} /> : <NotReady onRetry={refresh} onOpenSettings={() => props.nav.push({ name: "settings" })} />}
    </Page>
  );
}

function NotReady(props: { onRetry: () => void; onOpenSettings: () => void }) {
  const busy = () => reading().reading || daemonState() === "connecting";
  return (
    <>
      {!daemon() ? (
        <EmptyState testID="resources-empty" icon="pulse" title="No current server" detail="Pair a computer in Settings to see its resources." action="Open Settings" onAction={props.onOpenSettings} />
      ) : busy() ? (
        <EmptyState testID="resources-loading" busy title={connected() ? "Reading the machine" : "Connecting to server"} />
      ) : !connected() ? (
        <EmptyState testID="resources-offline" icon="pulse" title="Server offline" detail="Resources appear once the server reconnects." />
      ) : (
        <EmptyState testID="resources-failed" icon="pulse" title="Resources unavailable" detail={reading().error || "This server did not report resources."} action="Try again" onAction={props.onRetry} />
      )}
    </>
  );
}

function headline(t: ResourceTelemetry): { status: Status; title: string } {
  if (!hasMachineReading(t)) return { status: "neutral", title: "No reading" };
  if (t.state === "critical") return { status: "failed", title: "Critical pressure" };
  if (t.state === "elevated") return { status: "warning", title: "Elevated pressure" };
  return { status: "ready", title: "Normal" };
}

/** Pressure charts are ink until the machine is actually under pressure. */
function stateTone(t: ResourceTelemetry): ColorName {
  return t.state === "critical" ? "failed" : t.state === "elevated" ? "warning" : "ink";
}

function levelTone(level: MemoryPressureLevel): ColorName {
  return level === "critical" ? "failed" : level === "warn" ? "warning" : "ink";
}

function Ready(props: { telemetry: ResourceTelemetry; onRefresh: () => void }) {
  const t = () => props.telemetry;
  const caption = () => {
    const status = !connected() ? "Last sample · offline" : reading().error ? "Last sample" : sampledAgoLabel(t().sampledAt, now());
    return [status, historyWindowLabel(t())].filter(Boolean).join(" · ");
  };
  const refreshDisabled = () => reading().reading || !connected();
  return (
    <View onLayout={({ bounds }) => width.set(bounds.size.width)}>
      {connected() && reading().error ? <Notice testID="resources-refresh-failed" title="Refresh failed" detail={reading().error} action="Try again" onAction={props.onRefresh} /> : null}
      <View class="mt-4 flex-row items-center gap-2 px-1">
        <View class="flex-1 gap-1">
          <View class="flex-row items-center gap-2">
            <StatusMark status={headline(t()).status} />
            <Text testID="resources-headline" class="text-2xl font-bold" role="header">{headline(t()).title}</Text>
          </View>
          <Text testID="resources-caption" class="text-sm text-muted-foreground line-clamp-1">{caption()}</Text>
        </View>
        <Button testID="resources-refresh" accessibilityLabel="Refresh resources" disabled={refreshDisabled()} onPress={props.onRefresh} class="rounded-full p-2 active:opacity-60 disabled:opacity-40">
          <Icon name="refresh" size={18} />
        </Button>
        <View
          testID="resources-details"
          role="button"
          accessibilityLabel="Machine details"
          accessibilityState={{ expanded: details() }}
          tooltip={<Text class="text-xs">{details() ? "Hide machine details" : "Show machine details"}</Text>}
          onPress={() => details.set(!details())}
          class="rounded-full bg-secondary p-2 active:opacity-60"
        >
          <Icon name={details() ? "chevron-up" : "chevron-down"} size={18} />
        </View>
      </View>
      {t().signals.map((s) => (
        <View class="mt-2 flex-row items-center gap-2 px-1">
          <StatusMark status={s.state === "critical" ? "failed" : "warning"} />
          <Text class="text-sm">{`${s.name} · ${trim2(s.value)} / threshold ${trim2(s.threshold)}`}</Text>
        </View>
      ))}
      {/* Four across on a wide screen, one column on the narrowest, else two. */}
      <View class="flex-row flex-wrap gap-x-2">
        <View style={{ flexGrow: 1, flexBasis: tileBasis(width()) }}><Cpu t={t()} /></View>
        <View style={{ flexGrow: 1, flexBasis: tileBasis(width()) }}><Memory t={t()} /></View>
        <View style={{ flexGrow: 1, flexBasis: tileBasis(width()) }}><Disks t={t()} /></View>
        <View style={{ flexGrow: 1, flexBasis: tileBasis(width()) }}><Pressure t={t()} /></View>
      </View>
      <Consumers t={t()} />
    </View>
  );
}

function trim2(value: number): string {
  return value.toFixed(2).replace(/\.?0+$/, "");
}

/** An area chart of `series` (0..1) over the page's time window, named for a screen reader. */
function Chart(props: { series: SeriesPoint[]; window: { start: number; end: number }; label: string; tone?: ColorName; height?: number; grid?: boolean; fill?: boolean }) {
  const height = () => props.height ?? 44;
  return (
    <Canvas
      style={{ width: "100%", height: height() }}
      role="image"
      accessibilityLabel={props.label}
      draw={(cx, size) => {
        const span = Math.max(1, props.window.end - props.window.start);
        if (props.grid !== false) {
          cx.fillStyle = color("hairline");
          cx.fillRect(0, size.height - 1, size.width, 1);
        }
        const points = props.series;
        if (points.length === 0) return;
        const xy = points.map((p) => [((p.at - props.window.start) / span) * size.width, size.height - 1 - Math.min(1, Math.max(0, p.value)) * (size.height - 2)] as [number, number]);
        // A lone sample still draws a mark.
        if (xy.length === 1) xy.unshift([Math.max(0, xy[0][0] - 1), xy[0][1]]);
        const ink = color(props.tone ?? "ink");
        cx.beginPath();
        cx.moveTo(xy[0][0], xy[0][1]);
        for (const [x, y] of xy.slice(1)) cx.lineTo(x, y);
        cx.strokeStyle = ink;
        cx.lineWidth = 1.5;
        cx.lineJoin = "round";
        cx.stroke();
        if (props.fill === false) return;
        cx.lineTo(xy[xy.length - 1][0], size.height);
        cx.lineTo(xy[0][0], size.height);
        cx.closePath();
        cx.fillStyle = withAlpha(ink, 0.12);
        cx.fill();
      }}
    />
  );
}

/** A thin timeline of the pressure state under the CPU chart; normal stays quiet. */
function PressureStrip(props: { spans: StateSpan[]; window: { start: number; end: number } }) {
  return (
    <Canvas
      testID="cpu-pressure-strip"
      class="mt-1"
      style={{ width: "100%", height: 4 }}
      accessibilityElementsHidden
      draw={(cx, size) => {
        const span = Math.max(1, props.window.end - props.window.start);
        cx.fillStyle = color("hairline");
        cx.beginPath();
        cx.roundRect(0, 0, size.width, size.height, 2);
        cx.fill();
        for (const item of props.spans) {
          if (item.state === "normal") continue;
          const x = Math.max(0, ((item.start - props.window.start) / span) * size.width);
          const right = Math.min(size.width, ((item.end - props.window.start) / span) * size.width);
          cx.fillStyle = color(item.state === "critical" ? "failed" : "warning");
          cx.beginPath();
          cx.roundRect(x, 0, Math.max(3, right - x), size.height, 2);
          cx.fill();
        }
      }}
    />
  );
}

/** A colour key beside what it names. */
function Legend(props: { tone: ColorName; label: string }) {
  return (
    <View class="flex-row items-center gap-1.5">
      <View class="h-2 w-2 rounded-full" style={{ backgroundColor: color(props.tone) }} />
      <Text class="text-xs text-muted-foreground">{props.label}</Text>
    </View>
  );
}

function Tile(props: { title: string; value: string; testID: string; children?: JSX.Element; caption?: string; micro?: string; detail?: JSX.Element }) {
  return (
    <Section testID={props.testID}>
      <View class="gap-2 px-4 py-3">
        <View class="flex-row items-baseline justify-between">
          <Text class="text-sm font-medium text-muted-foreground">{props.title}</Text>
          <Text class="text-2xl font-bold" style={{ fontVariant: ["tabular-nums"] }}>{props.value}</Text>
        </View>
        {props.children}
        {props.caption ? <Text class="text-xs text-muted-foreground">{props.caption}</Text> : null}
        {props.micro ? <Text class="text-xs text-muted-foreground">{props.micro}</Text> : null}
        {details() ? props.detail ?? null : null}
      </View>
    </Section>
  );
}

function Fact(props: { label: string; value: string }) {
  return (
    <View class="flex-row justify-between gap-3">
      <Text class="text-sm text-muted-foreground">{props.label}</Text>
      <Text class="shrink font-mono text-sm" style={{ fontVariant: ["tabular-nums"] }}>{props.value}</Text>
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
            {cores().map((c) => <View class="w-[22px]"><Meter share={c / 100} tone={c >= 90 ? "warning" : "ink"} height={18} /></View>)}
          </View>
          <Fact label="Load · 1 / 5 / 15 min" value={`${load(props.t.cpu.load1)} / ${load(props.t.cpu.load5)} / ${load(props.t.cpu.load15)}`} />
        </View>
      }
    >
      <Chart series={cpuHistorySeries(props.t)} window={chartWindow(props.t)} label="CPU utilization history, scale 0–100%" />
      <PressureStrip spans={pressureSpans(props.t)} window={chartWindow(props.t)} />
    </Tile>
  );
}

function Memory(props: { t: ResourceTelemetry }) {
  const used = () => memoryUsedRatio(props.t);
  const m = () => props.t.memory;
  const swapShare = () => (m().swapTotalBytes ? (m().swapUsedBytes ?? 0) / m().swapTotalBytes! : 0);
  const tone = (): ColorName => ((used() ?? 0) >= 0.9 ? "warning" : "ink");
  return (
    <Tile
      testID="tile-memory"
      title="Memory"
      value={used() === undefined ? "—" : formatPercent(used()! * 100)}
      caption={`${formatBytes(memoryUsedBytes(props.t))} / ${formatBytes(m().totalBytes)}`}
      detail={
        <View class="gap-1.5 pt-1">
          <Fact label="Available" value={formatBytes(m().availableBytes)} />
          <Fact label={props.t.unavailable.includes("memory.shared") ? "Cache" : "Cache · Shared"} value={props.t.unavailable.includes("memory.shared") ? formatBytes(m().cacheBytes) : `${formatBytes(m().cacheBytes)} · ${formatBytes(m().sharedBytes)}`} />
          <Fact label="Swap used / total" value={`${formatBytes(m().swapUsedBytes)} / ${formatBytes(m().swapTotalBytes)}`} />
          {m().swapTotalBytes ? <Meter share={swapShare()} tone={swapShare() >= 0.9 ? "warning" : "ink"} height={4} /> : null}
        </View>
      }
    >
      <Chart series={memoryHistorySeries(props.t)} window={chartWindow(props.t)} tone={tone()} label="Memory utilization history, scale 0–100%" />
      <Meter share={used() ?? 0} tone={tone()} height={4} />
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
      {/* Read and write share one scale: write is drawn over read, each named on its own. */}
      <View style={{ height: 44 }}>
        <Chart series={series().read} window={chartWindow(props.t)} label="Disk read throughput history" />
        <View class="absolute inset-0" style={{ pointerEvents: "none" }}>
          <Chart series={series().write} window={chartWindow(props.t)} tone="soft" grid={false} fill={false} label="Disk write throughput history" />
        </View>
      </View>
      <View class="flex-row flex-wrap gap-x-3 gap-y-1">
        <Legend tone="ink" label={`R ${formatRate(latest().read)}`} />
        <Legend tone="soft" label={`W ${formatRate(latest().write)}`} />
      </View>
    </Tile>
  );
}

/** Linux reports PSI; a Mac reports a memory pressure level instead. */
function Pressure(props: { t: ResourceTelemetry }) {
  return <>{usesMemoryPressure(props.t) ? <MemoryPressureTile t={props.t} /> : <PsiTile t={props.t} />}</>;
}

function PsiTile(props: { t: ResourceTelemetry }) {
  const peak = () => psiPeak(props.t);
  const triple = (a?: { avg10: number; avg60: number; avg300: number }) => (a ? `${formatPressurePercent(a.avg10)} / ${formatPressurePercent(a.avg60)} / ${formatPressurePercent(a.avg300)}` : "—");
  return (
    <Tile
      testID="tile-pressure"
      title="Pressure"
      value={formatPressurePercent(peak()?.value)}
      caption={peak() ? `${PSI_LABEL[peak()!.key]} · max some 10s` : "PSI unavailable"}
      micro={PSI_KEYS.map((key) => `${PSI_LABEL[key]} ${formatPressurePercent(props.t.psi[key]?.some?.avg10)}`).join(" · ")}
      detail={
        <View class="gap-2 pt-1">
          <Text class="text-xs text-muted-foreground">some / full · 10s / 1m / 5m</Text>
          {PSI_KEYS.map((key) => (
            <View class="gap-0.5">
              <Text class="text-sm font-medium">{PSI_LABEL[key]}</Text>
              <Fact label="some" value={triple(props.t.psi[key]?.some)} />
              <Fact label="full" value={triple(props.t.psi[key]?.full)} />
              <Chart series={psiHistorySeries(props.t, key)} window={chartWindow(props.t)} tone={stateTone(props.t)} height={24} label={psiResourceChartLabel(key)} />
            </View>
          ))}
        </View>
      }
    >
      <Chart series={peak() ? psiHistorySeries(props.t, peak()!.key) : []} window={chartWindow(props.t)} tone={stateTone(props.t)} label={psiChartLabel(peak()?.key)} />
    </Tile>
  );
}

/** The level as a three-step meter, its history and the load behind it; no PSI figure is shown or implied. */
function MemoryPressureTile(props: { t: ResourceTelemetry }) {
  const level = () => props.t.memoryPressure!.level;
  const lit = () => MEMORY_PRESSURE_STEPS[level()];
  return (
    <Tile testID="tile-pressure" title="Pressure" value={MEMORY_PRESSURE_LABEL[level()]} caption="Memory pressure" micro={memoryPressureMicro(props.t)}>
      <Chart series={memoryPressureSeries(props.t)} window={chartWindow(props.t)} tone={levelTone(level())} label="Memory pressure history, from normal to critical" />
      <View testID="memory-pressure-meter" class="flex-row gap-[3px]" accessible accessibilityLabel={memoryPressureMeterLabel(level())}>
        {[1, 2, 3].map((step) => (
          <View class="h-1 flex-1 rounded-full" style={{ backgroundColor: color(step <= lit() ? levelTone(level()) : "hairline") }} />
        ))}
      </View>
    </Tile>
  );
}

const OWNERS: ReadonlyArray<{ key: ResourceConsumerOwner; label: string }> = [
  { key: "worker", label: "Workers" },
  { key: "orphaned_worker", label: "Orphaned Workers" },
  { key: "brain", label: "Brain" },
  { key: "docker", label: "Docker" },
  { key: "user", label: "User processes" },
];

/** One glyph names the owner, so rows don't repeat "User process" in words. */
const OWNER_ICON: Record<ResourceConsumerOwner, IconName> = {
  worker: "terminal",
  orphaned_worker: "warning",
  brain: "brain",
  docker: "cube",
  user: "person",
};

function Consumers(props: { t: ResourceTelemetry }) {
  const all = () => sortConsumers(props.t.consumers, sort());
  const filtered = () => (owner() === "all" ? all() : all().filter((c) => c.owner === owner()));
  const keys = () => consumerRowKeys(filtered());
  const counts = () => new Map(groupConsumers(props.t.consumers).map((g) => [g.consumers[0]?.owner, g.consumers.length]));
  const wide = () => consumersWide(width());
  const choose = (key: string) => {
    owner.set(key);
    shownCount.set(PAGE_SIZE);
  };
  return (
    <View class="mt-8">
      <View class="flex-row items-center justify-between px-1">
        <Text class="text-lg font-semibold" role="header">{`Using this machine ${props.t.consumers.length}`}</Text>
        <View class="w-[150px]">
          <Segmented testID="consumer-sort" options={[{ key: "rss", label: "Memory" }, { key: "cpu", label: "CPU" }]} value={sort()} onChange={(key) => sort.set(key as ConsumerSort)} />
        </View>
      </View>
      <View class="mt-3 flex-row flex-wrap gap-2" accessibilityLabel="Filter by owner">
        <Chip testID="owner-all" label={`All ${props.t.consumers.length}`} accessibilityLabel="Filter All" selected={owner() === "all"} onPress={() => choose("all")} />
        {OWNERS.filter((o) => counts().has(o.key)).map((o) => (
          <Chip
            testID={`owner-${o.key}`}
            icon={OWNER_ICON[o.key]}
            label={`${o.label} ${counts().get(o.key)}`}
            accessibilityLabel={`Filter ${o.label}`}
            selected={owner() === o.key}
            warning={o.key === "orphaned_worker"}
            onPress={() => choose(o.key)}
          />
        ))}
      </View>
      <Section testID="consumers">
        <View testID="consumers-header" class="flex-row items-center gap-3 px-4 pb-1 pt-2.5">
          <View class="flex-1" />
          <Text class="w-[52px] text-right text-xs text-muted-foreground">CPU</Text>
          <Text class="w-[64px] text-right text-xs text-muted-foreground">Memory</Text>
          {wide() ? <Text class="w-[44px] text-right text-xs text-muted-foreground">Procs</Text> : null}
          <View class="w-[14px]" />
        </View>
        {filtered().length === 0 ? (
          <Text class="px-4 py-4 text-sm text-muted-foreground">{owner() === "all" ? "No consumers reported." : "No consumers reported for this owner."}</Text>
        ) : (
          filtered().slice(0, shownCount()).map((c, index) => <ConsumerRow consumer={c} rowKey={keys()[index]} wide={wide()} />)
        )}
        {filtered().length > shownCount() ? (
          <Button testID="consumers-more" accessibilityLabel={`Show ${Math.min(PAGE_SIZE, filtered().length - shownCount())} more`} onPress={() => shownCount.set(shownCount() + PAGE_SIZE)} class="flex-row items-center gap-1.5 px-4 py-3 active:bg-accent">
            <Icon name="chevron-down" size={16} />
            <Text class="text-sm font-medium text-muted-foreground">{`${filtered().length - shownCount()} more`}</Text>
          </Button>
        ) : null}
      </Section>
      <Text class="mt-1.5 px-4 text-xs text-muted-foreground">CPU 100% is one core. Memory is resident size and may count shared pages twice.</Text>
    </View>
  );
}

function Chip(props: { label: string; selected: boolean; icon?: IconName; warning?: boolean; accessibilityLabel?: string; testID?: string; onPress: () => void }) {
  return (
    <Button
      testID={props.testID}
      accessibilityLabel={props.accessibilityLabel}
      selected={props.selected}
      onPress={props.onPress}
      class={props.selected ? "flex-row items-center gap-1.5 rounded-full bg-primary px-3 py-1.5" : "flex-row items-center gap-1.5 rounded-full border border-border bg-card px-3 py-1.5 active:bg-accent"}
    >
      {props.icon ? <Icon name={props.icon} size={14} tone={props.selected ? "paper" : props.warning ? "warning" : "soft"} /> : null}
      <Text class={props.selected ? "text-sm font-medium text-primary-foreground" : "text-sm font-medium"} style={!props.selected && props.warning ? { color: color("warning") } : undefined}>{props.label}</Text>
    </Button>
  );
}

function ConsumerRow(props: { consumer: ResourceConsumer; rowKey: string; wide: boolean }) {
  const c = () => props.consumer;
  const title = () => consumerTitle(c());
  const orphaned = () => c().owner === "orphaned_worker";
  const expandedNow = () => open().has(props.rowKey);
  const toggle = () => {
    const next = new Set(open());
    if (next.has(props.rowKey)) next.delete(props.rowKey);
    else next.add(props.rowKey);
    open.set(next);
  };
  const kinds = () => kindChips(c().kinds).map((k) => k.label).join(" · ");
  return (
    <View>
      <View
        testID={`consumer-${consumerKey(c())}`}
        role="button"
        accessibilityLabel={consumerAccessibilityLabel(c(), title(), expandedNow())}
        accessibilityState={{ expanded: expandedNow() }}
        onPress={toggle}
        class="active:bg-accent"
      >
        <View class="flex-row items-center gap-3 px-4 py-3">
          <Icon name={OWNER_ICON[c().owner]} size={16} tone={orphaned() ? "warning" : "faint"} />
          <View class="min-w-0 flex-1">
            <Text class="text-base line-clamp-1">{title()}</Text>
            {consumerRowMeta(c()) ? <Text testID="consumer-meta" class="text-sm text-muted-foreground line-clamp-1" style={orphaned() ? { color: color("warning") } : undefined}>{consumerRowMeta(c())}</Text> : null}
          </View>
          <Text class="w-[52px] text-right font-mono text-xs" style={{ fontVariant: ["tabular-nums"] }}>{formatPercent(c().cpuPercent)}</Text>
          <Text class="w-[64px] text-right font-mono text-sm" style={{ fontVariant: ["tabular-nums"] }}>{formatBytes(c().rssBytes)}</Text>
          {props.wide ? <Text class="w-[44px] text-right font-mono text-xs" style={{ fontVariant: ["tabular-nums"] }}>{String(c().processCount)}</Text> : null}
          <Icon name={expandedNow() ? "chevron-up" : "chevron-down"} size={14} tone="faint" />
        </View>
      </View>
      {expandedNow() ? (
        <View class="gap-1 bg-secondary px-4 py-3">
          {/* A long press copies the title, the IDs or the commands, as selecting them did. */}
          <View testID="consumer-details" class="gap-1" onLongPress={() => void copyMenu(title(), consumerCopyChoices(c(), title()))}>
            <Text class="text-base font-medium">{title()}</Text>
            <Text class="text-sm">{`${CONSUMER_OWNER_LABEL[c().owner]} · ${c().processCount} ${c().processCount === 1 ? "process" : "processes"}`}</Text>
            {c().id ? <Fact label="ID" value={c().id!} /> : null}
            {c().workerId ? <Fact label="Worker" value={c().workerId!} /> : null}
            {c().workId ? <Fact label="Work" value={c().workId!} /> : null}
            {consumerMeta(c()) ? <Text class="text-sm text-muted-foreground">{consumerMeta(c())}</Text> : null}
            {consumerCommands(c()).length ? <Text class="text-sm text-muted-foreground">{`Commands · ${consumerCommands(c()).join(", ")}`}</Text> : null}
            {kinds() ? <Text class="text-sm text-muted-foreground">{`Kinds · ${kinds()}`}</Text> : null}
          </View>
          {c().processes.length ? (
            <View class="gap-0.5 pt-1">
              <Text class="text-xs text-muted-foreground">{`${c().processes.length} / ${c().processCount} processes reported · largest memory`}</Text>
              <View class="flex-row gap-2 pt-1">
                <Text class="w-[64px] text-xs text-muted-foreground">PID</Text>
                <Text class="flex-1 text-xs text-muted-foreground">Command / start token</Text>
                <Text class="text-xs text-muted-foreground">Memory</Text>
              </View>
              {processesByMemory(c()).map((p) => (
                <View testID={`process-${p.pid ?? "unknown"}`} class="flex-row gap-2 py-0.5" onLongPress={() => void copyMenu(p.pid ? `PID ${p.pid}` : p.command ?? "Unknown process", processCopyChoices(p))}>
                  <Text class="w-[64px] font-mono text-xs">{p.pid ? String(p.pid) : "—"}</Text>
                  <View class="min-w-0 flex-1 gap-0.5">
                    <Text class="font-mono text-xs line-clamp-2">{p.command ?? "Unknown process"}</Text>
                    {p.start ? <Text class="font-mono text-xs text-muted-foreground">{`start ${p.start}`}</Text> : null}
                  </View>
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
