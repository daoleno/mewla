import { DateStyle, formatDate, formatNumber } from '../services/dateFormat';
import React, { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
import { unpricedReasonLabel } from '../services/unpricedReason';
import { costEstimateMark, statsModelHeading, statsModelKey } from '../services/statsCostPresentation';
import {
  Animated as NativeAnimated,
  type LayoutChangeEvent,
  ScrollView,
  StyleSheet,
  Text,
  useWindowDimensions,
  View,
} from 'react-native';
import { useFocusEffect } from 'expo-router';
import { SafeAreaView, useSafeAreaInsets } from 'react-native-safe-area-context';
import * as Haptics from 'expo-haptics';
import Animated, {
  useSharedValue,
  useAnimatedStyle,
  useReducedMotion,
  withTiming,
  interpolate,
} from 'react-native-reanimated';
import { TabView } from 'react-native-tab-view';
import { Gesture, GestureDetector } from 'react-native-gesture-handler';
import Svg, { Circle, Defs, Line, LinearGradient, Path, Stop } from 'react-native-svg';
import {
  ContentWidth,
  Radii,
  Spacing,
  TypeScale,
  UiTextMetrics,
  useAppTheme,
  type AppColors,
} from '../constants/tokens';
import { useWorkers } from '../store/workers';
import { useCurrentServer } from '../store/currentServer';
import { wsClient } from '../services/websocket';
import { failureMessage, warnOnce } from '../services/warnOnce';
import { AnimatedPressable } from '../components/ui/AnimatedPressable';
import { RisingSheet } from '../components/ui/RisingSheet';
import { EmptyState } from '../components/ui/EmptyState';
import { IconButton } from '../components/ui/IconButton';
import {
  codexAuthSummary,
  codexRemainingPercent,
  codexPlanLabel,
  codexSubscriptionVisibleForProviderState,
  codexWindowLabel,
  normalizeCodexUsedPercent,
  isOfficialCodexSubscription,
} from '../services/codexSubscriptionStats';
import {
  INITIAL_STATS_RANGE,
  STATS_RANGE_TABS,
  statsRangeAt,
  statsRangeIndex,
  type StatsRange,
} from '../services/statsTabs';
import {
  activityAreaPath,
  activityChartPoints,
  activityCurvePath,
  activityIndexAt,
  buildDailyActivitySeries,
  dateOnlyString,
  parseDateOnly,
  type ActivityDay,
} from '../services/statsActivity';
import {
  EMPTY_RANGE,
  hasRangeStats,
  isCostKnown,
  isTokenBreakdownKnown,
  isTotalTokensKnown,
  normalizeStatsPayload,
  type DayCell,
  type ModelStat,
  type PricingStatus,
  type ProjectStat,
  type RangeData,
  type StatsView,
} from '../services/statsPayload';


// ── Types (mirror daemon/stats/types.go) ───────────────────

type TimeRange = StatsRange;

// ── Constants ──────────────────────────────────────────────

const RANGE_OPTIONS = STATS_RANGE_TABS;
const STATS_TAB_ROUTES: Array<{ key: TimeRange; label: string }> = [
  ...STATS_RANGE_TABS,
];

const MAX_LIST_ITEMS = 5;
const EMPTY_STATS_RETRY_MS = 700;
const EMPTY_STATS_MAX_RETRIES = 3;
const STATS_CONTENT_MAX_WIDTH = ContentWidth.page;
const STATS_TAB_BAR_GAP = 4;
const STATS_TAB_BAR_PADDING = 3;

// ── Helpers ────────────────────────────────────────────────

function fmtCompact(n: number, units: readonly [number, string][]): string {
  const absolute = Math.abs(n);
  for (const [threshold, suffix] of units) {
    if (absolute >= threshold) {
      return `${(n / threshold).toFixed(1).replace(/\.0$/, '')}${suffix}`;
    }
  }
  return n.toString();
}

function fmt(n: number): string {
  return fmtCompact(n, [
    [1_000_000_000_000, 'T'],
    [1_000_000_000, 'B'],
    [1_000_000, 'M'],
    [1_000, 'K'],
  ]);
}

function fmtCost(n: number): string {
  if (Math.abs(n) >= 1_000) {
    return '$' + fmtCompact(n, [
      [1_000_000_000_000, 'T'],
      [1_000_000_000, 'B'],
      [1_000_000, 'M'],
      [1_000, 'K'],
    ]);
  }
  if (n >= 100) return '$' + n.toFixed(0);
  return '$' + n.toFixed(2);
}

function fmtAvailable(value: number, available: boolean | undefined, formatter: (n: number) => string): string {
  if (available === false) return value > 0 ? `${formatter(value)}+` : '—';
  return formatter(value);
}

function fmtAvailableCost(cost: number, costKnown?: boolean): string {
  return fmtAvailable(cost, costKnown, fmtCost);
}

function costSourceLabel(source?: 'reported' | 'estimated' | 'mixed' | 'unknown'): string {
  switch (source) {
    case 'reported': return 'Reported';
    case 'estimated': return 'Estimated';
    case 'mixed': return 'Mixed';
    case 'unknown': return 'Unavailable';
    default: return 'Estimated'; // Older daemons exposed estimate-only costs.
  }
}

function fmtAvailableTokens(tokens: number, totalTokensKnown?: boolean): string {
  return fmtAvailable(tokens, totalTokensKnown, fmt);
}

function sessionSummary(sessions: number): string {
  return `${sessions} ${sessions === 1 ? 'session' : 'sessions'}`;
}

function rowActivitySummary(item: {
  totalTokens: number;
  totalTokensKnown?: boolean;
  sessions: number;
}): string {
  if (!isTotalTokensKnown(item) && item.totalTokens === 0) return sessionSummary(item.sessions);
  return `${fmtAvailableTokens(item.totalTokens, item.totalTokensKnown)} tokens · ${sessionSummary(item.sessions)}`;
}

type ActivityStat = {
  name: string;
  totalTokens: number;
  totalTokensKnown?: boolean;
  sessions: number;
};

function activityUsesTokens(items: { totalTokens: number; totalTokensKnown?: boolean }[]): boolean {
  return items.every(item => isTotalTokensKnown(item) || item.totalTokens > 0);
}

function activityValue(
  item: { totalTokens: number; sessions: number },
  usesTokens: boolean,
): number {
  return usesTokens ? item.totalTokens : item.sessions;
}

function sortByActivity<T extends ActivityStat>(items: T[]): T[] {
  const usesTokens = activityUsesTokens(items);
  return [...items].sort((a, b) =>
    activityValue(b, usesTokens) - activityValue(a, usesTokens) || a.name.localeCompare(b.name),
  );
}

function shortDate(dateStr: string): string {
  // "2026-04-04" -> "4/4"
  const parts = dateStr.split('-');
  if (parts.length < 3) return dateStr;
  return `${parseInt(parts[1])}/${parseInt(parts[2])}`;
}

function accessibleDate(dateStr: string): string {
  const date = parseDateOnly(dateStr);
  if (!date) return dateStr;
  const weekdays = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
  const months = [
    'January', 'February', 'March', 'April', 'May', 'June',
    'July', 'August', 'September', 'October', 'November', 'December',
  ];
  return `${weekdays[date.getDay()]}, ${months[date.getMonth()]} ${date.getDate()}, ${date.getFullYear()}`;
}

function dayAccessibilityValue(day: DayCell): string {
  return [
    `${fmtAvailableTokens(day.totalTokens, day.totalTokensKnown)} tokens`,
    sessionSummary(day.sessions),
    `${fmtAvailableCost(day.cost, day.costKnown)} ${costSourceLabel(day.costProvenance).toLowerCase()} cost`,
  ].join(', ');
}

function topItems<T>(items: T[]): T[] {
  return items.slice(0, MAX_LIST_ITEMS);
}

function fmtReset(value?: string): string {
  if (!value) return 'Reset time unavailable';
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return 'Reset time unavailable';
  return `Resets ${formatDate(date, DateStyle.dateTime)}`;
}

// ── Component ──────────────────────────────────────────────

export default function StatsScreen() {
  const { colors, isLight } = useAppTheme();
  const s = useMemo(() => createStyles(colors, isLight), [colors, isLight]);
  const { width } = useWindowDimensions();
  const reducedMotion = useReducedMotion();
  const { state: agentsState } = useWorkers();
  const { currentServer, isCurrentServer } = useCurrentServer();
  const currentServerId = currentServer?.id ?? null;
  const [range, setRange] = useState<TimeRange>(INITIAL_STATS_RANGE);
  const [statsSnapshot, setStatsSnapshot] = useState<{ serverId: string; data: StatsView | null } | null>(null);
  const statsData = statsSnapshot?.serverId === currentServerId ? statsSnapshot.data : null;
  const [loading, setLoading] = useState(true);
  // A failed load is not "no stats yet": it keeps its own state and a retry.
  const [loadFailure, setLoadFailure] = useState<{ serverId: string; message: string } | null>(null);
  const [reloadToken, setReloadToken] = useState(0);
  const loadError = loadFailure?.serverId === currentServerId ? loadFailure.message : null;
  const [expandedSections, setExpandedSections] = useState<Set<string>>(new Set());
  const [daySelection, setDaySelection] = useState<{ serverId: string | null; day: DayCell } | null>(null);
  const [modelSelection, setModelSelection] = useState<{ serverId: string | null; range: TimeRange; key: string } | null>(null);
  const selectedModel = modelSelection?.serverId === currentServerId && modelSelection?.range === range
    ? statsData?.ranges[range]?.models.find(model => statsModelKey(model) === modelSelection.key) ?? null
    : null;
  useEffect(() => setModelSelection(null), [currentServerId, range]);
  const selectedDay = daySelection?.serverId === currentServerId ? daySelection?.day ?? null : null;
  const setSelectedDay = useCallback(
    (day: DayCell | null) => setDaySelection(day ? { serverId: currentServerId, day } : null),
    [currentServerId],
  );
  const [nestedHorizontalScrollActive, setNestedHorizontalScrollActive] = useState(false);
  const [rangeTabBarWidth, setRangeTabBarWidth] = useState(0);
  const statsSnapshotRef = useRef(statsSnapshot);

  useEffect(() => {
    statsSnapshotRef.current = statsSnapshot;
  }, [statsSnapshot]);

  const toggleSection = useCallback((section: string) => {
    setExpandedSections(prev => {
      const next = new Set(prev);
      if (next.has(section)) next.delete(section);
      else next.add(section);
      return next;
    });
  }, []);

  const connectionState = currentServerId ? agentsState.serverConnections[currentServerId] : undefined;
  const hasConnectingServer = connectionState === 'connecting';

  useFocusEffect(
    useCallback(() => {
      let cancelled = false;
      let retryTimer: ReturnType<typeof setTimeout> | null = null;
      const serverId = currentServerId;
      const loadStats = (attempt: number) => {
        retryTimer = null;
        const cached = statsSnapshotRef.current?.serverId === serverId ? statsSnapshotRef.current.data : null;
        if (!serverId || !isCurrentServer(serverId) || !wsClient.isConnected(serverId)) {
          setLoading(!cached && hasConnectingServer);
          return;
        }
        setLoading(!cached);
        void (async () => {
          const [providerSnapshot, result] = await Promise.all([
            // Without the provider list the Codex subscription card stays hidden; stats still load.
            wsClient.listProviders(serverId).catch((error: unknown) => { warnOnce('stats.providers', error); return null; }),
            wsClient.getStats(serverId).then(
              (payload) => ({ payload, failure: null as string | null }),
              (error: unknown) => ({ payload: null, failure: failureMessage(error) }),
            ),
          ]);
          if (cancelled || !isCurrentServer(serverId)) return;
          if (result.failure !== null) {
            // A failure is reported, not retried behind the user's back; Try again reloads.
            warnOnce('stats', result.failure);
            setLoadFailure({ serverId, message: result.failure });
            return;
          }
          setLoadFailure(null);
          const payload = result.payload;
          const current = payload && codexSubscriptionVisibleForProviderState(payload.codexSubscription, providerSnapshot)
            ? payload
            : payload ? { ...payload, codexSubscription: undefined } : null;
          const data = normalizeStatsPayload(current);
          if (Object.keys(data?.ranges ?? {}).length === 0 && attempt < EMPTY_STATS_MAX_RETRIES) {
            retryTimer = setTimeout(() => loadStats(attempt + 1), EMPTY_STATS_RETRY_MS * (attempt + 1));
            return;
          }
          const snapshot = { serverId, data };
          statsSnapshotRef.current = snapshot;
          setStatsSnapshot(snapshot);
        })().finally(() => {
          if (!cancelled && isCurrentServer(serverId) && !retryTimer) setLoading(false);
        });
      };
      loadStats(0);
      return () => { cancelled = true; if (retryTimer) clearTimeout(retryTimer); };
    }, [connectionState, currentServerId, hasConnectingServer, isCurrentServer, reloadToken]),
  );

  const selectRangeIndex = useCallback((index: number) => {
    const nextRange = statsRangeAt(index);
    if (nextRange === range) return;
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    setRange(nextRange);
  }, [range]);
  const beginNestedHorizontalGesture = useCallback(() => {
    setNestedHorizontalScrollActive(true);
  }, []);
  const endNestedHorizontalGesture = useCallback(() => {
    setNestedHorizontalScrollActive(false);
  }, []);
  const handleRangeTabBarLayout = useCallback((event: LayoutChangeEvent) => {
    setRangeTabBarWidth(event.nativeEvent.layout.width);
  }, []);

  return (
    <SafeAreaView style={s.container} edges={[]}>
      <TabView<{ key: TimeRange; label: string }>
        navigationState={{ index: statsRangeIndex(range), routes: STATS_TAB_ROUTES }}
        initialLayout={{ width }}
        animationEnabled={!reducedMotion}
        lazy={false}
        overScrollMode="never"
        swipeEnabled={!nestedHorizontalScrollActive}
        renderTabBar={({ position, jumpTo }) => (
          <StatsRangeTabBar
            position={position}
            range={range}
            width={rangeTabBarWidth}
            onLayout={handleRangeTabBarLayout}
            jumpTo={jumpTo}
            styles={s}
          />
        )}
        onIndexChange={selectRangeIndex}
        renderScene={({ route }) => (
          <StatsRangeScene
            range={route.key}
            active={route.key === range}
            statsData={statsData}
            loading={loading}
            loadError={loadError}
            onRetry={() => setReloadToken(token => token + 1)}
            expandedSections={expandedSections}
            toggleSection={toggleSection}
            setSelectedDay={setSelectedDay}
            onSelectModel={model => setModelSelection({ serverId: currentServerId, range: route.key, key: statsModelKey(model) })}
            onNestedHorizontalGestureStart={beginNestedHorizontalGesture}
            onNestedHorizontalGestureEnd={endNestedHorizontalGesture}
          />
        )}
        style={s.pager}
      />

      <DayDetailSheet selectedDay={selectedDay} onClose={() => setSelectedDay(null)} />
      <ModelDetailSheet model={selectedModel} pricing={statsData?.pricing} onClose={() => setModelSelection(null)} />
    </SafeAreaView>
  );
}

export function StatsScreenshotDemo({
  statsData,
  range = 'all',
}: {
  statsData: StatsView;
  range?: TimeRange;
}) {
  const [expandedSections, setExpandedSections] = useState<Set<string>>(
    new Set(),
  );
  const [selectedDay, setSelectedDay] = useState<DayCell | null>(null);
  const [selectedModel, setSelectedModel] = useState<ModelStat | null>(null);
  const toggleSection = useCallback((section: string) => {
    setExpandedSections((previous) => {
      const next = new Set(previous);
      if (next.has(section)) next.delete(section);
      else next.add(section);
      return next;
    });
  }, []);

  return (
    <View style={{ flex: 1 }}>
      <StatsRangeScene
        range={range}
        active
        statsData={statsData}
        loading={false}
        loadError={null}
        onRetry={() => undefined}
        expandedSections={expandedSections}
        toggleSection={toggleSection}
        setSelectedDay={setSelectedDay}
        onSelectModel={setSelectedModel}
        onNestedHorizontalGestureStart={() => undefined}
        onNestedHorizontalGestureEnd={() => undefined}
      />
      <DayDetailSheet
        selectedDay={selectedDay}
        onClose={() => setSelectedDay(null)}
      />
      <ModelDetailSheet model={selectedModel} pricing={statsData.pricing} onClose={() => setSelectedModel(null)} />
    </View>
  );
}

function StatsRangeTabBar({
  position,
  range,
  width,
  onLayout,
  jumpTo,
  styles,
}: {
  position: NativeAnimated.AnimatedInterpolation<number>;
  range: TimeRange;
  width: number;
  onLayout(event: LayoutChangeEvent): void;
  jumpTo(key: string): void;
  styles: ReturnType<typeof createStyles>;
}) {
  const tabWidth = Math.max(
    0,
    (width - STATS_TAB_BAR_PADDING * 2 - STATS_TAB_BAR_GAP * (RANGE_OPTIONS.length - 1)) /
      RANGE_OPTIONS.length,
  );
  const translateX = position.interpolate({
    inputRange: RANGE_OPTIONS.map((_, index) => index),
    outputRange: RANGE_OPTIONS.map((_, index) => index * (tabWidth + STATS_TAB_BAR_GAP)),
    extrapolate: 'clamp',
  });

  return (
    <View style={styles.header}>
      <View style={styles.rangeRow} onLayout={onLayout}>
        {tabWidth > 0 && (
          <NativeAnimated.View
            pointerEvents="none"
            style={[
              styles.rangeTabIndicator,
              { width: tabWidth, transform: [{ translateX }] },
            ]}
          />
        )}
        {RANGE_OPTIONS.map((opt, index) => {
          const active = range === opt.key;
          const activeTextOpacity = position.interpolate({
            inputRange: [index - 1, index, index + 1],
            outputRange: [0, 1, 0],
            extrapolate: 'clamp',
          });
          return (
            <AnimatedPressable
              key={opt.key}
              style={styles.rangeTab}
              scale={1}
              accessibilityRole="tab"
              accessibilityState={{ selected: active }}
              accessibilityLabel={`${opt.label} range`}
              onPress={() => jumpTo(opt.key)}
            >
              <Text style={styles.rangeTabText}>{opt.label}</Text>
              <NativeAnimated.Text
                accessible={false}
                aria-hidden
                style={[styles.rangeTabText, styles.rangeTabTextActive, { opacity: activeTextOpacity }]}
              >
                {opt.label}
              </NativeAnimated.Text>
            </AnimatedPressable>
          );
        })}
      </View>
    </View>
  );
}

interface StatsRangeSceneProps {
  range: TimeRange;
  active: boolean;
  statsData: StatsView | null;
  loading: boolean;
  loadError: string | null;
  onRetry(): void;
  expandedSections: Set<string>;
  toggleSection(section: string): void;
  setSelectedDay(day: DayCell | null): void;
  onSelectModel(model: ModelStat): void;
  onNestedHorizontalGestureStart(): void;
  onNestedHorizontalGestureEnd(): void;
}

function StatsRangeScene({
  range,
  active,
  statsData,
  loading,
  loadError,
  onRetry,
  expandedSections,
  toggleSection,
  setSelectedDay,
  onSelectModel,
  onNestedHorizontalGestureStart,
  onNestedHorizontalGestureEnd,
}: StatsRangeSceneProps) {
  const { colors } = useAppTheme();
  const insets = useSafeAreaInsets();
  const s = useMemo(() => createStyles(colors), [colors]);

  const data = statsData?.ranges?.[range] ?? EMPTY_RANGE;
  const allData = statsData?.ranges?.all ?? EMPTY_RANGE;
  const days = data.days ?? [];

  const rankedModels = useMemo(() => sortByActivity(data.models ?? []), [data.models]);
  const rankedProjects = useMemo(() => sortByActivity(data.projects ?? []), [data.projects]);
  const projectActivityUsesTokens = useMemo(() => activityUsesTokens(rankedProjects), [rankedProjects]);
  const maxProjectActivity = useMemo(
    () => Math.max(...rankedProjects.map(project => activityValue(project, projectActivityUsesTokens)), 1),
    [projectActivityUsesTokens, rankedProjects],
  );
  const maxSkillCalls = useMemo(() => Math.max(...(data.skills?.map(s => s.calls) ?? [0]), 1), [data.skills]);
  const maxToolCalls = useMemo(() => Math.max(...(data.tools?.map(t => t.calls) ?? [0]), 1), [data.tools]);
  const totalSkills = data.skills?.length ?? 0;
  const totalSkillCalls = useMemo(() => (data.skills ?? []).reduce((s, v) => s + v.calls, 0), [data.skills]);
  const totalToolCalls = useMemo(() => (data.tools ?? []).reduce((s, v) => s + v.calls, 0), [data.tools]);
  const visibleModels = useMemo(() => topItems(rankedModels), [rankedModels]);
  const visibleProjects = useMemo(() => topItems(rankedProjects), [rankedProjects]);
  const visibleSkills = useMemo(() => topItems(data.skills ?? []), [data.skills]);
  const visibleTools = useMemo(() => topItems(data.tools ?? []), [data.tools]);
  // One day has no trend, so the Day range charts the week that ends today.
  const activityDays = useMemo(
    () => buildDailyActivitySeries(range === 'day' ? allData.days ?? [] : days, range),
    [allData.days, days, range],
  );

  const hasData = hasRangeStats(data);
  const codexSubscriptions = (statsData?.codexSubscriptions ?? [])
    .filter(isOfficialCodexSubscription);
  const showStatsContent = hasData || codexSubscriptions.length > 0;
  const hasAnyStats = useMemo(
    () => Object.values(statsData?.ranges ?? {}).some(item => hasRangeStats(item)),
    [statsData],
  );
  const latestDay = allData.days?.[allData.days.length - 1] ?? null;
  const emptyTitle = !hasAnyStats
    ? 'No stats yet'
    : range === 'day'
      ? 'No activity today'
      : `No ${RANGE_OPTIONS.find(opt => opt.key === range)?.label.toLowerCase()} activity`;

  return (
    <View
      style={s.scene}
      accessibilityElementsHidden={!active}
      importantForAccessibility={active ? 'auto' : 'no-hide-descendants'}
      aria-hidden={!active}
    >
      {loading && !statsData ? (
        <View style={s.emptyContainer}>
          <EmptyState busy />
        </View>
      ) : !statsData && loadError ? (
        <View style={s.emptyContainer}>
          <EmptyState
            tone="danger"
            icon="alert"
            title="Stats didn't load"
            detail={loadError}
            action={{ label: 'Try again', icon: 'refresh', onPress: onRetry }}
          />
        </View>
      ) : !showStatsContent ? (
        <View style={s.emptyContainer}>
          <EmptyState
            icon="stats"
            title={emptyTitle}
            detail={latestDay ? `Latest activity: ${shortDate(latestDay.date)}` : null}
          />
        </View>
      ) : (
        <ScrollView
          style={s.scrollView}
          contentContainerStyle={[
            s.scroll,
            { paddingBottom: Math.max(insets.bottom, 20) + 12 },
          ]}
          showsVerticalScrollIndicator={false}
        >
            {codexSubscriptions.map((subscription, index) => (
              <View key={`${subscription.serverLabel ?? 'server'}:${index}`} style={[s.card, s.codexCard]}>
                <View style={s.codexTitleRow}>
                  <View style={s.codexTitleBlock}>
                    <Text style={s.label}>Codex subscription</Text>
                    <Text style={s.codexPlan}>
                      {subscription.authKind === 'official'
                        ? codexPlanLabel(subscription.plan)
                        : codexAuthSummary(subscription.authKind)}
                    </Text>
                  </View>
                  {subscription.stale && <Text style={s.staleBadge}>STALE</Text>}
                </View>
                {subscription.authKind === 'official' && subscription.state === 'available' ? (
                  <View style={s.codexWindows}>
                    {(subscription.windows ?? []).map(window => {
                      const used = normalizeCodexUsedPercent(window.usedPercent);
                      const remaining = codexRemainingPercent(window.usedPercent);
                      return (
                        <View key={window.name} style={s.codexWindow} accessible accessibilityLabel={`${codexWindowLabel(window.windowMinutes)}, ${remaining.toFixed(0)} percent remaining, ${fmtReset(window.resetsAt)}`}>
                          <View style={s.codexWindowHeader}>
                            <Text style={s.codexWindowLabel}>{codexWindowLabel(window.windowMinutes)}</Text>
                            <Text style={s.codexRemaining}>{remaining.toFixed(0)}% left</Text>
                          </View>
                          <View style={s.codexTrack}>
                            <View style={[s.codexFill, { width: `${used}%` }]} />
                          </View>
                          <Text style={s.codexReset}>{fmtReset(window.resetsAt)}</Text>
                        </View>
                      );
                    })}
                    {subscription.stale && (
                      <Text style={s.codexNotice}>Live usage is temporarily unavailable. Showing the last successful update.</Text>
                    )}
                  </View>
                ) : (
                  <Text style={s.codexNotice}>
                    {subscription.authKind === 'api_key'
                      ? 'Subscription limits are only available when Codex is signed in with ChatGPT.'
                      : subscription.authKind === 'official'
                        ? 'Codex usage is temporarily unavailable. Existing activity stats are unaffected.'
                        : subscription.authKind === 'absent'
                          ? 'Sign in to the Codex CLI with ChatGPT to see subscription limits.'
                          : 'Mewla could not confidently identify official Codex subscription authentication.'}
                  </Text>
                )}
              </View>
            ))}

            {/* ── Summary ── */}
            {hasData && <View style={[s.card, s.summaryCard]}>
              <View style={s.summaryMetrics}>
                <View style={s.summaryMetric}>
                  <Text style={s.summaryLabel}>Cost · {costSourceLabel(data.costProvenance)}</Text>
                  <Text
                    style={[
                      s.summaryValue,
                      s.summaryCost,
                      !isCostKnown(data) && data.cost === 0 && s.summaryUnavailable,
                    ]}
                  >
                    {fmtAvailableCost(data.cost, data.costKnown)}
                  </Text>
                </View>
                <View style={s.summaryDivider} />
                <View style={s.summaryMetric}>
                  <Text style={s.summaryLabel}>Tokens</Text>
                  <Text
                    style={[
                      s.summaryValue,
                      !isTotalTokensKnown(data) && data.totalTokens === 0 && s.summaryUnavailable,
                    ]}
                  >
                    {fmtAvailableTokens(data.totalTokens, data.totalTokensKnown)}
                  </Text>
                </View>
              </View>
              <Text style={s.summarySessions}>{sessionSummary(data.sessions)}</Text>
              {statsData?.pricing && (
                <Text style={s.summaryNote}>
                  {statsData.pricing.source}
                  {statsData.pricing.updatedAt
                    ? ` · ${formatDate(statsData.pricing.updatedAt, DateStyle.shortDate, { year: 'numeric' })}`
                    : ''}
                  {statsData.pricing.lastError
                    ? ' · Refresh failed'
                    : statsData.pricing.stale
                      ? ' · Outdated'
                      : ''}
                </Text>
              )}
            </View>}

            {/* ── Models ── */}
            {(data.models?.length ?? 0) > 0 && (
              <View style={s.card}>
                <Text style={s.label}>Models</Text>
                {(expandedSections.has('models') ? rankedModels : visibleModels).map((m, i, list) => (
                  <AnimatedPressable
                    key={statsModelKey(m)}
                    style={[s.modelRow, i === list.length - 1 && s.rowLast]}
                    accessibilityRole="button"
                    accessibilityLabel={`${m.id || m.name}, ${m.provider || ''}, ${costSourceLabel(m.costProvenance)} cost ${fmtAvailableCost(m.cost, m.costKnown)}, ${rowActivitySummary(m)}`}
                    accessibilityHint="Open model usage and pricing details"
                    onPress={() => onSelectModel(m)}
                  >
                    <View style={s.modelHeading}>
                      <Text style={[s.rowName, s.rowInfo]} numberOfLines={1} ellipsizeMode="middle">{statsModelHeading(m, data.models)}</Text>
                      <Text style={[s.rowCost, s.modelAmount, !isCostKnown(m) && m.cost === 0 && s.rowValueUnavailable]} numberOfLines={1}>
                        {costEstimateMark(m.cost, m.costKnown, m.costProvenance)}{fmtAvailableCost(m.cost, m.costKnown)}
                      </Text>
                    </View>
                    <Text style={s.rowMeta} numberOfLines={1}>{rowActivitySummary(m)}</Text>
                  </AnimatedPressable>
                ))}
                {data.models.length > MAX_LIST_ITEMS && (
                  <ExpandToggle expanded={expandedSections.has('models')} total={data.models.length} onPress={() => toggleSection('models')} colors={colors} />
                )}
              </View>
            )}

            {/* ── Projects ── */}
            {(data.projects?.length ?? 0) > 0 && (
              <View style={s.card}>
                <Text style={s.label}>Projects</Text>
                {(expandedSections.has('projects') ? rankedProjects : visibleProjects).map((p, i, list) => (
                  <View key={p.name} style={[s.row, i === list.length - 1 && s.rowLast]}>
                    <View style={s.rowInfo}>
                      <Text style={s.rowName} numberOfLines={1}>{p.name}</Text>
                      <Text style={s.rowMeta} numberOfLines={1}>{[rowActivitySummary(p), costSourceLabel(p.costProvenance)].join(' · ')}</Text>
                    </View>
                    <Text
                      style={[
                        s.rowCost,
                        !isCostKnown(p) && p.cost === 0 && s.rowValueUnavailable,
                      ]}
                    >
                      {fmtAvailableCost(p.cost, p.costKnown)}
                    </Text>
                    <Bar
                      ratio={activityValue(p, projectActivityUsesTokens) / maxProjectActivity}
                      color={colors.accent}
                      trackColor={colors.borderSubtle}
                    />
                  </View>
                ))}
                {data.projects.length > MAX_LIST_ITEMS && (
                  <ExpandToggle expanded={expandedSections.has('projects')} total={data.projects.length} onPress={() => toggleSection('projects')} colors={colors} />
                )}
              </View>
            )}

            {/* ── Activity ── */}
            {days.length > 0 && (
              <ActivityChart
                days={activityDays}
                onOpenDay={setSelectedDay}
                onGestureStart={onNestedHorizontalGestureStart}
                onGestureEnd={onNestedHorizontalGestureEnd}
              />
            )}

            {/* ── Skills ── */}
            {totalSkills > 0 && (
              <View style={s.card}>
                <View style={s.labelRow}>
                  <Text style={s.label}>Skills</Text>
                  <Text style={s.labelCount}>{totalSkillCalls} calls</Text>
                </View>
                {(expandedSections.has('skills') ? data.skills : visibleSkills).map((sk, i, list) => (
                  <View key={sk.name} style={[s.row, i === list.length - 1 && s.rowLast]}>
                    <View style={s.rowInfo}>
                      <Text style={s.skillCmd} numberOfLines={1}>{sk.name}</Text>
                      <Text style={s.rowMeta} numberOfLines={1}>{sk.projects?.join(' · ')}</Text>
                    </View>
                    <Text style={s.rowCount}>{sk.calls}</Text>
                    <Bar ratio={sk.calls / maxSkillCalls} color={colors.accent} trackColor={colors.borderSubtle} />
                  </View>
                ))}
                {totalSkills > MAX_LIST_ITEMS && (
                  <ExpandToggle expanded={expandedSections.has('skills')} total={totalSkills} onPress={() => toggleSection('skills')} colors={colors} />
                )}
              </View>
            )}

            {/* ── Tools ── */}
            {totalToolCalls > 0 && (
              <View style={s.card}>
                <View style={s.labelRow}>
                  <Text style={s.label}>Tools</Text>
                  <Text style={s.labelCount}>{totalToolCalls} calls</Text>
                </View>
                {(expandedSections.has('tools') ? data.tools : visibleTools).map((t, i, list) => (
                  <View key={t.name} style={[s.row, i === list.length - 1 && s.rowLast]}>
                    <View style={s.rowInfo}>
                      <Text style={s.rowName} numberOfLines={1}>{t.name}</Text>
                    </View>
                    <Text style={s.rowCount}>{t.calls}</Text>
                    <Bar ratio={t.calls / maxToolCalls} color={colors.accent} trackColor={colors.borderSubtle} />
                  </View>
                ))}
                {(data.tools?.length ?? 0) > MAX_LIST_ITEMS && (
                  <ExpandToggle expanded={expandedSections.has('tools')} total={data.tools.length} onPress={() => toggleSection('tools')} colors={colors} />
                )}
              </View>
            )}

            <View style={{ height: 28 }} />
        </ScrollView>
      )}

    </View>
  );
}

// ── Small components ───────────────────────────────────────

function ModelDetailSheet({ model, pricing, onClose }: {
  model: ModelStat | null;
  pricing?: PricingStatus;
  onClose(): void;
}) {
  const { colors } = useAppTheme();
  const s = useMemo(() => createStyles(colors), [colors]);
  const money = (value: number) => formatNumber(value, {
    style: 'currency', currency: 'USD', maximumFractionDigits: 6,
  });
  const reported = model?.costReported ?? model?.reportedCost;
  const estimated = model?.costEstimated ?? model?.estimatedCost;
  const facts: Array<[string, string | undefined]> = model ? [
    ['Provider', model.provider],
    ['Price type', costSourceLabel(model.costProvenance)],
    ['Cost', fmtAvailable(model.cost, model.costKnown, money)],
    ['Reported', model.costProvenance === 'mixed' && reported !== undefined && reported > 0 ? money(reported) : undefined],
    ['Estimated', model.costProvenance === 'mixed' && estimated !== undefined && estimated > 0 ? money(estimated) : undefined],
    ['Tokens', fmtAvailableTokens(model.totalTokens, model.totalTokensKnown)],
    ['Sessions', String(model.sessions)],
    ['Input', fmtAvailableTokens(model.inputTokens, model.tokenBreakdownKnown)],
    ['Output', fmtAvailableTokens(model.outputTokens, model.tokenBreakdownKnown)],
    ['Reasoning', fmtAvailableTokens(model.reasoningTokens, model.tokenBreakdownKnown)],
    ['Cache read', fmtAvailableTokens(model.cacheRead, model.tokenBreakdownKnown)],
    ['Cache write', fmtAvailableTokens(model.cacheCreate, model.tokenBreakdownKnown)],
    ['Source', model.estimateSource],
    ['Reference provider', model.referenceProvider],
    ['Updated', model.pricingUpdatedAt ? formatDate(model.pricingUpdatedAt, DateStyle.fullDateTime) : undefined],
  ] : [];
  return (
    <RisingSheet visible={model !== null} onClose={onClose} align="bottom" cardStyle={s.modelDetailCard}>
      {model ? <>
        <View style={s.modelDetailHeader}>
          <Text selectable accessibilityRole="header" style={[s.rowName, s.rowInfo]}>{model.id || model.name}</Text>
          <IconButton icon="close" accessibilityLabel="Close model details" onPress={onClose} tone="ghost" />
        </View>
        <ScrollView contentContainerStyle={s.modelDetailContent}>
          {facts.filter(([, value]) => value !== undefined).map(([label, value]) => (
            <View key={label} style={s.modelFact}>
              <Text style={s.rowMeta}>{label}</Text>
              <Text selectable style={s.modelFactValue}>{value}</Text>
            </View>
          ))}
          {unpricedReasonLabel(model.unpricedReason) ? <Text selectable style={s.rowMeta}>{unpricedReasonLabel(model.unpricedReason)}</Text> : null}
          {estimated !== undefined && estimated > 0 && pricing?.basis ? <Text selectable style={s.rowMeta}>{pricing.basis}</Text> : null}
          {pricing?.lastError ? <Text selectable style={s.rowMeta}>{pricing.lastError}</Text> : null}
        </ScrollView>
      </> : null}
    </RisingSheet>
  );
}

const ACTIVITY_CHART_HEIGHT = 120;
const ACTIVITY_CHART_INSET = 5;

function chartDate(dateStr: string): string {
  const date = parseDateOnly(dateStr);
  return date ? formatDate(date, DateStyle.shortDate) : dateStr;
}

function activityReadout(day: DayCell | null, usesTokens: boolean): string {
  if (usesTokens) return `${day ? fmtAvailableTokens(day.totalTokens, day.totalTokensKnown) : '0'} tokens`;
  return sessionSummary(day?.sessions ?? 0);
}

/**
 * Daily activity as one ink curve. The readout names today's value; touching,
 * dragging or hovering moves it to that day, and a tap opens the day's detail.
 */
function ActivityChart({
  days,
  onOpenDay,
  onGestureStart,
  onGestureEnd,
}: {
  days: ActivityDay[];
  onOpenDay(day: DayCell): void;
  onGestureStart(): void;
  onGestureEnd(): void;
}) {
  const { colors } = useAppTheme();
  const s = useMemo(() => createStyles(colors), [colors]);
  const gradientId = `activity-${useId().replace(/[^a-zA-Z0-9_-]/g, '')}`;
  const [width, setWidth] = useState(0);
  const [focusIndex, setFocusIndex] = useState<number | null>(null);
  const usesTokens = useMemo(
    () => activityUsesTokens(days.flatMap(item => (item.day ? [item.day] : []))),
    [days],
  );
  const points = useMemo(
    () => activityChartPoints(
      days.map(item => (item.day ? activityValue(item.day, usesTokens) : 0)),
      { width, height: ACTIVITY_CHART_HEIGHT, inset: ACTIVITY_CHART_INSET },
    ),
    [days, usesTokens, width],
  );
  const paths = useMemo(
    () => ({ line: activityCurvePath(points), area: activityAreaPath(points, ACTIVITY_CHART_HEIGHT) }),
    [points],
  );
  // A long history packs days under 3 pt apart; a thinner stroke keeps it an outline.
  const dense = points.length > 1 && points[1].x - points[0].x < 3;
  const lastIndex = days.length - 1;
  const activeIndex = Math.min(focusIndex ?? lastIndex, lastIndex);
  const active = days[activeIndex];
  const activePoint = points[activeIndex];
  const activeDate = active.date === dateOnlyString(new Date()) ? 'Today' : chartDate(active.date);

  const gesture = useMemo(() => {
    const focusAt = (x: number) => setFocusIndex(activityIndexAt(x, points));
    const scrub = Gesture.Pan()
      .runOnJS(true)
      .activeOffsetX([-6, 6])
      .failOffsetY([-10, 10])
      .onBegin(event => focusAt(event.x))
      // Pause the range pager only for a real scrub. Pausing on touch-down
      // re-rendered every range while a vertical scroll began, and the
      // readout's reset waited behind that work.
      .onStart(() => onGestureStart())
      .onUpdate(event => focusAt(event.x))
      .onFinalize(() => {
        setFocusIndex(null);
        onGestureEnd();
      });
    const tap = Gesture.Tap()
      .runOnJS(true)
      .onEnd((event, success) => {
        const day = days[activityIndexAt(event.x, points)]?.day;
        if (!success || !day) return;
        Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
        onOpenDay(day);
      });
    const hover = Gesture.Hover()
      .runOnJS(true)
      .onBegin(event => focusAt(event.x))
      .onUpdate(event => focusAt(event.x))
      .onFinalize(() => setFocusIndex(null));
    return Gesture.Simultaneous(hover, Gesture.Race(scrub, tap));
  }, [days, onGestureEnd, onGestureStart, onOpenDay, points]);

  return (
    <View style={s.card}>
      <View style={s.labelRow}>
        <Text style={[s.label, s.activityLabel]}>Activity</Text>
        <Text style={s.activityReadout} numberOfLines={1}>
          <Text style={s.activityReadoutDate}>{activeDate}  </Text>
          {activityReadout(active.day, usesTokens)}
        </Text>
      </View>
      <GestureDetector gesture={gesture}>
        <View
          style={s.activityChart}
          onLayout={event => {
            const next = Math.round(event.nativeEvent.layout.width);
            setWidth(previous => (previous === next ? previous : next));
          }}
          accessible
          accessibilityRole="adjustable"
          accessibilityLabel="Daily activity"
          accessibilityValue={{
            text: `${accessibleDate(active.date)}, ${active.day ? dayAccessibilityValue(active.day) : 'No recorded activity'}`,
          }}
          accessibilityHint="Adjust to move between days; activate for the day's details"
          accessibilityActions={[{ name: 'increment' }, { name: 'decrement' }, { name: 'activate' }]}
          onAccessibilityAction={event => {
            if (event.nativeEvent.actionName === 'increment') setFocusIndex(Math.min(lastIndex, activeIndex + 1));
            else if (event.nativeEvent.actionName === 'decrement') setFocusIndex(Math.max(0, activeIndex - 1));
            else if (event.nativeEvent.actionName === 'activate' && active.day) onOpenDay(active.day);
          }}
        >
          {width > 0 && activePoint ? (
            <Svg width={width} height={ACTIVITY_CHART_HEIGHT}>
              <Defs>
                <LinearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
                  <Stop offset="0" stopColor={colors.accent} stopOpacity={0.14} />
                  <Stop offset="1" stopColor={colors.accent} stopOpacity={0} />
                </LinearGradient>
              </Defs>
              <Line
                x1={0}
                x2={width}
                y1={ACTIVITY_CHART_HEIGHT - 0.5}
                y2={ACTIVITY_CHART_HEIGHT - 0.5}
                stroke={colors.borderSubtle}
                strokeWidth={1}
              />
              {paths.area ? <Path d={paths.area} fill={`url(#${gradientId})`} /> : null}
              <Path
                d={paths.line}
                stroke={colors.accent}
                strokeWidth={dense ? 1 : 1.75}
                fill="none"
                strokeLinejoin="round"
                strokeLinecap="round"
              />
              {focusIndex !== null ? (
                <Line
                  x1={activePoint.x}
                  x2={activePoint.x}
                  y1={0}
                  y2={ACTIVITY_CHART_HEIGHT}
                  stroke={colors.textTertiary}
                  strokeWidth={StyleSheet.hairlineWidth}
                />
              ) : null}
              <Circle
                cx={activePoint.x}
                cy={activePoint.y}
                r={3.5}
                fill={colors.accent}
                stroke={colors.bgPrimary}
                strokeWidth={1.5}
              />
            </Svg>
          ) : null}
        </View>
      </GestureDetector>
      <View style={s.activityAxisRow}>
        <Text style={s.activityAxisLabel}>{chartDate(days[0].date)}</Text>
        <Text style={s.activityAxisLabel}>{chartDate(days[lastIndex].date)}</Text>
      </View>
    </View>
  );
}

function DayDetailSheet({
  selectedDay,
  onClose,
}: {
  selectedDay: DayCell | null;
  onClose(): void;
}) {
  const { colors } = useAppTheme();
  const s = useMemo(() => createStyles(colors), [colors]);

  return (
    <RisingSheet
      visible={selectedDay !== null}
      onClose={onClose}
      cardStyle={s.detailCard}
    >
      {selectedDay && (
        <>
          <Text
            style={s.detailTitle}
            accessibilityRole="header"
            accessibilityLabel={accessibleDate(selectedDay.date)}
          >
            {selectedDay.date}
          </Text>
          <View style={s.detailGrid}>
            <DItem
              label="Cost"
              value={fmtAvailableCost(selectedDay.cost, selectedDay.costKnown)}
              accent={isCostKnown(selectedDay) || selectedDay.cost > 0}
              colors={colors}
              styles={s}
            />
            <DItem label="Sessions" value={`${selectedDay.sessions}`} colors={colors} styles={s} />
            <DItem
              label="Total"
              value={fmtAvailableTokens(selectedDay.totalTokens, selectedDay.totalTokensKnown)}
              colors={colors}
              styles={s}
            />
            <DItem
              label="Input"
              value={fmtAvailableTokens(selectedDay.inputTokens, selectedDay.tokenBreakdownKnown)}
              colors={colors}
              styles={s}
            />
            <DItem
              label="Cache"
              value={fmtAvailableTokens(selectedDay.cacheRead, selectedDay.tokenBreakdownKnown)}
              colors={colors}
              styles={s}
            />
            <DItem
              label="Output"
              value={fmtAvailableTokens(selectedDay.outputTokens, selectedDay.tokenBreakdownKnown)}
              colors={colors}
              styles={s}
            />
            <DItem
              label="Reason"
              value={fmtAvailableTokens(selectedDay.reasoningTokens, selectedDay.tokenBreakdownKnown)}
              colors={colors}
              styles={s}
            />
          </View>
        </>
      )}
    </RisingSheet>
  );
}

function DItem({
  label,
  value,
  accent,
  colors,
  styles,
}: {
  label: string;
  value: string;
  accent?: boolean;
  colors: AppColors;
  styles: ReturnType<typeof createStyles>;
}) {
  return (
    <View
      style={styles.dItem}
      accessible
      accessibilityLabel={label}
      accessibilityValue={{ text: value }}
    >
      <Text style={styles.dLabel}>{label}</Text>
      <Text style={[styles.dValue, accent && { color: colors.accent }]}>{value}</Text>
    </View>
  );
}

function Bar({
  ratio,
  color,
  trackColor,
}: {
  ratio: number;
  color: string;
  trackColor: string;
}) {
  const width = useSharedValue(0);
  React.useEffect(() => {
    width.value = withTiming(Math.min(ratio, 1), { duration: 320 });
  }, [ratio, width]);
  const style = useAnimatedStyle(() => ({
    width: `${interpolate(width.value, [0, 1], [0, 100])}%`,
  }));
  return (
    <View style={{ width: 44, height: 2.5, borderRadius: 1.5, backgroundColor: trackColor }}>
      <Animated.View
        style={[{ height: 2.5, borderRadius: 1.5, backgroundColor: color, opacity: 0.7 }, style]}
      />
    </View>
  );
}

function ExpandToggle({
  expanded,
  total,
  onPress,
  colors,
}: {
  expanded: boolean;
  total: number;
  onPress: () => void;
  colors: AppColors;
}) {
  return (
    <AnimatedPressable
      style={{
        minHeight: 44,
        alignItems: 'center',
        justifyContent: 'center',
        marginTop: 4,
      }}
      scale={0.97}
      accessibilityRole="button"
      accessibilityState={{ expanded }}
      accessibilityLabel={expanded ? 'Show fewer items' : `Show ${total - MAX_LIST_ITEMS} more items`}
      onPress={onPress}
    >
      <Text style={[TypeScale.label, UiTextMetrics, { color: colors.accent }]}>
        {expanded ? 'less' : `${total - MAX_LIST_ITEMS} more`}
      </Text>
    </AnimatedPressable>
  );
}

// ── Styles ─────────────────────────────────────────────────

function createStyles(colors: AppColors, isLight = true) {
  return StyleSheet.create({
    container: { flex: 1, backgroundColor: colors.bgPrimary },
    pager: { flex: 1 },
    scene: { flex: 1, backgroundColor: colors.bgPrimary },
    header: {
      width: '100%',
      maxWidth: STATS_CONTENT_MAX_WIDTH,
      alignSelf: 'center',
      paddingHorizontal: Spacing.lg,
      paddingTop: 12,
      paddingBottom: 10,
      backgroundColor: colors.bgPrimary,
      zIndex: 2,
    },
    // The same track and thumb as SegmentedControl.
    rangeRow: {
      flexDirection: 'row',
      gap: STATS_TAB_BAR_GAP,
      padding: STATS_TAB_BAR_PADDING,
      borderRadius: Radii.pill,
      backgroundColor: colors.surfaceSubtle,
    },
    rangeTabIndicator: {
      position: 'absolute',
      left: STATS_TAB_BAR_PADDING,
      top: STATS_TAB_BAR_PADDING,
      bottom: STATS_TAB_BAR_PADDING,
      borderRadius: Radii.pill,
      backgroundColor: isLight ? colors.bgSurface : colors.surfaceActive,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: isLight ? colors.border : 'transparent',
    },
    rangeTab: {
      flex: 1,
      minWidth: 0,
      minHeight: 38,
      alignItems: 'center',
      justifyContent: 'center',
      paddingHorizontal: 4,
      borderRadius: Radii.pill,
      zIndex: 1,
    },
    rangeTabText: {
      ...TypeScale.label,
      ...UiTextMetrics,
      color: colors.textSecondary,
      textAlign: 'center',
    },
    rangeTabTextActive: { position: 'absolute', color: colors.textPrimary },
    scrollView: { flex: 1 },
    scroll: {
      width: '100%',
      maxWidth: STATS_CONTENT_MAX_WIDTH,
      alignSelf: 'center',
      paddingHorizontal: Spacing.lg,
      gap: 12,
      paddingTop: 6,
    },

    // Empty
    emptyContainer: {
      flex: 1,
      justifyContent: 'center',
      alignItems: 'center',
      paddingHorizontal: 36,
    },

    // Sections
    card: {
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: colors.borderSubtle,
      paddingVertical: 18,
    },
    label: {
      ...TypeScale.heading,
      ...UiTextMetrics,
      color: colors.textPrimary,
      marginBottom: 12,
    },
    labelRow: {
      flexDirection: 'row',
      justifyContent: 'space-between',
      alignItems: 'center',
      gap: 12,
      marginBottom: 12,
    },
    labelCount: {
      ...TypeScale.caption,
      ...UiTextMetrics,
      color: colors.textTertiary,
    },

    // Summary
    summaryCard: {
      paddingHorizontal: 16,
      paddingVertical: 16,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderSubtle,
      borderRadius: Radii.md,
      backgroundColor: colors.bgSurface,
    },
    summaryMetrics: {
      flexDirection: 'row',
      alignItems: 'stretch',
    },
    summaryMetric: { flex: 1, minWidth: 0 },
    summaryDivider: {
      width: StyleSheet.hairlineWidth,
      backgroundColor: colors.borderSubtle,
      marginHorizontal: 12,
    },
    summaryLabel: {
      ...TypeScale.caption,
      ...UiTextMetrics,
      color: colors.textTertiary,
      marginBottom: 6,
    },
    summaryValue: {
      ...TypeScale.title,
      ...UiTextMetrics,
      color: colors.textPrimary,
      flexShrink: 1,
    },
    summaryCost: { color: colors.accent },
    summaryUnavailable: { color: colors.textTertiary },
    summarySessions: {
      ...TypeScale.label,
      ...UiTextMetrics,
      color: colors.textSecondary,
      marginTop: 12,
    },
    summaryNote: {
      ...TypeScale.caption,
      ...UiTextMetrics,
      color: colors.textTertiary,
      marginTop: 4,
    },

    codexCard: {
      paddingHorizontal: 16,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderSubtle,
      borderRadius: Radii.md,
      backgroundColor: colors.bgSurface,
    },
    codexTitleRow: { flexDirection: 'row', alignItems: 'flex-start', gap: 12 },
    codexTitleBlock: { flex: 1, minWidth: 0 },
    codexPlan: { ...TypeScale.caption, ...UiTextMetrics, color: colors.textSecondary, marginTop: -8 },
    staleBadge: { ...TypeScale.micro, ...UiTextMetrics, color: colors.textTertiary, marginTop: 3 },
    codexWindows: { gap: 16, marginTop: 18 },
    codexWindow: { gap: 7 },
    codexWindowHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'baseline', gap: 12 },
    codexWindowLabel: { ...TypeScale.label, ...UiTextMetrics, color: colors.textPrimary },
    codexRemaining: { ...TypeScale.label, ...UiTextMetrics, fontVariant: ['tabular-nums'], color: colors.accent },
    codexTrack: { height: 7, borderRadius: 4, backgroundColor: colors.borderSubtle, overflow: 'hidden' },
    codexFill: { height: '100%', borderRadius: 4, backgroundColor: colors.accent, opacity: 0.78 },
    codexReset: { ...TypeScale.caption, ...UiTextMetrics, color: colors.textTertiary },
    codexNotice: { ...TypeScale.compact, ...UiTextMetrics, color: colors.textSecondary, marginTop: 14 },

    // Activity chart
    activityLabel: { marginBottom: 0 },
    activityReadout: {
      ...TypeScale.label,
      ...UiTextMetrics,
      fontVariant: ['tabular-nums'],
      color: colors.textPrimary,
      flexShrink: 1,
    },
    activityReadoutDate: { color: colors.textTertiary },
    activityChart: { height: ACTIVITY_CHART_HEIGHT },
    activityAxisRow: {
      marginTop: 6,
      flexDirection: 'row',
      justifyContent: 'space-between',
    },
    activityAxisLabel: {
      ...TypeScale.micro,
      ...UiTextMetrics,
      color: colors.textTertiary,
    },

    // Rank rows
    modelRow: {
      gap: 4,
      paddingVertical: 12,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    modelHeading: { flexDirection: 'row', alignItems: 'center', gap: 12 },
    modelAmount: { width: 96, flexShrink: 0 },
    modelDetailCard: {
      width: '100%', maxWidth: 560, maxHeight: '85%',
      padding: 16, borderRadius: 8, backgroundColor: colors.modalSurface,
    },
    modelDetailHeader: { flexDirection: 'row', alignItems: 'flex-start', gap: 8, paddingBottom: 12 },
    modelDetailContent: { gap: 12, paddingBottom: 16 },
    modelFact: { gap: 2 },
    modelFactValue: { ...TypeScale.compact, ...UiTextMetrics, color: colors.textPrimary },
    row: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: 10,
      paddingVertical: 10,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    // The next section's top rule closes the list; a last-row rule doubled it.
    rowLast: { borderBottomWidth: 0 },
    rowInfo: { flex: 1, minWidth: 0 },
    rowName: {
      ...TypeScale.mono,
      ...UiTextMetrics,
      color: colors.textPrimary,
    },
    rowMeta: {
      ...TypeScale.caption,
      ...UiTextMetrics,
      color: colors.textTertiary,
      marginTop: 2,
    },
    rowCost: {
      ...TypeScale.label,
      ...UiTextMetrics,
      fontVariant: ['tabular-nums'],
      color: colors.accent,
      minWidth: 44,
      textAlign: 'right',
    },
    rowValueUnavailable: { color: colors.textTertiary },
    rowCount: {
      ...TypeScale.label,
      ...UiTextMetrics,
      fontVariant: ['tabular-nums'],
      color: colors.textSecondary,
      minWidth: 34,
      textAlign: 'right',
    },
    skillCmd: {
      ...TypeScale.monoStrong,
      ...UiTextMetrics,
      color: colors.textSecondary,
    },

    // Modal / day detail
    detailCard: {
      width: '100%',
      maxWidth: 320,
      alignSelf: 'center',
      borderRadius: Radii.lg,
      padding: 20,
      backgroundColor: colors.modalSurface,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
    },
    detailTitle: {
      ...TypeScale.heading,
      ...UiTextMetrics,
      color: colors.textPrimary,
      marginBottom: 16,
      textAlign: 'center',
    },
    detailGrid: { flexDirection: 'row', flexWrap: 'wrap', gap: 12 },
    dItem: {
      width: '46%',
      minHeight: 64,
      alignItems: 'center',
      justifyContent: 'center',
    },
    dLabel: {
      ...TypeScale.caption,
      ...UiTextMetrics,
      color: colors.textTertiary,
      marginBottom: 4,
      textAlign: 'center',
    },
    dValue: {
      ...TypeScale.label,
      ...UiTextMetrics,
      fontVariant: ['tabular-nums'],
      color: colors.textPrimary,
      textAlign: 'center',
    },
  });
}
