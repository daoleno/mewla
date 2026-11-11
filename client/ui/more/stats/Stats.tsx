import { Canvas, effect, Path2D, signal, Text, untracked, View } from "nsh";
import { NavPage } from "../../shell/NavPage";
import { formatDate, formatFullDateTime, formatLongDay, parseDateOnly } from "../../../core/more/dates";
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
import { loadStats } from "../../../core/stats/load";
import { activityAreaPath, activityChartPoints, activityCurvePath, activityIndexAt, buildDailyActivitySeries } from "../../../core/stats/statsActivity";
import { costEstimateMark, statsModelHeading, statsModelKey } from "../../../core/stats/statsCostPresentation";
import { EMPTY_RANGE, hasRangeStats, type CodexSubscriptionUsage, type DayCell, type ModelStat, type PricingStatus, type RangeData, type StatsView } from "../../../core/stats/statsPayload";
import { INITIAL_STATS_RANGE, STATS_RANGE_TABS, type StatsRange } from "../../../core/stats/statsTabs";
import { color, withAlpha } from "../kit/colors";
import { EmptyState, Meter, Page, Row, Section, ShowMore, Tag } from "../kit/kit";
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

export function Stats(props: { nav: PageNav }) {
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
      {busy() ? (
        <EmptyState busy testID="stats-loading" />
      ) : !view() && failure() ? (
        <EmptyState testID="stats-failed" icon="alert" title="Stats didn't load" detail={failure()} action="Try again" onAction={reload} />
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
            return (
              <View class="gap-1.5 px-4 py-3">
                <View class="flex-row justify-between">
                  <Text class="text-base">{codexWindowLabel(w.windowMinutes)}</Text>
                  <Text class="text-base text-muted-foreground" style={{ fontVariant: ["tabular-nums"] }}>{`${Math.round(codexRemainingPercent(used))}% left`}</Text>
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
  const unknownCost = () => props.data.costKnown === false && props.data.cost === 0;
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
          <Text testID="stats-tokens" class="mt-1 text-3xl font-bold" style={{ fontVariant: ["tabular-nums"] }}>{formatAvailableTokens(props.data.totalTokens, props.data.totalTokensKnown)}</Text>
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
  return models().length === 0 ? null : (
    <Section title="Models" testID="stats-models">
      {shown("models", models()).map((m: ModelStat) => (
        <Row
          testID={`model-${statsModelKey(m)}`}
          title={statsModelHeading(m, models())}
          subtitle={rowActivitySummary(m)}
          value={`${costEstimateMark(m.cost, m.costKnown, m.costProvenance)}${formatAvailableCost(m.cost, m.costKnown)}`}
          chevron
          onPress={() => props.nav.push({ name: "stats-model", model: m, pricing: props.pricing })}
        />
      ))}
      {models().length > MAX_LIST_ITEMS ? <ShowMore testID="more-models" hidden={models().length - MAX_LIST_ITEMS} expanded={expanded().has("models")} onToggle={() => toggle("models")} /> : null}
    </Section>
  );
}

function Projects(props: { data: RangeData }) {
  const projects = () => sortByActivity(props.data.projects);
  const usesTokens = () => activityUsesTokens(projects());
  const max = () => Math.max(1, ...projects().map((p) => activityValue(p, usesTokens())));
  return projects().length === 0 ? null : (
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
  );
}

function Counted(props: { section: string; title: string; items: Array<{ name: string; calls: number; detail: string }> }) {
  const total = () => props.items.reduce((sum, item) => sum + item.calls, 0);
  const max = () => Math.max(1, ...props.items.map((i) => i.calls));
  return props.items.length === 0 || (props.section === "tools" && total() === 0) ? null : (
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
  );
}

const CHART_HEIGHT = 120;
const CHART_INSET = 5;

/**
 * Daily activity as one ink curve. The readout names the last day; a finger on the
 * chart moves it to that day, and lifting it there opens the day.
 */
function Activity(props: { days: readonly DayCell[]; range: StatsRange; nav: PageNav }) {
  const width = signal(0);
  const focus = signal<number | null>(null);
  const series = () => buildDailyActivitySeries(props.days, props.range);
  const usesTokens = () => activityUsesTokens(series().flatMap((d) => (d.day ? [d.day] : [])));
  const values = () => series().map((d) => (d.day ? activityValue(d.day, usesTokens()) : 0));
  const points = () => activityChartPoints(values(), { width: width(), height: CHART_HEIGHT, inset: CHART_INSET });
  const index = () => focus() ?? Math.max(0, series().length - 1);
  const active = () => series()[index()];
  const at = (x: number) => (points().length ? activityIndexAt(x, points()) : 0);
  const todayKey = () => series()[series().length - 1]?.date;
  return (
    <Section title="Activity" testID="stats-activity">
      <View class="px-4 pt-3">
        <Text class="text-xs text-muted-foreground">{active()?.date === todayKey() && focus() === null ? "Today" : chartDate(active()?.date ?? "")}</Text>
        <Text testID="activity-readout" class="text-lg font-semibold" style={{ fontVariant: ["tabular-nums"] }}>{activityReadout(active()?.day ?? null, usesTokens())}</Text>
      </View>
      <View
        class="mx-4 mt-2"
        style={{ height: CHART_HEIGHT }}
        role="adjustable"
        accessibilityLabel="Daily activity"
        accessibilityValue={{ text: `${formatLongDay(parseDateOnly(active()?.date ?? "") ?? 0)}, ${activityReadout(active()?.day ?? null, usesTokens())}` }}
        onLayout={({ bounds }) => width.set(bounds.size.width)}
        onTouchStart={(e) => focus.set(at(e.locationX))}
        onTouchMove={(e) => focus.set(at(e.locationX))}
        onTouchEnd={(e) => {
          const day = series()[at(e.locationX)];
          focus.set(null);
          props.nav.push({ name: "stats-day", day: day?.day ?? emptyDay(day?.date ?? "") });
        }}
      >
        <Canvas
          style={{ width: "100%", height: CHART_HEIGHT }}
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
            if (dot) {
              cx.fillStyle = ink;
              cx.beginPath();
              cx.arc(dot.x, dot.y, 3.5, 0, Math.PI * 2);
              cx.fill();
            }
          }}
        />
      </View>
      <View class="flex-row justify-between px-4 pb-3 pt-1.5">
        <Text class="text-xs text-muted-foreground">{chartDate(series()[0]?.date ?? "")}</Text>
        <Text class="text-xs text-muted-foreground">{chartDate(todayKey() ?? "")}</Text>
      </View>
    </Section>
  );
}

function emptyDay(date: string): DayCell {
  return { date, totalTokens: 0, inputTokens: 0, outputTokens: 0, reasoningTokens: 0, cacheRead: 0, cacheCreate: 0, cost: 0, sessions: 0 };
}

/** One model's usage and price, in full. */
export function StatsModel(props: { model: ModelStat; pricing?: PricingStatus }) {
  return (
    <Page title={props.model.id || props.model.name} testID="stats-model">
      <Section>
        {modelFacts(props.model, formatFullDateTime).map(([label, value]) => <Row title={label} value={value} />)}
      </Section>
      {modelNotes(props.model, props.pricing).map((note) => <Text class="mt-3 px-4 text-sm text-muted-foreground">{note}</Text>)}
    </Page>
  );
}

/** One day's totals. */
export function StatsDay(props: { day: DayCell }) {
  return (
    <NavPage title={chartDate(props.day.date)} testID="stats-day">
      <View class="flex-1 bg-background px-4 pt-2">
        <Text class="px-4 pt-4 text-sm text-muted-foreground">{formatLongDay(parseDateOnly(props.day.date) ?? 0)}</Text>
        <Section>
          {dayFacts(props.day).map(([label, value]) => <Row title={label} value={value} />)}
        </Section>
      </View>
    </NavPage>
  );
}
