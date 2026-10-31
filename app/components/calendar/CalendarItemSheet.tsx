import React, { useEffect, useMemo, useState } from "react";
import {
  Alert,
  Modal,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { Radii, TypeScale, useAppColors } from "../../constants/tokens";
import { Icon } from "../icons/Icon";
import { StatusMark } from "../ui/StatusMark";
import { workStatusTextInk } from "../ui/workStatus";
import { MarkdownView } from "../work/MarkdownView";
import { ImageOwnerContext } from "../terminal/AppImage";
import { useBrainHostImageOwner } from "./useBrainHostImageOwner";
import { wsClient } from "../../services/websocket";
import { DateStyle, formatDate } from "../../services/dateFormat";
import {
  calendarStatusLabel,
  calendarStatusMark,
  executes,
} from "../../services/calendarPresentation";
import {
  calendarItemSummary,
  calendarNextLine,
  calendarRunDate,
  calendarRunPreview,
  calendarRunsNewestFirst,
  calendarScheduleLine,
} from "../../services/calendarRuns";
import type { CalendarItem, CalendarRun } from "../../store/calendar";

/** Runs shown before "Show all": a daily item gains one a day. */
const RECENT_RUNS = 5;

/**
 * One Calendar item: what it does, when it runs next and what each run
 * produced. A run opens its full result, or its live Session while it runs.
 * Links are taps; no ids are shown.
 */
export function CalendarItemSheet({
  item,
  serverId,
  initialRunId,
  error,
  liveSessions,
  onClose,
  onEdit,
  onCancel,
  onRun,
  onOpenSession,
}: {
  item: CalendarItem | null;
  serverId?: string;
  /** Opens straight on this run, as a back-link from its Work or Session does. */
  initialRunId?: string;
  error: string;
  /** Sessions still open on this server, by id. */
  liveSessions: ReadonlySet<string>;
  onClose(): void;
  onEdit(item: CalendarItem): void;
  onCancel(item: CalendarItem): void;
  onRun(item: CalendarItem): void;
  onOpenSession(sessionId: string): void;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const imageOwner = useBrainHostImageOwner(serverId);
  const [runId, setRunId] = useState<string | null>(initialRunId ?? null);
  const [showAll, setShowAll] = useState(false);
  const [instructionOpen, setInstructionOpen] = useState(false);
  // Lists carry each run's first line; the full results come with the item.
  const [full, setFull] = useState<CalendarItem | null>(null);
  const itemId = item?.id;
  useEffect(() => {
    setRunId(initialRunId ?? null);
    setShowAll(false);
    setInstructionOpen(false);
  }, [itemId, initialRunId]);
  useEffect(() => {
    if (!itemId || !serverId || !item || !executes(item)) {
      setFull(null);
      return;
    }
    let live = true;
    wsClient
      .getCalendarItem(serverId, itemId)
      .then((value) => {
        if (live) setFull(value);
      })
      .catch(() => {
        if (live) setFull(null);
      });
    return () => {
      live = false;
    };
  }, [itemId, serverId, item?.updated_at, item?.revision]);
  if (!item) return null;
  const now = new Date();
  const viewerZone = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  const fullRuns = full?.id === item.id ? full.runs : undefined;
  // The newest state of a run comes from the list; its result from the item.
  const runs = calendarRunsNewestFirst({
    runs: mergeRuns(fullRuns, item.runs),
  });
  const selectedRun = runId ? runs.find((candidate) => candidate.id === runId) : undefined;
  const canRun = executes(item) && item.status !== "running" && item.status !== "cancelled";
  const summary = calendarItemSummary(item);
  const next = calendarNextLine(item, now, viewerZone);
  const schedule = [calendarScheduleLine(item, viewerZone), next].filter(Boolean).join(" · ");
  const lastRun = runs[0];
  // An item fails as a whole only when no run carries the reason.
  const itemFailure =
    item.status === "failed" && item.failure_reason && !(lastRun?.status === "failed")
      ? item.failure_reason
      : "";
  const visibleRuns = showAll ? runs : runs.slice(0, RECENT_RUNS);
  return (
    <Modal visible transparent animationType="slide" onRequestClose={selectedRun ? () => setRunId(null) : onClose}>
      <View style={styles.backdrop}>
        <SafeAreaView edges={["bottom", "left", "right"]} style={styles.sheet}>
          <View style={styles.handle} />
          {selectedRun ? (
            <ImageOwnerContext.Provider value={imageOwner}>
            <RunView
              item={item}
              run={selectedRun}
              resultLoaded={Boolean(fullRuns)}
              sessionLive={Boolean(selectedRun.worker_session && liveSessions.has(selectedRun.worker_session))}
              onBack={() => setRunId(null)}
              onClose={onClose}
              onOpenSession={onOpenSession}
            />
            </ImageOwnerContext.Provider>
          ) : (
            <>
              <View style={styles.header}>
                <View style={styles.headerText}>
                  <Text style={styles.title}>{item.title}</Text>
                  <Text style={styles.schedule}>{schedule}</Text>
                </View>
                <Pressable accessibilityRole="button" accessibilityLabel="Close" onPress={onClose} style={styles.iconButton}>
                  <Icon name="close" size={23} color={colors.textPrimary} />
                </Pressable>
              </View>
              <ScrollView contentContainerStyle={styles.content}>
                {summary ? <Text style={styles.summary}>{summary}</Text> : null}
                {item.kind === "event" && item.end_at ? (
                  <Text style={styles.secondary}>
                    Ends {formatDate(item.end_at, DateStyle.dateTime, { timeZone: item.timezone })}
                  </Text>
                ) : null}
                {item.notes ? <Text style={styles.secondary}>{item.notes}</Text> : null}
                {item.action_instruction ? (
                  <View>
                    <Pressable
                      accessibilityRole="button"
                      accessibilityState={{ expanded: instructionOpen }}
                      onPress={() => setInstructionOpen((open) => !open)}
                      style={styles.disclosure}
                    >
                      <Text style={styles.disclosureText}>Instructions</Text>
                      <Icon
                        name={instructionOpen ? "chevron-up" : "chevron-down"}
                        size={16}
                        color={colors.textSecondary}
                      />
                    </Pressable>
                    {instructionOpen ? (
                      <Text selectable style={styles.instruction}>
                        {item.action_instruction}
                      </Text>
                    ) : null}
                  </View>
                ) : null}
                {itemFailure ? (
                  <View style={styles.failure}>
                    <StatusMark status="failed" knockout={colors.surfaceSubtle} />
                    <Text style={styles.failureBody}>{itemFailure}</Text>
                  </View>
                ) : null}
                {runs.length ? (
                  <View style={styles.runs}>
                    <Text style={styles.caption}>Runs</Text>
                    {visibleRuns.map((run) => (
                      <RunRow
                        key={run.id}
                        item={item}
                        run={run}
                        onPress={() =>
                          run.status === "running" && run.worker_session && liveSessions.has(run.worker_session)
                            ? onOpenSession(run.worker_session)
                            : setRunId(run.id)
                        }
                      />
                    ))}
                    {runs.length > visibleRuns.length ? (
                      <Pressable accessibilityRole="button" onPress={() => setShowAll(true)} style={styles.more}>
                        <Text style={styles.moreText}>Show all {runs.length}</Text>
                      </Pressable>
                    ) : null}
                  </View>
                ) : null}
                {error ? <Text style={styles.errorText}>{error}</Text> : null}
              </ScrollView>
              <View style={styles.actions}>
                {canRun ? (
                  <Pressable accessibilityRole="button" onPress={() => void onRun(item)} style={styles.primaryAction}>
                    <Text style={styles.primaryText}>{item.status === "failed" ? "Retry now" : "Run now"}</Text>
                  </Pressable>
                ) : null}
                <Pressable
                  accessibilityRole="button"
                  disabled={item.status === "running"}
                  onPress={() => onEdit(item)}
                  style={styles.secondaryAction}
                >
                  <Text style={styles.secondaryText}>Edit</Text>
                </Pressable>
                <Pressable
                  accessibilityRole="button"
                  disabled={item.status === "running" || item.status === "cancelled"}
                  onPress={() =>
                    Alert.alert("Cancel calendar item?", "It will remain in Calendar as cancelled.", [
                      { text: "Keep", style: "cancel" },
                      { text: "Cancel item", style: "destructive", onPress: () => void onCancel(item) },
                    ])
                  }
                  style={styles.secondaryAction}
                >
                  <Text style={styles.cancelText}>Cancel</Text>
                </Pressable>
              </View>
            </>
          )}
        </SafeAreaView>
      </View>
    </Modal>
  );
}

/** The full results come from get_calendar_item; newer states from the list. */
function mergeRuns(full: CalendarRun[] | undefined, listed: CalendarRun[] | undefined) {
  const byId = new Map<string, CalendarRun>();
  for (const run of full ?? []) byId.set(run.id, run);
  for (const run of listed ?? []) {
    const known = byId.get(run.id);
    byId.set(run.id, known ? { ...known, ...run, result: known.result ?? run.result } : run);
  }
  return [...byId.values()];
}

function RunRow({ item, run, onPress }: { item: CalendarItem; run: CalendarRun; onPress(): void }) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const preview = calendarRunPreview(run);
  const date = calendarRunDate(run, item);
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${date}, ${calendarStatusLabel[run.status]}${preview ? `, ${preview}` : ""}`}
      onPress={onPress}
      style={({ pressed }) => [styles.runRow, pressed && styles.pressed]}
    >
      <View style={styles.runMain}>
        <View style={styles.runMeta}>
          <Text style={styles.runDate}>{date}</Text>
          <CalendarStatus status={run.status} />
        </View>
        {preview ? (
          <Text numberOfLines={1} style={styles.runPreview}>
            {preview}
          </Text>
        ) : null}
      </View>
      <Icon name="chevron-right" size={16} color={colors.textTertiary} />
    </Pressable>
  );
}

function RunView({
  item,
  run,
  resultLoaded,
  sessionLive,
  onBack,
  onClose,
  onOpenSession,
}: {
  item: CalendarItem;
  run: CalendarRun;
  resultLoaded: boolean;
  sessionLive: boolean;
  onBack(): void;
  onClose(): void;
  onOpenSession(sessionId: string): void;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const time = formatDate(run.started_at, DateStyle.time, { timeZone: item.timezone });
  return (
    <>
      <View style={styles.header}>
        <Pressable accessibilityRole="button" accessibilityLabel="All runs" onPress={onBack} style={styles.iconButton}>
          <Icon name="chevron-left" size={22} color={colors.textPrimary} />
        </Pressable>
        <View style={styles.headerText}>
          <Text numberOfLines={1} style={styles.runTitle}>
            {item.title}
          </Text>
          <View style={styles.runMeta}>
            <Text style={styles.runDate}>
              {calendarRunDate(run, item)} · {time}
            </Text>
            <CalendarStatus status={run.status} />
          </View>
        </View>
        <Pressable accessibilityRole="button" accessibilityLabel="Close" onPress={onClose} style={styles.iconButton}>
          <Icon name="close" size={23} color={colors.textPrimary} />
        </Pressable>
      </View>
      <ScrollView contentContainerStyle={styles.runContent}>
        {run.status === "failed" && run.failure_reason ? (
          <View style={[styles.failure, styles.runFailure]}>
            <StatusMark status="failed" knockout={colors.surfaceSubtle} />
            <Text style={styles.failureBody}>{run.failure_reason}</Text>
          </View>
        ) : null}
        {run.status === "completed" ? (
          run.result ? (
            <MarkdownView value={run.result} />
          ) : (
            <Text style={[styles.summary, styles.runPending]}>
              {resultLoaded ? calendarRunPreview(run) : `${calendarRunPreview(run)}…`}
            </Text>
          )
        ) : null}
      </ScrollView>
      {run.worker_session && sessionLive ? (
        <View style={styles.actions}>
          <Pressable
            accessibilityRole="button"
            onPress={() => onOpenSession(run.worker_session!)}
            style={run.status === "running" ? styles.primaryAction : styles.secondaryWide}
          >
            <Text style={run.status === "running" ? styles.primaryText : styles.secondaryText}>Open Session</Text>
          </Pressable>
        </View>
      ) : null}
    </>
  );
}

/** The status word with its Seal & Slip mark; Scheduled stays a plain word. */
export function CalendarStatus({ status }: { status: CalendarItem["status"] }) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const mark = calendarStatusMark[status];
  return (
    <View style={styles.status}>
      {mark ? <StatusMark status={mark} /> : null}
      <Text
        style={[
          styles.statusText,
          mark && mark !== "blocked"
            ? {
                color: workStatusTextInk(mark, {
                  statusReady: colors.statusDone,
                  statusRunning: colors.statusRunning,
                  seal: colors.seal,
                  sealText: colors.sealText,
                  statusWarning: colors.statusWarning,
                  statusFailed: colors.statusFailed,
                  statusBlocked: colors.statusBlocked,
                }),
              }
            : null,
        ]}
      >
        {calendarStatusLabel[status]}
      </Text>
    </View>
  );
}

function createStyles(colors: any) {
  return StyleSheet.create({
    backdrop: { flex: 1, justifyContent: "flex-end", backgroundColor: colors.modalBackdrop },
    // A sheet on the phone; on a wide screen a reading-width column.
    sheet: {
      width: "100%",
      maxWidth: 760,
      alignSelf: "center",
      maxHeight: "88%",
      backgroundColor: colors.bgSurface,
      borderTopLeftRadius: Radii.lg,
      borderTopRightRadius: Radii.lg,
    },
    handle: {
      width: 40,
      height: 4,
      borderRadius: 2,
      backgroundColor: colors.border,
      alignSelf: "center",
      marginTop: 8,
    },
    header: { paddingHorizontal: 16, paddingTop: 12, paddingBottom: 8, flexDirection: "row", alignItems: "flex-start", gap: 8 },
    headerText: { flex: 1, minWidth: 0, gap: 4, paddingTop: 2 },
    title: { ...TypeScale.title, color: colors.textPrimary },
    schedule: { ...TypeScale.compact, color: colors.textSecondary },
    iconButton: { width: 44, height: 44, alignItems: "center", justifyContent: "center", marginTop: -6 },
    content: { paddingHorizontal: 16, paddingBottom: 20, gap: 14 },
    summary: { ...TypeScale.body, color: colors.textPrimary },
    secondary: { ...TypeScale.compact, color: colors.textSecondary },
    disclosure: { minHeight: 36, flexDirection: "row", alignItems: "center", gap: 4, alignSelf: "flex-start" },
    disclosureText: { ...TypeScale.label, color: colors.textSecondary },
    instruction: {
      ...TypeScale.compact,
      color: colors.textSecondary,
      padding: 12,
      borderRadius: Radii.sm,
      backgroundColor: colors.surfaceSubtle,
    },
    failure: {
      padding: 12,
      borderRadius: Radii.sm,
      backgroundColor: colors.surfaceSubtle,
      flexDirection: "row",
      alignItems: "flex-start",
      gap: 8,
    },
    failureBody: { ...TypeScale.compact, color: colors.textSecondary, flex: 1 },
    runs: { gap: 2 },
    caption: { ...TypeScale.label, color: colors.textSecondary, marginBottom: 4 },
    runRow: {
      minHeight: 56,
      paddingVertical: 10,
      flexDirection: "row",
      alignItems: "center",
      gap: 10,
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: colors.borderSubtle,
    },
    pressed: { backgroundColor: colors.surfacePressed },
    runMain: { flex: 1, minWidth: 0, gap: 3 },
    runMeta: { flexDirection: "row", alignItems: "center", gap: 8, flexWrap: "wrap" },
    runDate: { ...TypeScale.caption, color: colors.textSecondary },
    runPreview: { ...TypeScale.compact, color: colors.textPrimary },
    runTitle: { ...TypeScale.heading, color: colors.textPrimary },
    runContent: { paddingBottom: 20 },
    runFailure: { marginHorizontal: 16, marginTop: 8 },
    runPending: { paddingHorizontal: 18, paddingTop: 10, color: colors.textSecondary },
    more: { minHeight: 44, justifyContent: "center", borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: colors.borderSubtle },
    moreText: { ...TypeScale.label, color: colors.accent },
    status: { flexDirection: "row", alignItems: "center", gap: 4 },
    statusText: { ...TypeScale.caption, color: colors.textTertiary },
    errorText: { ...TypeScale.compact, color: colors.statusFailed },
    actions: {
      padding: 16,
      flexDirection: "row",
      gap: 8,
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: colors.borderSubtle,
    },
    primaryAction: {
      flex: 1,
      minHeight: 46,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.accent,
      borderRadius: Radii.sm,
    },
    primaryText: { ...TypeScale.label, color: colors.textOnAccent },
    secondaryAction: {
      minWidth: 70,
      minHeight: 46,
      alignItems: "center",
      justifyContent: "center",
      paddingHorizontal: 12,
      borderRadius: Radii.sm,
      backgroundColor: colors.surfaceSubtle,
    },
    secondaryWide: {
      flex: 1,
      minHeight: 46,
      alignItems: "center",
      justifyContent: "center",
      borderRadius: Radii.sm,
      backgroundColor: colors.surfaceSubtle,
    },
    secondaryText: { ...TypeScale.label, color: colors.textPrimary },
    cancelText: { ...TypeScale.label, color: colors.statusFailed },
  });
}
