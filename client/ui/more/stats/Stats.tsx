import { announce, Canvas, cleanup, Clipboard, effect, keymap, platform, signal, Text, untracked, View } from "nsh";
import { Button } from "nsh/base";
import { dateKey, formatDate, formatFullDateTime } from "../../../core/more/dates";
import { failureText } from "../../../core/more/daemon";
import { codexPlanLabel, codexRemainingPercent, codexWindowLabel, normalizeCodexUsedPercent } from "../../../core/stats/codexSubscriptionStats";
import {
  activityUsesTokens,
  activityValue,
  chartDate,
  chartReadout,
  chartSeries,
  chartValue,
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
import { activityBarIndexAt, activityBars, buildDailyActivitySeries } from "../../../core/stats/statsActivity";
import { costEstimateMark, statsModelHeading, statsModelKey } from "../../../core/stats/statsCostPresentation";
import { EMPTY_RANGE, hasRangeStats, isCostKnown, type CodexSubscriptionUsage, type DayCell, type ModelStat, type PricingStatus, type RangeData, type StatsView } from "../../../core/stats/statsPayload";
import { availableRanges, INITIAL_STATS_RANGE, rangeDays, rangeEyebrow, shownRange, type StatsRange } from "../../../core/stats/statsTabs";
import { color } from "../../kit/colors";
import { Icon } from "../../kit/Icon";
import { copyMenu, EmptyState, Meter, Page, RankedRow, Row, Section, ShowMore, StatGrid, StatTile, Tag } from "../../kit/kit";
import { Segmented } from "../../kit/Segmented";
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

/**
 * A pull to refresh. While the computer can't be reached no request goes, but the spinner the pull
 * started must still be told to stop: `refreshing` turns on and off again (it stayed on, 102/107).
 */
function pull(): void {
  const current = daemon();
  if (current && current.state === "connected") return reload();
  loading.set(true);
  setTimeout(() => loading.set(false), 300);
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

/**
 * A detail page shows one computer's numbers: another computer closes it, once. The
 * unbound moment between two daemons is not a switch, so the same computer bound
 * again (a changed address) keeps it.
 */
function leaveOnSwitch(nav: PageNav): void {
  const openedFor = untracked(() => daemon())?.server.id;
  let left = false;
  effect(() => {
    const id = daemon()?.server.id;
    if (left || id === undefined || id === openedFor) return;
    left = true;
    untracked(() => nav.pop());
  });
}

export function Stats(props: { nav: PageNav }) {
  const swipePaging = platform.os !== "web";
  // Each visit starts on 14 days (SPEC §15 Metrics); a computer without it shows the ranges it has.
  range.set(INITIAL_STATS_RANGE);
  const tabs = () => availableRanges(view()?.ranges);
  const picked = () => shownRange(range(), tabs());
  // Load on open and whenever the computer (re)connects.
  effect(() => {
    const current = daemon();
    forgetOtherComputer(current?.server.id ?? "");
    if (current && daemonState() === "connected") untracked(reload);
  });
  const data = (): RangeData => view()?.ranges[picked()] ?? EMPTY_RANGE;
  const anyStats = () => Object.values(view()?.ranges ?? {}).some((r) => hasRangeStats(r));
  const subscriptions = () => view()?.codexSubscriptions ?? [];
  const busy = () => !view() && (loading() || daemonState() === "connecting");
  // The headline answers "how much": the range's total cost, once there is one to read.
  const headline = () => (hasRangeStats(data()) ? formatAvailableCost(data().cost, data().costKnown) : "Stats");
  return (
    <Page display title="Stats" displayTitle={headline()} eyebrow={rangeEyebrow(picked())} testID="stats" refreshing={loading() && Boolean(view())} onRefresh={pull}>
      <View class="mt-2">
        <Segmented testID="stats-range" accessibilityLabel="Stats range" options={tabs()} value={picked()} onChange={(key) => range.set(key)} />
      </View>
      {/* A swipe across the page moves to the next or previous range, as the tabs do; on the web the tabs alone do (client/DESIGN.md). */}
      <View
        testID="stats-body"
        class="min-h-[420px]"
        panActivation={swipePaging ? { activeOffsetX: 24, failOffsetY: 16 } : undefined}
        onPanStart={swipePaging ? () => {
          swipeFromChart = chartTouched;
        } : undefined}
        onPanEnd={swipePaging ? (e) => {
          const fromChart = swipeFromChart || chartTouched;
          swipeFromChart = false;
          if (fromChart || e.cancelled) return;
          const next = swipedRange(picked(), e.translation.x, e.translation.y, tabs());
          if (next) range.set(next);
        } : undefined}
      >
        {busy() ? (
          <EmptyState busy testID="stats-loading" />
        ) : !view() && failure() ? (
          <EmptyState testID="stats-failed" icon="alert" tone="failed" title="Stats didn't load" detail={failure()} action="Try again" onAction={reload} />
        ) : !hasRangeStats(data()) && subscriptions().length === 0 ? (
          <EmptyState testID="stats-empty" icon="stats" title={emptyRangeTitle(rangeDays(picked()), anyStats())} detail={latestActivity(view())} />
        ) : (
          <View>
            {subscriptions().map((s) => <CodexCard subscription={s} />)}
            {hasRangeStats(data()) ? <Summary data={data()} pricing={view()?.pricing} /> : null}
            {data().days.length > 0 ? <Activity days={data().days} range={picked()} nav={props.nav} /> : null}
            <Models data={data()} pricing={view()?.pricing} nav={props.nav} />
            <Projects data={data()} />
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
                  <Text class="text-body">{codexWindowLabel(w.windowMinutes)}</Text>
                  <Text class="text-body text-muted-foreground" style={{ fontVariant: ["tabular-nums"] }}>{`${Math.round(remaining)}% left`}</Text>
                </View>
                <Meter share={used / 100} height={4} />
                <Text class="text-caption text-muted-foreground">{resetLabel(w.resetsAt)}</Text>
              </View>
            );
          })}
          {s().stale ? <Text class="px-4 pb-3 text-sub text-muted-foreground">Live usage is temporarily unavailable. Showing the last successful update.</Text> : null}
        </View>
      ) : (
        <Text class="px-4 py-3 text-sub text-muted-foreground">{codexNotice(s())}</Text>
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

/** The range's figures as stat tiles: cost and where it comes from, tokens, sessions; the price list's date under them. */
function Summary(props: { data: RangeData; pricing?: PricingStatus }) {
  // A figure that is not known and adds up to nothing is drawn muted.
  const unknownCost = () => props.data.costKnown === false && props.data.cost === 0;
  const unknownTokens = () => props.data.totalTokensKnown === false && props.data.totalTokens === 0;
  return (
    <View>
      <StatGrid testID="stats-summary">
        <StatTile label="Cost" valueTestID="stats-cost" value={formatAvailableCost(props.data.cost, props.data.costKnown)} muted={unknownCost()} detail={costSourceLabel(props.data.costProvenance)} />
        <StatTile label="Tokens" valueTestID="stats-tokens" value={formatAvailableTokens(props.data.totalTokens, props.data.totalTokensKnown)} muted={unknownTokens()} />
        <StatTile label="Sessions" value={String(props.data.sessions)} />
      </StatGrid>
      {props.pricing ? <Text class="mt-2 px-4 text-caption text-muted-foreground">{pricingLine(props.pricing, formatDate)}</Text> : null}
    </View>
  );
}

function Models(props: { data: RangeData; pricing?: PricingStatus; nav: PageNav }) {
  const models = () => sortByActivity(props.data.models);
  const usesTokens = () => activityUsesTokens(models());
  const max = () => Math.max(1, ...models().map((m) => activityValue(m, usesTokens())));
  return (
    <>
      {models().length === 0 ? null : (
        <Section title="Models" testID="stats-models">
          {shown("models", models()).map((m: ModelStat) => (
            <RankedRow
              testID={`model-${statsModelKey(m)}`}
              icon="chip"
              title={statsModelHeading(m, models())}
              subtitle={rowActivitySummary(m)}
              share={activityValue(m, usesTokens()) / max()}
              value={`${costEstimateMark(m.cost, m.costKnown, m.costProvenance)}${formatAvailableCost(m.cost, m.costKnown)}`}
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
            <RankedRow icon="folder" title={p.name} subtitle={`${rowActivitySummary(p)} · ${costSourceLabel(p.costProvenance)}`} share={activityValue(p, usesTokens()) / max()} value={formatAvailableCost(p.cost, p.costKnown)} />
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
        <Section title={props.title} testID={`stats-${props.section}`} detail={<Text class="text-caption font-semibold text-muted-foreground" style={{ fontVariant: ["tabular-nums"] }}>{`${total()} calls`}</Text>}>
          {shown(props.section, props.items).map((item) => (
            <RankedRow icon={props.section === "skills" ? "skills" : "construct"} title={item.name} subtitle={item.detail || undefined} share={item.calls / max()} value={String(item.calls)} />
          ))}
          {props.items.length > MAX_LIST_ITEMS ? <ShowMore hidden={props.items.length - MAX_LIST_ITEMS} expanded={expanded().has(props.section)} onToggle={() => toggle(props.section)} /> : null}
        </Section>
      )}
    </>
  );
}

const CHART_HEIGHT = 120;
const BAR_GAP = 4;
const BAR_RADIUS = 4;

/**
 * The one chart (SPEC §15 Metrics): spend by day (activity, when no day has a known cost) as ink bars with rounded tops on a square baseline,
 * `hair` gridlines, dates at the start, middle and end. The tooltip over the chart names the last day; a
 * finger dragged across it, the pointer over it or ← → while it has the keyboard focus move it day by day,
 * and letting go resets it, opening nothing. A tap opens the tapped day when it had activity. A screen
 * reader steps between days and opens one with the chart's actions.
 */
function Activity(props: { days: readonly DayCell[]; range: StatsRange; nav: PageNav }) {
  const width = signal(0);
  const tipWidth = signal(0);
  const focus = signal<number | null>(null);
  let touch: { x: number; y: number; phase: ScrubPhase } | null = null;
  const series = () => buildDailyActivitySeries(props.days, props.range);
  const kind = () => chartSeries(series().map((d) => d.day));
  const values = () => series().map((d) => chartValue(d.day, kind()));
  const last = () => Math.max(0, series().length - 1);
  const index = () => Math.min(focus() ?? last(), last());
  const active = () => series()[index()];
  const at = (x: number) => activityBarIndexAt(x, series().length, { width: width(), gap: BAR_GAP });
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
  // ← → step through the days while the chart has the keyboard focus; Enter opens the day.
  let unbind: (() => void) | null = null;
  const step = (by: number) => focus.set(Math.min(last(), Math.max(0, (focus() ?? last()) + by)));
  const release = () => {
    unbind?.();
    unbind = null;
  };
  cleanup(release);
  // The tooltip sits over its day's bar, kept inside the chart.
  const tipLeft = () => {
    const bars = activityBars(values(), { width: width(), height: CHART_HEIGHT, gap: BAR_GAP });
    const bar = bars[index()];
    if (!bar) return 0;
    return Math.max(0, Math.min(width() - tipWidth(), bar.x + bar.width / 2 - tipWidth() / 2));
  };
  const axis = () => {
    const s = series();
    return [s[0]?.date ?? "", s[Math.floor((s.length - 1) / 2)]?.date ?? "", s[s.length - 1]?.date ?? ""].map(chartDate);
  };
  return (
    <Section title={kind() === "cost" ? "Spend" : "Activity"} testID="stats-activity">
      <View class="px-4 pt-3">
        <View class="h-[28px] flex-row">
          <View style={{ width: tipLeft() }} />
          <View testID="activity-tooltip" class="flex-row items-center gap-1.5 self-start rounded-full bg-primary px-2.5 py-1" onLayout={({ bounds }) => tipWidth.set(bounds.size.width)}>
            <Text class="text-caption text-primary-foreground">{`${active()?.date === today ? "Today" : chartDate(active()?.date ?? "")} ·`}</Text>
            <Text testID="activity-readout" class="text-caption font-semibold text-primary-foreground" style={{ fontVariant: ["tabular-nums"] }}>{chartReadout(active()?.day ?? null, kind())}</Text>
          </View>
        </View>
        <View
          testID="activity-chart"
          class="mt-2"
          style={{ height: CHART_HEIGHT }}
          role="adjustable"
          focusable
          accessibilityLabel={kind() === "cost" ? "Daily spend" : "Daily activity"}
          accessibilityValue={{ text: activityAccessibilityValue(active()?.date ?? "", active()?.day ?? null) }}
          accessibilityHint={ACTIVITY_ACCESSIBILITY_HINT}
          accessibilityActions={ACTIVITY_ACCESSIBILITY_ACTIONS}
          onAccessibilityAction={({ actionName }) => {
            const next = activityAction(actionName, index(), series());
            if (!next) return;
            if ("focus" in next) focus.set(next.focus);
            else open(next.open);
          }}
          onFocus={() => {
            release();
            unbind = keymap.bind({ left: () => step(-1), right: () => step(1), enter: () => open(active()?.day ?? null) }, { when: "notEditing" });
          }}
          onBlur={() => {
            release();
            focus.set(null);
          }}
          onMouseMove={(e) => focus.set(at(e.position.x))}
          onMouseLeave={() => focus.set(null)}
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
              const baseline = CHART_HEIGHT - 2;
              // Gridlines at the peak and half of it, under the bars.
              cx.fillStyle = color("hair");
              cx.fillRect(0, 0, size.width, 1);
              cx.fillRect(0, Math.round(baseline / 2), size.width, 1);
              const bars = activityBars(values(), { width: size.width, height: baseline - 4, gap: BAR_GAP });
              const chosen = focus();
              bars.forEach((bar, i) => {
                if (bar.height <= 0) return;
                // While a day is chosen, the others step back so it reads first.
                cx.fillStyle = chosen === null || chosen === i ? color("ink") : color("faint");
                cx.beginPath();
                const r = Math.min(BAR_RADIUS, bar.width / 2, bar.height);
                cx.roundRect(bar.x, baseline - bar.height, bar.width, bar.height, [r, r, 0, 0]);
                cx.fill();
              });
              cx.fillStyle = color("track");
              cx.fillRect(0, baseline, size.width, 2);
            }}
          />
        </View>
      </View>
      <View class="flex-row justify-between px-4 pb-3 pt-1.5">
        {axis().map((label) => <Text class="text-caption text-muted-foreground">{label}</Text>)}
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
      display
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
          <Text class="mt-3 px-4 text-sub text-muted-foreground">{note}</Text>
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
    <Page display title={chartDate(props.day.date)} eyebrow={accessibleDate(props.day.date)} testID="stats-day">
      <Section>
        {dayFacts(props.day).map(([label, value]) =>
          label === "Cost" && costShown ? (
            <Row title={label} trailing={<Text testID="day-cost" class="text-body font-semibold" style={{ fontVariant: ["tabular-nums"] }}>{value}</Text>} />
          ) : (
            <Row title={label} value={value} />
          ),
        )}
      </Section>
    </Page>
  );
}
