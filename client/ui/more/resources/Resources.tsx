import { wide } from "../../shell/layout";
import type { JSX } from "nsh";
import { cleanup, effect, Scroll, signal, Text, untracked, View, lifecycle } from "nsh";
import { Button } from "nsh/base";
import { serverDisplayName } from "../../../core/extensions/serverDisplayName";
import { ResourcePoller, type ResourceReading } from "../../../core/resources/poll";
import {
  consumerAccessibilityLabel,
  consumerCommands,
  consumerCopyChoices,
  consumerRowKeys,
  consumerRowMeta,
  consumersWide,
  memoryPressureMicro,
  memoryUsedBytes,
  pressureWords,
  primaryDisk,
  processCopyChoices,
  processesByMemory,
  PSI_KEYS,
  PSI_LABEL,
  tileBasis,
  usesMemoryPressure,
} from "../../../core/resources/presentation";
import {
  CONSUMER_OWNER_LABEL,
  consumerKey,
  consumerMeta,
  consumerTitle,
  formatBytes,
  formatPercent,
  formatPressurePercent,
  formatRate,
  groupConsumers,
  hasMachineReading,
  kindChips,
  latestDiskThroughput,
  MEMORY_PRESSURE_LABEL,
  memoryUsedRatio,
  sortConsumers,
  type ConsumerSort,
  type ResourceConsumer,
  type ResourceConsumerOwner,
  type ResourceTelemetry,
} from "../../../core/resources/resourceTelemetry";
import { consumerLead, meterWord, pressureTone, resourceMeterTone, type ResourceMeter } from "../../../core/resources/colour";
import { DateStyle, formatDate } from "../../../core/timeline/dates";
import { AgentIcon } from "../../icons/AgentIcon";
import { pet } from "../../pets/pets";
import { color } from "../../kit/colors";
import { Icon } from "../../kit/Icon";
import { copyMenu, DisplayTitle, EmptyState, Meter, Notice, Page, Section, SectionLabel, StatTile } from "../../kit/kit";
import { LEAD_SLOT, meterColor, RowTile, StateLine, type Lead } from "../../kit/RowTile";
import { Segmented } from "../../kit/Segmented";
import { connected, daemon, daemonName, daemonState } from "../daemon";
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
    poller = new ResourcePoller(current, (state) => reading.set(state), undefined, untracked(reading).telemetry);
    poller.start();
  });
  cleanup(() => poller?.stop());
  const refresh = () => poller?.refresh();
  // SPEC §16.2: background polls (every 5 s) draw nothing; the pull's spinner shows only for a person's pull.
  const pulled = signal(false);
  effect(() => {
    if (!reading().reading) untracked(() => pulled() && pulled.set(false));
  });
  const pull = () => {
    pulled.set(true);
    refresh();
  };
  const telemetry = () => reading().telemetry;
  return (
    <Page
      display
      title="Resources"
      hero={<If when={telemetry()} else={<DisplayTitle title="Resources" />}><Headline telemetry={telemetry()!} /></If>}
      headerRight={<If when={telemetry()}><HeaderActions onRefresh={refresh} /></If>}
      testID="resources"
      maxWidth={CONTENT_MAX_WIDTH}
      refreshing={pulled() && reading().reading && Boolean(telemetry())}
      onRefresh={pull}
    >
      <If when={telemetry()} else={<NotReady onRetry={refresh} onOpenSettings={() => props.nav.push({ name: "settings" })} />}><Ready telemetry={telemetry()!} onRefresh={refresh} /></If>
    </Page>
  );
}

function NotReady(props: { onRetry: () => void; onOpenSettings: () => void }) {
  const busy = () => reading().reading || daemonState() === "connecting";
  return (
    <>
      <If when={!daemon()} else={<If when={busy()} else={<If when={!connected()} else={<EmptyState testID="resources-failed" icon="pulse" title="Resources unavailable" detail={reading().error || "This server did not report resources."} action="Try again" onAction={props.onRetry} />}><EmptyState testID="resources-offline" icon="pulse" title="Server offline" detail="Resources appear once the server reconnects." /></If>}><EmptyState testID="resources-loading" busy title={connected() ? "Reading the machine" : "Connecting to server"} /></If>}><EmptyState testID="resources-empty" icon="pulse" title="No current server" detail="Pair a computer in Settings to see its resources." action="Open Settings" onAction={props.onOpenSettings} /></If>
    </>
  );
}

/** The headline is the pressure in words (SPEC §15 Metrics). */
function headline(t: ResourceTelemetry): string {
  if (!hasMachineReading(t)) return "No reading";
  if (t.state === "critical") return "Critical pressure";
  if (t.state === "elevated") return "Elevated pressure";
  return "Comfortable";
}

/** The page's top block (SPEC §15 Metrics): the computer's name, the pressure in words, then what is under pressure and when it was read. */
function Headline(props: { telemetry: ResourceTelemetry }) {
  const t = () => props.telemetry;
  const updated = () => `· updated ${formatDate(new Date(t().sampledAt), DateStyle.time)}`;
  return (
    <View testID="resources-headline">
      <DisplayTitle eyebrow={serverDisplayName(daemonName(), "This computer")} title={headline(t())} />
      {/* The daemon's pressure (SPEC §10.4): its dot and words in ok, warn or red; offline, a soft ring and soft words. */}
      <View class="flex-row items-center gap-1.5 px-1 pb-2">
        <If when={connected()} else={<>
            <View class="size-2 rounded-full" style={{ borderWidth: 1.5, borderColor: color("soft") }} />
            <Text testID="resources-line" class="shrink text-sub text-muted-foreground line-clamp-1">{`${pressureWords(t())} ${updated()}`}</Text>
          </>}><StateLine testID="resources-line" tone={pressureTone(t().state)} label={pressureWords(t())} detail={updated()} /></If>
      </View>
    </View>
  );
}

/** Refresh and the machine details, in the header. */
function HeaderActions(props: { onRefresh: () => void }) {
  const refreshDisabled = () => reading().reading || !connected();
  return (
    <View class="flex-row items-center gap-1">
      <Button testID="resources-refresh" accessibilityLabel="Refresh resources" disabled={refreshDisabled()} onPress={props.onRefresh} class="size-11 items-center justify-center rounded-full active:bg-accent disabled:opacity-40">
        <Icon name="refresh" size={20} />
      </Button>
      <View
        testID="resources-details"
        role="button"
        accessibilityLabel="Machine details"
        accessibilityState={{ expanded: details() }}
        tooltip={<Text class="text-caption">{details() ? "Hide machine details" : "Show machine details"}</Text>}
        onPress={() => details.set(!details())}
        class={`size-11 items-center justify-center rounded-full active:bg-accent ${details() ? "bg-muted" : ""}`}
      >
        <Icon name="info" size={20} tone={details() ? "ink" : "soft"} />
      </View>
    </View>
  );
}

function Ready(props: { telemetry: ResourceTelemetry; onRefresh: () => void }) {
  const t = () => props.telemetry;
  const basis = () => tileBasis(width(), wide());
  return (
    <View onLayout={({ bounds }) => width.set(bounds.size.width)}>
      <If when={connected() && reading().error}><Notice testID="resources-refresh-failed" title="Refresh failed" detail={reading().error} action="Try again" onAction={props.onRefresh} /></If>
      <If when={details()}><MachineDetails t={t()} /></If>
      {/* CPU, Memory, Disk, Swap: two across on a phone, four on a wide screen (SPEC §15 Metrics). */}
      <View testID="resource-tiles" class="mt-3 flex-row flex-wrap gap-3">
        <View style={{ flexGrow: 1, flexBasis: basis() }}><Cpu t={t()} /></View>
        <View style={{ flexGrow: 1, flexBasis: basis() }}><Memory t={t()} /></View>
        <View style={{ flexGrow: 1, flexBasis: basis() }}><Disk t={t()} /></View>
        <View style={{ flexGrow: 1, flexBasis: basis() }}><Swap t={t()} /></View>
      </View>
      <Consumers t={t()} />
    </View>
  );
}

function trim2(value: number): string {
  return value.toFixed(2).replace(/\.?0+$/, "");
}

/** The raw readings behind the words, while ⓘ is open: each signal against its threshold, and the pressure the kernel reports. */
function MachineDetails(props: { t: ResourceTelemetry }) {
  const triple = (a?: { avg10: number; avg60: number; avg300: number }) => (a ? `${formatPressurePercent(a.avg10)} / ${formatPressurePercent(a.avg60)} / ${formatPressurePercent(a.avg300)}` : "—");
  return (
    <View testID="resources-machine-details" class="mt-2 gap-2 rounded-(--radius-group) bg-card p-4">
      {props.t.signals.map((s) => (
        <View class="flex-row items-center gap-2">
          <View class="size-2 rounded-full" style={{ backgroundColor: color(s.state === "critical" ? "seal" : "warning") }} />
          <Text class="shrink font-mono text-code">{`${s.name} · ${trim2(s.value)} / threshold ${trim2(s.threshold)}`}</Text>
        </View>
      ))}
      <If when={usesMemoryPressure(props.t)} else={<View class="gap-1">
          <Text class="text-caption text-muted-foreground">PSI some / full · 10s / 1m / 5m</Text>
          {PSI_KEYS.map((key) => (
            <View class="gap-0.5">
              <Text class="text-sub font-semibold">{PSI_LABEL[key]}</Text>
              <Fact label="some" value={triple(props.t.psi[key]?.some)} />
              <Fact label="full" value={triple(props.t.psi[key]?.full)} />
            </View>
          ))}
        </View>}><View class="gap-0.5">
          <Fact label="Memory pressure" value={MEMORY_PRESSURE_LABEL[props.t.memoryPressure!.level]} />
          <Text class="text-sub text-muted-foreground">{memoryPressureMicro(props.t)}</Text>
        </View></If>
    </View>
  );
}

/** A stat tile (SPEC §15 Metrics): the label, the figure, a soft line, a 4 pt bar, and its facts while ⓘ is open. */
/**
 * A tile's meter turns on the daemon's thresholds for what it judges (`meter`), on 80 % and 95 % for what it does
 * not (swap); its status word ("High") carries the colour beside the number, never the number (SPEC §10.4).
 */
function Tile(props: { title: string; value: string; testID: string; t: ResourceTelemetry; meter: ResourceMeter | null; mount?: string; line?: string; share?: number; detail?: JSX.Element }) {
  const tone = () => resourceMeterTone(props.t, props.meter, props.share ?? 0, props.mount);
  const word = () => (props.share === undefined && props.meter === null ? null : meterWord(tone()));
  return (
    <StatTile testID={props.testID} label={props.title} value={props.value} detail={props.line} status={word() ? { word: word()!, tone: tone() === "error" ? "error" : "warn" } : undefined}>
      <If when={props.share !== undefined}><Meter share={props.share!} tone={meterColor(tone())} height={4} /></If>
      {details() ? props.detail ?? null : null}
    </StatTile>
  );
}

/** A label and its value on one line, or the value on its own line at the right when a narrow tile can't hold both. */
function Fact(props: { label: string; value: string }) {
  return (
    <View class="flex-row flex-wrap justify-between gap-x-3">
      <Text class="shrink text-sub text-muted-foreground">{props.label}</Text>
      <Text class="ml-auto shrink-0 text-right font-mono text-code" style={{ fontVariant: ["tabular-nums"] }}>{props.value}</Text>
    </View>
  );
}

function Cpu(props: { t: ResourceTelemetry }) {
  const cores = () => props.t.cpu.perCorePercent;
  const used = () => props.t.cpu.utilizationPercent;
  const load = (v?: number) => (v === undefined ? "—" : v.toFixed(2));
  return (
    <Tile
      testID="tile-cpu"
      title="CPU"
      t={props.t}
      meter="cpu"
      value={formatPercent(used())}
      line={cores().length ? `${cores().length} cores` : undefined}
      share={used() === undefined ? undefined : used()! / 100}
      detail={
        <View class="gap-2">
          <If when={cores().length}><Fact label="Busiest core" value={formatPercent(Math.max(...cores()))} /></If>
          <View class="flex-row flex-wrap gap-1">
            {cores().map((c) => <View class="w-[22px]"><Meter share={c / 100} tone={meterColor(resourceMeterTone(props.t, null, c / 100))} height={18} /></View>)}
          </View>
          <Fact label="Load · 1 / 5 / 15 min" value={`${load(props.t.cpu.load1)} / ${load(props.t.cpu.load5)} / ${load(props.t.cpu.load15)}`} />
        </View>
      }
    />
  );
}

function Memory(props: { t: ResourceTelemetry }) {
  const m = () => props.t.memory;
  return (
    <Tile
      testID="tile-memory"
      title="Memory"
      t={props.t}
      meter="memory"
      value={formatBytes(memoryUsedBytes(props.t))}
      line={m().totalBytes !== undefined ? `of ${formatBytes(m().totalBytes)}` : undefined}
      share={memoryUsedRatio(props.t)}
      detail={
        <View class="gap-1.5">
          <Fact label="Available" value={formatBytes(m().availableBytes)} />
          <Fact label={props.t.unavailable.includes("memory.shared") ? "Cache" : "Cache · Shared"} value={props.t.unavailable.includes("memory.shared") ? formatBytes(m().cacheBytes) : `${formatBytes(m().cacheBytes)} · ${formatBytes(m().sharedBytes)}`} />
        </View>
      }
    />
  );
}

/** The root disk's space (else the largest); every mount and the disk traffic while ⓘ is open. */
function Disk(props: { t: ResourceTelemetry }) {
  const disk = () => primaryDisk(props.t);
  const traffic = () => latestDiskThroughput(props.t);
  return (
    <Tile
      testID="tile-disk"
      title="Disk"
      t={props.t}
      meter="disk"
      mount={disk()?.mount}
      value={disk() ? formatBytes(disk()!.usedBytes) : "—"}
      line={disk() ? `of ${formatBytes(disk()!.totalBytes)}` : undefined}
      share={disk()?.totalBytes ? disk()!.usedBytes / disk()!.totalBytes : undefined}
      detail={
        <View class="gap-2">
          <Fact label="Read · Write" value={`${formatRate(traffic().read)} · ${formatRate(traffic().write)}`} />
          {props.t.disks.map((d) => (
            <View class="gap-1">
              <Fact label={d.mount} value={`${formatBytes(d.freeBytes)} free / ${formatBytes(d.totalBytes)}`} />
              <Meter share={d.totalBytes ? d.usedBytes / d.totalBytes : 0} tone={meterColor(resourceMeterTone(props.t, "disk", 0, d.mount))} height={4} />
            </View>
          ))}
        </View>
      }
    />
  );
}

function Swap(props: { t: ResourceTelemetry }) {
  const total = () => props.t.memory.swapTotalBytes;
  const used = () => props.t.memory.swapUsedBytes ?? 0;
  return (
    <Tile
      testID="tile-swap"
      title="Swap"
      t={props.t}
      meter={null}
      value={total() === undefined ? "—" : formatBytes(used())}
      line={total() === undefined ? undefined : `of ${formatBytes(total())}`}
      share={total() ? used() / total()! : undefined}
    />
  );
}

const OWNERS: ReadonlyArray<{ key: ResourceConsumerOwner; label: string }> = [
  { key: "worker", label: "Workers" },
  { key: "orphaned_worker", label: "Orphaned Workers" },
  { key: "brain", label: "Brain" },
  { key: "docker", label: "Docker" },
  { key: "user", label: "User processes" },
];

/** A row's mark (SPEC §10.4): an agent's logo, Mewla's own (the pet) for Brain and the daemon, a program's mark, or the cube on the neutral tile. */
function consumerRowLead(c: ResourceConsumer): Lead {
  const lead = consumerLead(c);
  if (lead.kind === "agent") return { kind: "logo", node: <AgentIcon kind={lead.agent} size={28} slot={LEAD_SLOT} /> };
  if (lead.kind === "mewla") return { kind: "art", src: pet().art.portrait };
  return lead;
}

function Consumers(props: { t: ResourceTelemetry }) {
  const all = () => sortConsumers(props.t.consumers, sort());
  const filtered = () => (owner() === "all" ? all() : all().filter((c) => c.owner === owner()));
  const keys = () => consumerRowKeys(filtered());
  const counts = () => new Map(groupConsumers(props.t.consumers).map((g) => [g.consumers[0]?.owner, g.consumers.length]));
  const procs = () => consumersWide(width(), wide());
  const choose = (key: string) => {
    owner.set(key);
    shownCount.set(PAGE_SIZE);
  };
  return (
    <View class="mt-2">
      <SectionLabel
        title="Using this machine"
        icon="layers"
        count={props.t.consumers.length}
        trailing={
          <View class="ml-2 w-[150px]">
            <Segmented testID="consumer-sort" options={[{ key: "rss", label: "Memory" }, { key: "cpu", label: "CPU" }]} value={sort()} onChange={(key) => sort.set(key as ConsumerSort)} />
          </View>
        }
      />
      {/* One row that scrolls sideways, no glyphs (FIDELITY-AUDIT 19): the chips never wrap. */}
      <Scroll horizontal scrollbar={false} testID="owner-chips" class="grow-0" contentClass="flex-row gap-2 pr-4" keyboardShouldPersistTaps="handled">
        <Chip testID="owner-all" label={`All ${props.t.consumers.length}`} accessibilityLabel="Filter All" selected={owner() === "all"} onPress={() => choose("all")} />
        {OWNERS.filter((o) => counts().has(o.key)).map((o) => (
          <Chip
            testID={`owner-${o.key}`}
            label={`${o.label} ${counts().get(o.key)}`}
            accessibilityLabel={`Filter ${o.label}`}
            selected={owner() === o.key}
            onPress={() => choose(o.key)}
          />
        ))}
      </Scroll>
      <Section testID="consumers">
        <View testID="consumers-header" class="flex-row items-center gap-3 px-4 pb-1 pt-2.5">
          <View class="flex-1" />
          <Text class="w-[52px] text-right text-caption text-muted-foreground">CPU</Text>
          <Text class="w-[64px] text-right text-caption text-muted-foreground">Memory</Text>
          <If when={procs()}><Text class="w-[44px] text-right text-caption text-muted-foreground">Procs</Text></If>
          <If when={procs()}><View class="w-[72px]" /></If>
          <View class="w-[14px]" />
        </View>
        <If when={filtered().length === 0} else={<>{filtered().slice(0, shownCount()).map((c, index) => <ConsumerRow consumer={c} rowKey={keys()[index]} wide={procs()} share={consumerShare(c, filtered())} />)}</>}><View class="px-4 py-4"><Text class="text-sub text-muted-foreground">{owner() === "all" ? "No consumers reported." : "No consumers reported for this owner."}</Text></View></If>
        <If when={filtered().length > shownCount()}><Button testID="consumers-more" accessibilityLabel={`Show ${Math.min(PAGE_SIZE, filtered().length - shownCount())} more`} onPress={() => shownCount.set(shownCount() + PAGE_SIZE)} class="flex-row items-center gap-1.5 px-4 py-3 active:bg-accent">
            <Icon name="chevron-down" size={16} />
            <Text class="text-sub font-semibold text-muted-foreground">{`${filtered().length - shownCount()} more`}</Text>
          </Button></If>
      </Section>
      <View class="mt-1.5 px-4"><Text class="text-caption text-muted-foreground">CPU 100% is one core. Memory is resident size and may count shared pages twice.</Text></View>
    </View>
  );
}

function Chip(props: { label: string; selected: boolean; accessibilityLabel?: string; testID?: string; onPress: () => void }) {
  return (
    <Button
      testID={props.testID}
      accessibilityLabel={props.accessibilityLabel}
      selected={props.selected}
      onPress={props.onPress}
      class={props.selected ? "h-9 flex-row items-center gap-1.5 rounded-full bg-primary px-3.5" : "h-9 flex-row items-center gap-1.5 rounded-full bg-card px-3.5 active:bg-accent"}
    >
      <Text class={props.selected ? "text-sub font-semibold text-primary-foreground line-clamp-1" : "text-sub font-semibold line-clamp-1"}>{props.label}</Text>
    </Button>
  );
}

/** A consumer's share of the largest one by the chosen sort: the ranked row's bar. */
function consumerShare(c: ResourceConsumer, all: readonly ResourceConsumer[]): number {
  const value = (x: ResourceConsumer) => (sort() === "cpu" ? x.cpuPercent ?? 0 : x.rssBytes ?? 0);
  const max = Math.max(0, ...all.map(value));
  return max > 0 ? value(c) / max : 0;
}

function ConsumerRow(props: { consumer: ResourceConsumer; rowKey: string; wide: boolean; share: number }) {
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
        <View class="flex-row items-center gap-3.5 px-4 py-3">
          <RowTile lead={consumerRowLead(c())} testID="consumer-lead" />
          <View class="min-w-0 flex-1">
            <Text class="text-body line-clamp-1">{title()}</Text>
            <If when={consumerRowMeta(c())}><Text testID="consumer-meta" class="text-sub text-muted-foreground line-clamp-1" style={orphaned() ? { color: color("warning") } : undefined}>{consumerRowMeta(c())}</Text></If>
          </View>
          <Text class="w-[52px] text-right font-mono text-code" style={{ fontVariant: ["tabular-nums"] }}>{formatPercent(c().cpuPercent)}</Text>
          <Text class="w-[64px] text-right font-mono text-code" style={{ fontVariant: ["tabular-nums"] }}>{formatBytes(c().rssBytes)}</Text>
          <If when={props.wide}><Text class="w-[44px] text-right font-mono text-code" style={{ fontVariant: ["tabular-nums"] }}>{String(c().processCount)}</Text></If>
          <If when={props.wide}><View class="w-[72px]"><Meter share={props.share} /></View></If>
          <Icon name={expandedNow() ? "chevron-up" : "chevron-down"} size={14} tone="faint" />
        </View>
      </View>
      <If when={expandedNow()}><View class="gap-1 bg-secondary px-4 py-3">
          {/* A long press copies the title, the IDs or the commands, as selecting them did. */}
          <View testID="consumer-details" class="gap-1" onLongPress={() => void copyMenu(title(), consumerCopyChoices(c(), title()))}>
            <Text class="text-body font-semibold">{title()}</Text>
            <Text class="text-sub">{`${CONSUMER_OWNER_LABEL[c().owner]} · ${c().processCount} ${c().processCount === 1 ? "process" : "processes"}`}</Text>
            <If when={c().id}><Fact label="ID" value={c().id!} /></If>
            <If when={c().workerId}><Fact label="Worker" value={c().workerId!} /></If>
            <If when={c().workId}><Fact label="Work" value={c().workId!} /></If>
            <If when={consumerMeta(c())}><Text class="text-sub text-muted-foreground">{consumerMeta(c())}</Text></If>
            <If when={consumerCommands(c()).length}><Text class="text-sub text-muted-foreground">{`Commands · ${consumerCommands(c()).join(", ")}`}</Text></If>
            <If when={kinds()}><Text class="text-sub text-muted-foreground">{`Kinds · ${kinds()}`}</Text></If>
          </View>
          <If when={c().processes.length} else={<Text class="text-sub text-muted-foreground">Process details unavailable in this sample.</Text>}><View class="gap-0.5 pt-1">
              <Text class="text-caption text-muted-foreground">{`${c().processes.length} / ${c().processCount} processes reported · largest memory`}</Text>
              <View class="flex-row gap-2 pt-1">
                <Text class="w-[64px] text-caption text-muted-foreground">PID</Text>
                <Text class="flex-1 text-caption text-muted-foreground">Command / start token</Text>
                <Text class="text-caption text-muted-foreground">Memory</Text>
              </View>
              {processesByMemory(c()).map((p) => (
                <View testID={`process-${p.pid ?? "unknown"}`} class="flex-row gap-2 py-0.5" onLongPress={() => void copyMenu(p.pid ? `PID ${p.pid}` : p.command ?? "Unknown process", processCopyChoices(p))}>
                  <Text class="w-[64px] font-mono text-code">{p.pid ? String(p.pid) : "—"}</Text>
                  <View class="min-w-0 flex-1 gap-0.5">
                    <Text class="font-mono text-code line-clamp-2">{p.command ?? "Unknown process"}</Text>
                    <If when={p.start}>{(startValue) => <Text class="font-mono text-code text-muted-foreground">{`start ${startValue()}`}</Text>}</If>
                  </View>
                  <Text class="font-mono text-code">{formatBytes(p.rssBytes)}</Text>
                </View>
              ))}
            </View></If>
        </View></If>
      <View class="absolute bottom-0 right-0 h-px bg-border" style={{ left: 60 }} />
    </View>
  );
}
