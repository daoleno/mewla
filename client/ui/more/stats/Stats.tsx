import { announce, Canvas, Clipboard, effect, Path2D, signal, Text, untracked, View } from "nsh";
import { Button, NavPage } from "nsh/base";
import { dateKey, formatDate, formatFullDateTime } from "../../../core/more/dates";
import { failureText } from "../../../core/more/daemon";
import { codexPlanLabel, codexRemainingPercent, codexWindowLabel, normalizeCodexUsedPercent } from "../../../core/stats/codexSubscriptionStats";
import {
  activityReadout,
  activityUsesTokens,
  activityValue,
  chartDate,
  costSourceLabel,
  dayFacts,
  emptyRangeTitle,
  formatAvailableCost,
  formatAvailableTokens,
  modelFacts,
  modelNotes,
  pricingLine,
  resetLabel,
  rowActivitySummary,
  sessionSummary,
  shortDay,
  sortByActivity,
} from "../../../core/stats/format";
import {
  accessibleDate,
  ACTIVITY_ACCESSIBILITY_ACTIONS,
  ACTIVITY_ACCESSIBILITY_HINT,
  activityAccessibilityValue,
  activityAction,
  codexWindowAccessibilityLabel,
  dayToOpen,
  MODEL_ROW_HINT,
  modelAccessibilityLabel,
  SCRUB_ACTIVE_X,
  SCRUB_FAIL_Y,
  scrubPhase,
  swipedRange,
  type ScrubPhase,
} from "../../../core/stats/interaction";
import { loadStats } from "../../../core/stats/load";
import { activityAreaPath, activityChartPoints, activityCurvePath, activityIndexAt, buildDailyActivitySeries } from "../../../core/stats/statsActivity";
import { costEstimateMark, statsModelHeading, statsModelKey } from "../../../core/stats/statsCostPresentation";
import { EMPTY_RANGE, hasRangeStats, isCostKnown, type CodexSubscriptionUsage, type DayCell, type ModelStat, type PricingStatus, type RangeData, type StatsView } from "../../../core/stats/statsPayload";
import { INITIAL_STATS_RANGE, STATS_RANGE_TABS, type StatsRange } from "../../../core/stats/statsTabs";
import { color, withAlpha } from "../kit/colors";
import { Icon } from "../kit/Icon";
import { copyMenu, EmptyState, Meter, Page, Row, Section, ShowMore, Tag } from "../kit/kit";
import { Segmented } from "../kit/Segmented";
import { daemon, daemonState } from "../daemon";
import type { PageNav } from "../routes";

const MAX_LIST_ITEMS = 5;

/** What Stats holds for the current computer: dropped when the computer changes. */
const view = signal<StatsView | null>(null);
const viewFor = signal("");
const loading = signal(false);
const failure = signal("");
const range = signal<StatsRange>(INITIAL_STATS_RANGE);
/** Which long cards are open, shared by the ranges. */
const expanded = signal<ReadonlySet<string>>(new Set());

let generation = 0;
/** A finger is on the activity chart: a swipe it makes scrubs the chart, never changes the range. */
let chartTouched = false;
let swipeFromChart = false;

function reload(): void {
  const current = daemon();
  if (!current || current.state !== "connected") return;
  const mine = ++generation;
  loading.set(true);
  loadStats(current, { stillWanted: () => mine === generation })
    .then((next) => {
      if (mine !== generation) return;
      view.set(next);
      viewFor.set(current.server.id);
      failure.set("");
    })
    .catch((error: unknown) => mine === generation && failure.set(failureText(error, "Stats didn't load")))
    .finally(() => mine === generation && loading.set(false));
}

/** One computer at a time: another computer drops the last one's stats and any late answer for them. */
function forgetOtherComputer(serverId: string): void {
  if (serverId === untracked(viewFor)) return;
  generation++;
  view.set(null);
  viewFor.set("");
  failure.set("");
  loading.set(false);
}

function toggle(section: string): void {
  const next = new Set(expanded());
  if (next.has(section)) next.delete(section);
  else next.add(section);
  expanded.set(next);
}

function shown<T>(section: string, items: readonly T[]): readonly T[] {
  return expanded().has(section) ? items : items.slice(0, MAX_LIST_ITEMS);
}

/** A detail page shows one computer's numbers: switching computer closes it, once. */
function leaveOnSwitch(nav: PageNav): void {
  const openedFor = untracked(() => daemon())?.server.id;
  let left = false;
  effect(() => {
    const id = daemon()?.server.id;
    if (left || id === openedFor) return;
    left = true;
    untracked(() => nav.pop());
  });
}

export function Stats(props: { nav: PageNav }) {
  // Each visit starts on Week, as the range control always has.
  range.set(INITIAL_STATS_RANGE);
  // Load on open and whenever the computer (re)connects.
  effect(() => {
    const current = daemon();
    forgetOtherComputer(current?.server.id ?? "");
    if (current && daemonState() === "connected") untracked(reload);
  });
  const data = (): RangeData => view()?.ranges[range()] ?? EMPTY_RANGE;
  const anyStats = () => Object.values(view()?.ranges ?? {}).some((r) => hasRangeStats(r));
  const subscriptions = () => view()?.codexSubscriptions ?? [];
  const busy = () => !view() && (loading() || daemonState() === "connecting");
  return (
    <Page title="Stats" testID="stats" refreshing={loading() && Boolean(view())} onRefresh={reload}>
      <View class="mt-2">
        <Segmented testID="stats-range" accessibilityLabel="Stats range" options={STATS_RANGE_TABS} value={range()} onChange={(key) => range.set(key)} />
      </View>
      {/* A swipe across the page moves to the next or previous range, as the tabs do. */}
      <View
        testID="stats-body"
        class="min-h-[420px]"
        panActivation={{ activeOffsetX: 24, failOffsetY: 16 }}
        onPanStart={() => {
          swipeFromChart = chartTouched;
        }}
        onPanEnd={(e) => {
          const fromChart = swipeFromChart || chartTouched;
          swipeFromChart = false;
          if (fromChart || e.cancelled) return;
          const next = swipedRange(range(), e.translation.x, e.translation.y);
          if (next) range.set(next);
        }}
      >
        {busy() ? (
          <EmptyState busy testID="stats-loading" />
        ) : !view() && failure() ? (
          <EmptyState testID="stats-failed" icon="alert" tone="failed" title="Stats didn't load" detail={failure()} action="Try again" onAction={reload} />
        ) : !hasRangeStats(data()) && subscriptions().length === 0 ? (
          <EmptyState testID="stats-empty" icon="stats" title={emptyRangeTitle(range(), anyStats())} detail={latestActivity(view())} />
        ) : (
          <View>
            {subscriptions().map((s) => <CodexCard subscription={s} />)}
            {hasRangeStats(data()) ? <Summary data={data()} pricing={view()?.pricing} /> : null}
            <Models data={data()} pricing={view()?.pricing} nav={props.nav} />
            <Projects data={data()} />
            {data().days.length > 0 ? <Activity days={range() === "day" ? view()?.ranges.all?.days ?? data().days : data().days} range={range()} nav={props.nav} /> : null}
            <Counted section="skills" title="Skills" items={data().skills.map((s) => ({ name: s.name, calls: s.calls, detail: s.projects.join(" · ") }))} />
            <Counted section="tools" title="Tools" items={data().tools.map((t) => ({ name: t.name, calls: t.calls, detail: "" }))} />
          </View>
        )}
      </View>
    </Page>
  );
}

function latestActivity(stats: StatsView | null): string | undefined {
  const days = stats?.ranges.all?.days ?? [];
  const last = days.length ? days[days.length - 1].date : "";
  return last ? `Latest activity: ${shortDay(last)}` : undefined;
}

function CodexCard(props: { subscription: CodexSubscriptionUsage }) {
  const s = () => props.subscription;
  const available = () => s().authKind === "official" && s().state === "available";
  return (
    <Section title="Codex subscription" testID="codex-card" detail={s().stale ? <Tag label="STALE" /> : undefined}>
      <Row title={codexPlanLabel(s().plan)} />
      {available() ? (
        <View>
          {(s().windows ?? []).map((w) => {
            const used = normalizeCodexUsedPercent(w.usedPercent);
            const remaining = codexRemainingPercent(used);
            return (
              <View class="gap-1.5 px-4 py-3" accessible accessibilityLabel={codexWindowAccessibilityLabel(codexWindowLabel(w.windowMinutes), remaining, resetLabel(w.resetsAt))}>
                <View class="flex-row justify-between">
                  <Text class="text-base">{codexWindowLabel(w.windowMinutes)}</Text>
                  <Text class="text-base text-muted-foreground" style={{ fontVariant: ["tabular-nums"] }}>{`${Math.round(remaining)}% left`}</Text>
                </View>
                <Meter share={used / 100} height={4} />
                <Text class="text-xs text-muted-foreground">{resetLabel(w.resetsAt)}</Text>
              </View>
            );
          })}
          {s().stale ? <Text class="px-4 pb-3 text-sm text-muted-foreground">Live usage is temporarily unavailable. Showing the last successful update.</Text> : null}
        </View>
      ) : (
        <Text class="px-4 py-3 text-sm text-muted-foreground">{codexNotice(s())}</Text>
      )}
    </Section>
  );
}

function codexNotice(s: CodexSubscriptionUsage): string {
  if (s.authKind === "api_key") return "Subscription limits are only available when Codex is signed in with ChatGPT.";
  if (s.authKind === "official") return "Codex usage is temporarily unavailable. Existing activity stats are unaffected.";
  if (s.authKind === "absent") return "Sign in to the Codex CLI with ChatGPT to see subscription limits.";
  return "Mewla could not confidently identify official Codex subscription authentication.";
}

function Summary(props: { data: RangeData; pricing?: PricingStatus }) {
  // A figure that is not known and adds up to nothing is drawn muted.
  const unknownCost = () => props.data.costKnown === false && props.data.cost === 0;
  const unknownTokens = () => props.data.totalTokensKnown === false && props.data.totalTokens === 0;
  return (
    <Section testID="stats-summary">
      <View class="flex-row px-4 pt-4">
        <View class="flex-1">
          <Text class="text-xs text-muted-foreground">{`Cost · ${costSourceLabel(props.data.costProvenance)}`}</Text>
          <Text testID="stats-cost" class={unknownCost() ? "mt-1 text-3xl font-bold text-muted-foreground" : "mt-1 text-3xl font-bold"} style={{ fontVariant: ["tabular-nums"] }}>
            {formatAvailableCost(props.data.cost, props.data.costKnown)}
          </Text>
        </View>
        <View class="flex-1">
          <Text class="text-xs text-muted-foreground">Tokens</Text>
          <Text testID="stats-tokens" class={unknownTokens() ? "mt-1 text-3xl font-bold text-muted-foreground" : "mt-1 text-3xl font-bold"} style={{ fontVariant: ["tabular-nums"] }}>
            {formatAvailableTokens(props.data.totalTokens, props.data.totalTokensKnown)}
          </Text>
        </View>
      </View>
      <View class="gap-0.5 px-4 pb-4 pt-2">
        <Text class="text-sm text-muted-foreground">{sessionSummary(props.data.sessions)}</Text>
        {props.pricing ? <Text class="text-xs text-muted-foreground">{pricingLine(props.pricing, formatDate)}</Text> : null}
      </View>
    </Section>
  );
}

function Models(props: { data: RangeData; pricing?: PricingStatus; nav: PageNav }) {
  const models = () => sortByActivity(props.data.models);
  return (
    <>
      {models().length === 0 ? null : (
        <Section title="Models" testID="stats-models">
          {shown("models", models()).map((m: ModelStat) => (
            <Row
              testID={`model-${statsModelKey(m)}`}
              title={statsModelHeading(m, models())}
              subtitle={rowActivitySummary(m)}
              value={`${costEstimateMark(m.cost, m.costKnown, m.costProvenance)}${formatAvailableCost(m.cost, m.costKnown)}`}
              chevron
              accessibilityLabel={modelAccessibilityLabel(m)}
              accessibilityHint={MODEL_ROW_HINT}
              onPress={() => props.nav.push({ name: "stats-model", model: m, pricing: props.pricing })}
            />
          ))}
          {models().length > MAX_LIST_ITEMS ? <ShowMore testID="more-models" hidden={models().length - MAX_LIST_ITEMS} expanded={expanded().has("models")} onToggle={() => toggle("models")} /> : null}
        </Section>
      )}
    </>
  );
}

function Projects(props: { data: RangeData }) {
  const projects = () => sortByActivity(props.data.projects);
  const usesTokens = () => activityUsesTokens(projects());
  const max = () => Math.max(1, ...projects().map((p) => activityValue(p, usesTokens())));
  return (
    <>
      {projects().length === 0 ? null : (
        <Section title="Projects" testID="stats-projects">
          {shown("projects", projects()).map((p) => (
            <View class="gap-1.5 px-4 py-3">
              <View class="flex-row items-baseline gap-3">
                <Text class="flex-1 text-base line-clamp-1">{p.name}</Text>
                <Text class="text-base text-muted-foreground" style={{ fontVariant: ["tabular-nums"] }}>{formatAvailableCost(p.cost, p.costKnown)}</Text>
              </View>
              <Text class="text-sm text-muted-foreground line-clamp-1">{`${rowActivitySummary(p)} · ${costSourceLabel(p.costProvenance)}`}</Text>
              <Meter share={activityValue(p, usesTokens()) / max()} />
            </View>
          ))}
          {projects().length > MAX_LIST_ITEMS ? <ShowMore hidden={projects().length - MAX_LIST_ITEMS} expanded={expanded().has("projects")} onToggle={() => toggle("projects")} /> : null}
        </Section>
      )}
    </>
  );
}

function Counted(props: { section: string; title: string; items: Array<{ name: string; calls: number; detail: string }> }) {
  const total = () => props.items.reduce((sum, item) => sum + item.calls, 0);
  const max = () => Math.max(1, ...props.items.map((i) => i.calls));
  return (
    <>
      {props.items.length === 0 || (props.section === "tools" && total() === 0) ? null : (
        <Section title={props.title} testID={`stats-${props.section}`} detail={<Text class="text-sm text-muted-foreground">{`${total()} calls`}</Text>}>
          {shown(props.section, props.items).map((item) => (
            <View class="gap-1.5 px-4 py-3">
              <View class="flex-row items-baseline gap-3">
                <Text class="flex-1 text-base line-clamp-1">{item.name}</Text>
                <Text class="text-base text-muted-foreground" style={{ fontVariant: ["tabular-nums"] }}>{String(item.calls)}</Text>
              </View>
              {item.detail ? <Text class="text-sm text-muted-foreground line-clamp-1">{item.detail}</Text> : null}
              <Meter share={item.calls / max()} />
            </View>
          ))}
          {props.items.length > MAX_LIST_ITEMS ? <ShowMore hidden={props.items.length - MAX_LIST_ITEMS} expanded={expanded().has(props.section)} onToggle={() => toggle(props.section)} /> : null}
        </Section>
      )}
    </>
  );
}

const CHART_HEIGHT = 120;
const CHART_INSET = 5;

/**
 * Daily activity as one ink curve. The readout names the last day; a finger dragged
 * across the chart moves it day by day and lifting resets it, opening nothing; a tap
 * opens the tapped day when it had activity. A screen reader steps between days and
 * opens one with the chart's actions.
 */
function Activity(props: { days: readonly DayCell[]; range: StatsRange; nav: PageNav }) {
  const width = signal(0);
  const focus = signal<number | null>(null);
  let touch: { x: number; y: number; phase: ScrubPhase } | null = null;
  const series = () => buildDailyActivitySeries(props.days, props.range);
  const usesTokens = () => activityUsesTokens(series().flatMap((d) => (d.day ? [d.day] : [])));
  const values = () => series().map((d) => (d.day ? activityValue(d.day, usesTokens()) : 0));
  const points = () => activityChartPoints(values(), { width: width(), height: CHART_HEIGHT, inset: CHART_INSET });
  const last = () => Math.max(0, series().length - 1);
  const index = () => Math.min(focus() ?? last(), last());
  const active = () => series()[index()];
  const at = (x: number) => (points().length ? activityIndexAt(x, points()) : 0);
  const today = dateKey(new Date());
  const open = (day: DayCell | null) => {
    if (day) props.nav.push({ name: "stats-day", day });
  };
  const endTouch = () => {
    chartTouched = false;
    if (!touch) return;
    touch = null;
    focus.set(null);
  };
  return (
    <Section title="Activity" testID="stats-activity">
      <View class="px-4 pt-3">
        <Text class="text-xs text-muted-foreground">{active()?.date === today ? "Today" : chartDate(active()?.date ?? "")}</Text>
        <Text testID="activity-readout" class="text-lg font-semibold" style={{ fontVariant: ["tabular-nums"] }}>{activityReadout(active()?.day ?? null, usesTokens())}</Text>
      </View>
      <View
        testID="activity-chart"
        class="mx-4 mt-2"
        style={{ height: CHART_HEIGHT }}
        role="adjustable"
        accessibilityLabel="Daily activity"
        accessibilityValue={{ text: activityAccessibilityValue(active()?.date ?? "", active()?.day ?? null) }}
        accessibilityHint={ACTIVITY_ACCESSIBILITY_HINT}
        accessibilityActions={ACTIVITY_ACCESSIBILITY_ACTIONS}
        onAccessibilityAction={({ actionName }) => {
          const step = activityAction(actionName, index(), series());
          if (!step) return;
          if ("focus" in step) focus.set(step.focus);
          else open(step.open);
        }}
        panActivation={{ activeOffsetX: SCRUB_ACTIVE_X, failOffsetY: SCRUB_FAIL_Y }}
        onLayout={({ bounds }) => width.set(bounds.size.width)}
        onTouchStart={(e) => {
          chartTouched = true;
          touch = { x: e.pageX, y: e.pageY, phase: "pending" };
          focus.set(at(e.locationX));
        }}
        onTouchMove={(e) => {
          if (!touch) return;
          touch.phase = scrubPhase(touch.phase, e.pageX - touch.x, e.pageY - touch.y);
          // A page scroll that began on the chart lets the chart go.
          if (touch.phase === "scrub") focus.set(at(e.locationX));
          else if (touch.phase === "scroll") focus.set(null);
        }}
        onTouchEnd={endTouch}
        onTouchCancel={endTouch}
        onPress={(e) => open(dayToOpen(series(), at(e.position.x)))}
      >
        <Canvas
          style={{ width: "100%", height: CHART_HEIGHT }}
          accessibilityElementsHidden
          draw={(cx, size) => {
            const pts = activityChartPoints(values(), { width: size.width, height: CHART_HEIGHT, inset: CHART_INSET });
            if (!pts.length) return;
            const baseline = CHART_HEIGHT - CHART_INSET;
            const ink = color("ink");
            const wash = cx.createLinearGradient(0, 0, 0, CHART_HEIGHT);
            wash.addColorStop(0, withAlpha(ink, 0.14));
            wash.addColorStop(1, withAlpha(ink, 0));
            cx.fillStyle = wash;
            cx.fill(new Path2D(activityAreaPath(pts, baseline)));
            cx.strokeStyle = color("hairline");
            cx.lineWidth = 1;
            cx.beginPath();
            cx.moveTo(0, baseline);
            cx.lineTo(size.width, baseline);
            cx.stroke();
            cx.strokeStyle = ink;
            cx.lineWidth = pts.length > 1 && pts[1].x - pts[0].x < 3 ? 1 : 1.75;
            cx.lineCap = "round";
            cx.lineJoin = "round";
            cx.stroke(new Path2D(activityCurvePath(pts)));
            const dot = pts[index()];
            if (!dot) return;
            // While a finger or a screen reader moves through the days, a hairline marks the day.
            if (focus() !== null) {
              cx.strokeStyle = color("faint");
              cx.lineWidth = 0.5;
              cx.beginPath();
              cx.moveTo(dot.x, 0);
              cx.lineTo(dot.x, CHART_HEIGHT);
              cx.stroke();
            }
            cx.fillStyle = ink;
            cx.strokeStyle = color("paper");
            cx.lineWidth = 1.5;
            cx.beginPath();
            cx.arc(dot.x, dot.y, 3.5, 0, Math.PI * 2);
            cx.fill();
            cx.stroke();
          }}
        />
      </View>
      <View class="flex-row justify-between px-4 pb-3 pt-1.5">
        <Text class="text-xs text-muted-foreground">{chartDate(series()[0]?.date ?? "")}</Text>
        <Text class="text-xs text-muted-foreground">{chartDate(series()[series().length - 1]?.date ?? "")}</Text>
      </View>
    </Section>
  );
}

/** One model's usage and price, in full. The id, each fact and each note can be copied. */
export function StatsModel(props: { model: ModelStat; pricing?: PricingStatus; nav: PageNav }) {
  leaveOnSwitch(props.nav);
  const id = props.model.id || props.model.name;
  const copied = signal(false);
  const copyId = () =>
    void Clipboard.writeText(id)
      .then(() => {
        copied.set(true);
        announce("Copied");
        setTimeout(() => copied.set(false), 1_600);
      })
      .catch(() => {});
  return (
    <Page
      title={id}
      testID="stats-model"
      headerRight={
        <Button testID="copy-model-id" accessibilityLabel="Copy model ID" onPress={copyId} class="p-2 active:opacity-60">
          <Icon name={copied() ? "check" : "copy"} size={18} />
        </Button>
      }
    >
      <Section>
        {modelFacts(props.model, formatFullDateTime).map(([label, value]) => (
          <Row testID={`model-fact-${label}`} title={label} value={value} onLongPress={() => void copyMenu(label, [{ label: "Copy", value }])} />
        ))}
      </Section>
      {modelNotes(props.model, props.pricing).map((note) => (
        <View role="text" accessibilityLabel={note} onLongPress={() => void copyMenu(note, [{ label: "Copy", value: note }])}>
          <Text class="mt-3 px-4 text-sm text-muted-foreground">{note}</Text>
        </View>
      ))}
    </Page>
  );
}

/** One day's totals. */
export function StatsDay(props: { day: DayCell; nav: PageNav }) {
  leaveOnSwitch(props.nav);
  // The cost stands out when there is one to read.
  const costShown = isCostKnown(props.day) || props.day.cost > 0;
  return (
    <NavPage title={chartDate(props.day.date)} testID="stats-day">
      <View class="flex-1 bg-background px-4 pt-2">
        <Text class="px-4 pt-4 text-sm text-muted-foreground">{accessibleDate(props.day.date)}</Text>
        <Section>
          {dayFacts(props.day).map(([label, value]) =>
            label === "Cost" && costShown ? (
              <Row title={label} trailing={<Text testID="day-cost" class="text-base font-semibold" style={{ fontVariant: ["tabular-nums"] }}>{value}</Text>} />
            ) : (
              <Row title={label} value={value} />
            ),
          )}
        </Section>
      </View>
    </NavPage>
  );
}
