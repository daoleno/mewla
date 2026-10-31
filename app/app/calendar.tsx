import { DateStyle, formatDate } from "../services/dateFormat";
import React, {
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  AppState,
  KeyboardAvoidingView,
  Linking,
  Modal,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from "react-native";
import { useLocalSearchParams, useNavigation, useRouter } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import {
  ContentWidth,
  Radii,
  Spacing,
  TypeScale,
  Typography,
  useAppColors,
} from "../constants/tokens";
import {
  calendarDateKey,
  calendarStatusLabel,
  groupAgenda,
  itemInstant,
  kindLabel,
  viewerTimezone,
} from "../services/calendarPresentation";
import { calendarActiveRun, calendarItemSummary } from "../services/calendarRuns";
import { terminalRouteParams } from "../services/workerRouteId";
import { selectCurrentServerItems } from "../services/currentServerSelection";
import { useWorkerList } from "../store/workers";
import {
  CalendarItemSheet,
  CalendarStatus,
} from "../components/calendar/CalendarItemSheet";
import {
  formatResolvedInstant,
  localFieldsFromInstant,
  resolveLocalDateTime,
  type LocalDateTimeResolution,
} from "../services/calendarTime";
import {
  calendarNotificationState,
  requestCalendarNotifications,
  syncCalendarNotifications,
  type CalendarNotificationState,
} from "../services/calendarNotifications";
import { wsClient } from "../services/websocket";
import {
  selectCurrentServerCalendar,
  useCalendar,
  type CalendarItem,
  type CalendarKind,
  type CalendarRecurrence,
} from "../store/calendar";
import { useBrain } from "../store/brain";
import { useCurrentServer } from "../store/currentServer";
import { Icon } from "../components/icons/Icon";
import { IconButton } from "../components/ui/IconButton";
import { InlineNotice } from "../components/ui/InlineNotice";
import { useDesktopWeb } from "../components/navigation/useDesktopWeb";

type ServerItem = CalendarItem & { serverId: string; serverName: string };
const CALENDAR_FONT_SCALE_MAX = 1.25;
type CalendarScreenProps = {
  notificationStateOverride?: CalendarNotificationState;
  initialError?: string;
  loading?: boolean;
  serverIdOverride?: string;
};

export default function CalendarScreen(props: CalendarScreenProps = {}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const router = useRouter();
  const navigation = useNavigation();
  const params = useLocalSearchParams<{ id?: string; serverId?: string; run?: string }>();
  const state = useCalendar();
  const { currentServerId } = useCurrentServer();
  const scopedServerId = props.serverIdOverride ?? currentServerId;
  const viewerZone = useMemo(() => viewerTimezone(), []);
  const now = new Date();
  const todayKey = calendarDateKey(now, viewerZone);
  // Desktop web has the room: the month is always there, so the header needs
  // no toggle. The phone opens it from the header, and the button stays lit
  // while it is open.
  const desktop = useDesktopWeb();
  const [monthToggled, setMonthToggled] = useState(false);
  const monthExpanded = desktop || monthToggled;
  const [month, setMonth] = useState(() => new Date());
  const [selectedDate, setSelectedDate] = useState<string | null>(null);
  // The open item by id, so its runs move on while the sheet is open.
  const [selected, setSelected] = useState<{ id: string; runId?: string } | null>(null);
  const [editing, setEditing] = useState<ServerItem | "new" | null>(null);
  const [error, setError] = useState(props.initialError ?? "");
  const [notificationState, setNotificationState] =
    useState<CalendarNotificationState>(
      props.notificationStateOverride ?? "undetermined",
    );
  const serverScopeRef = useRef(scopedServerId);
  const workers = useWorkerList();
  const liveSessions = useMemo(
    () => new Set(selectCurrentServerItems(workers, scopedServerId).map((worker) => worker.id)),
    [workers, scopedServerId],
  );
  const activeServer = selectCurrentServerCalendar(state, scopedServerId);
  const items = useMemo(
    () =>
      activeServer?.items.map((item) => ({
        ...item,
        serverId: activeServer.serverId,
        serverName: activeServer.serverName,
      })) ?? [],
    [activeServer],
  );
  useEffect(() => {
    if (serverScopeRef.current === scopedServerId) return;
    serverScopeRef.current = scopedServerId;
    setSelected(null);
    setEditing(null);
    setError("");
  }, [scopedServerId]);
  useLayoutEffect(() => {
    navigation.setOptions({
      headerShown: true,
      headerRight: () => (
        <View style={styles.calendarHeaderActions}>
          {desktop ? null : (
            <IconButton
              icon={monthToggled ? "calendar-fill" : "calendar"}
              size={40}
              iconSize={20}
              tone={monthToggled ? "tinted" : "default"}
              color={monthToggled ? undefined : colors.textPrimary}
              accessibilityLabel={monthToggled ? "Hide month" : "Show month"}
              accessibilityState={{ expanded: monthToggled }}
              onPress={() => {
                setMonthToggled((expanded) => {
                  if (expanded) setSelectedDate(null);
                  return !expanded;
                });
              }}
            />
          )}
          <IconButton
            icon="add"
            size={40}
            iconSize={20}
            color={activeServer ? colors.textPrimary : colors.disabledText}
            accessibilityLabel="Add calendar item"
            disabled={!activeServer}
            onPress={() => setEditing("new")}
          />
        </View>
      ),
    });
  }, [activeServer, colors, desktop, monthToggled, navigation, styles]);
  useEffect(() => {
    if (props.notificationStateOverride) return;
    let active = true;
    const refreshPermission = async () => {
      const next = await calendarNotificationState();
      if (!active) return;
      setNotificationState(next);
      if (next === "granted" && activeServer) {
        await syncCalendarNotifications(
          activeServer.serverId,
          activeServer.items,
        );
      }
    };
    void refreshPermission();
    const subscription = AppState.addEventListener("change", (next) => {
      if (next === "active") void refreshPermission();
    });
    return () => {
      active = false;
      subscription.remove();
    };
  }, [activeServer, props.notificationStateOverride]);
  // A back-link opens its item (and run) once; closing the sheet stays closed
  // as the item changes.
  const openedLinkRef = useRef("");
  useEffect(() => {
    if (!params.id) return;
    const link = `${params.serverId ?? ""}:${params.id}:${params.run ?? ""}`;
    if (openedLinkRef.current === link) return;
    const found = items.find(
      (item) =>
        item.id === params.id &&
        (!params.serverId || item.serverId === params.serverId),
    );
    if (!found) return;
    openedLinkRef.current = link;
    setError("");
    setSelected({ id: found.id, runId: params.run || undefined });
  }, [items, params.id, params.run, params.serverId]);
  const selectedItem = selected ? items.find((item) => item.id === selected.id) ?? null : null;
  const openSession = (sessionId: string) => {
    if (!scopedServerId) return;
    setSelected(null);
    router.push({
      pathname: "/terminal/[id]",
      params: terminalRouteParams(sessionId, scopedServerId),
    });
  };
  const visible = items.filter((item) => item.status !== "cancelled");
  const agendaItems = selectedDate
    ? visible.filter(
        (item) =>
          calendarDateKey(itemInstant(item), viewerZone) === selectedDate,
      )
    : visible;
  const sections = selectedDate
    ? [{ title: selectedDate, data: agendaItems }]
    : groupAgenda(agendaItems, now, viewerZone);
  const openItem = (item: ServerItem) => {
    // Running now: its live Session is what there is to see.
    const session = item.status === "running" ? calendarActiveRun(item)?.worker_session : undefined;
    if (session && liveSessions.has(session)) {
      openSession(session);
      return;
    }
    setError("");
    setSelected({ id: item.id });
  };
  const actionError = (value: unknown) =>
    setError(
      value instanceof Error ? value.message : "Calendar action failed.",
    );
  const enableNotifications = async () => {
    if (notificationState === "denied") {
      try {
        await Linking.openSettings();
      } catch (value) {
        actionError(value);
      }
      return;
    }
    if (notificationState === "unavailable") return;
    const next = await requestCalendarNotifications();
    if (next !== "granted") {
      setNotificationState(next);
      return;
    }
    try {
      if (activeServer) {
        await syncCalendarNotifications(
          activeServer.serverId,
          activeServer.items,
        );
      }
      setNotificationState("granted");
    } catch (value) {
      setNotificationState("unavailable");
      actionError(value);
    }
  };
  const cancel = async (item: ServerItem) => {
    try {
      await wsClient.cancelCalendarItem(item.serverId, item.id, item.revision);
      setSelected(null);
    } catch (e) {
      actionError(e);
    }
  };
  const runNow = async (item: ServerItem) => {
    try {
      await wsClient.runCalendarItem(item.serverId, item.id);
    } catch (e) {
      actionError(e);
    }
  };
  return (
    <SafeAreaView edges={["bottom"]} style={styles.screen}>
      {notificationState !== "granted" ? (
        <InlineNotice
          icon="notifications-off"
          title={
            notificationState === "denied"
              ? "Reminders disabled"
              : notificationState === "unavailable"
                ? "Reminders unavailable"
                : "Reminders not enabled"
          }
          action={
            notificationState === "unavailable"
              ? undefined
              : {
                  label: notificationState === "denied" ? "Settings" : "Enable",
                  accessibilityLabel:
                    notificationState === "denied" ? "Open notification settings" : "Enable reminders",
                  onPress: () => void enableNotifications(),
                }
          }
          style={styles.notice}
        />
      ) : null}
      {error ? <InlineNotice tone="danger" title={error} style={styles.notice} /> : null}
      <ScrollView contentContainerStyle={[styles.content, desktop ? styles.contentDesktop : null]}>
        {monthExpanded ? (
          <View style={desktop ? styles.monthPane : undefined}>
            <MonthNavigator
              items={visible}
              month={month}
              selectedDate={selectedDate}
              timeZone={viewerZone}
              todayKey={todayKey}
              onChangeMonth={setMonth}
              onSelectDate={(date, key) => {
                setSelectedDate(key);
                if (
                  date.getMonth() !== month.getMonth() ||
                  date.getFullYear() !== month.getFullYear()
                ) {
                  setMonth(new Date(date.getFullYear(), date.getMonth(), 1));
                }
              }}
            />
          </View>
        ) : null}
        <View style={styles.agendaPane}>
          <AgendaHeading
            now={now}
            selectedDate={selectedDate}
            timeZone={viewerZone}
            onToday={() => {
              setSelectedDate(null);
              setMonth(new Date());
            }}
          />
          {props.loading ? (
            <View
              accessibilityRole="progressbar"
              accessibilityLabel="Loading calendar"
              style={styles.agendaStatus}
            >
              <Icon name="sync" size={20} color={colors.accent} />
              <Text style={styles.emptyTitle}>Loading calendar…</Text>
            </View>
          ) : sections.length ? (
            sections.map((section) => (
              <View key={section.title} style={styles.section}>
                {selectedDate || section.title === "Today" ? null : (
                  <Text style={styles.sectionTitle}>{section.title}</Text>
                )}
                {section.data.map((raw) => (
                  <CalendarRow
                    key={`${(raw as ServerItem).serverId}:${raw.id}`}
                    item={raw as ServerItem}
                    onPress={() => openItem(raw as ServerItem)}
                  />
                ))}
              </View>
            ))
          ) : (
            <EmptyState
              connected={Boolean(activeServer)}
              dateFiltered={selectedDate !== null}
            />
          )}
        </View>
      </ScrollView>
      <CalendarItemSheet
        item={selectedItem}
        serverId={selectedItem?.serverId}
        initialRunId={selected?.runId}
        error={error}
        liveSessions={liveSessions}
        onClose={() => {
          setSelected(null);
          setError("");
        }}
        onEdit={(item) => {
          setSelected(null);
          setEditing(item as ServerItem);
        }}
        onCancel={(item) => cancel(item as ServerItem)}
        onRun={(item) => runNow(item as ServerItem)}
        onOpenSession={openSession}
      />
      <EditorModal
        value={editing}
        serverId={
          editing === "new"
            ? activeServer?.serverId
            : (editing as ServerItem | null)?.serverId
        }
        onClose={() => setEditing(null)}
        onSaved={() => setEditing(null)}
        onError={actionError}
      />
    </SafeAreaView>
  );
}

function AgendaHeading({
  now,
  selectedDate,
  timeZone,
  onToday,
}: {
  now: Date;
  selectedDate: string | null;
  timeZone: string;
  onToday(): void;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const date = formatDate(selectedDate ? dateFromKey(selectedDate) : now, DateStyle.longDay, { timeZone });
  const time = formatDate(now, DateStyle.time, { timeZone, timeZoneName: "short" });
  const isToday =
    selectedDate === null || selectedDate === calendarDateKey(now, timeZone);
  return (
    <View
      accessibilityRole="header"
      accessibilityLabel={isToday ? `Today, ${date}, ${time}` : date}
      style={styles.agendaHeading}
    >
      <View style={styles.agendaTitleRow}>
        <Text
          maxFontSizeMultiplier={CALENDAR_FONT_SCALE_MAX}
          numberOfLines={2}
          style={styles.agendaTitle}
        >
          {isToday ? "Today" : date}
        </Text>
        {!isToday ? (
          <Pressable
            accessibilityRole="button"
            accessibilityLabel="Return to today"
            onPress={onToday}
            style={styles.todayButton}
          >
            <Text style={styles.todayButtonText}>Today</Text>
          </Pressable>
        ) : null}
      </View>
      {isToday ? (
        <Text
          maxFontSizeMultiplier={CALENDAR_FONT_SCALE_MAX}
          numberOfLines={2}
          style={styles.agendaAnchor}
        >
          {date} · {time}
        </Text>
      ) : null}
    </View>
  );
}

function CalendarRow({ item, onPress }: { item: ServerItem; onPress(): void }) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const summary = calendarItemSummary(item);
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${item.title}, ${summary || kindLabel[item.kind]}, ${calendarStatusLabel[item.status]}`}
      onPress={onPress}
      style={({ pressed }) => [styles.row, pressed && styles.rowPressed]}
    >
      <View style={styles.timeRail}>
        <Text style={styles.rowTime}>
          {formatDate(itemInstant(item), DateStyle.time, { timeZone: item.timezone })}
        </Text>
        <View style={styles.kindRail} />
      </View>
      <View style={styles.rowMain}>
        <Text style={styles.rowTitle} numberOfLines={2}>
          {item.title}
        </Text>
        <View style={styles.rowMeta}>
          <CalendarStatus status={item.status} />
          <Text style={styles.metaDot}>·</Text>
          <Text style={[styles.metaText, styles.metaSummary]} numberOfLines={1}>
            {summary || kindLabel[item.kind]}
          </Text>
        </View>
      </View>
      <Icon name="chevron-right" size={17} color={colors.textTertiary} />
    </Pressable>
  );
}
function MonthNavigator({
  items,
  month,
  selectedDate,
  timeZone,
  todayKey,
  onChangeMonth,
  onSelectDate,
}: {
  items: ServerItem[];
  month: Date;
  selectedDate: string | null;
  timeZone: string;
  todayKey: string;
  onChangeMonth(date: Date): void;
  onSelectDate(date: Date, key: string): void;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const counts = useMemo(() => {
    const next = new Map<string, number>();
    for (const item of items) {
      const key = calendarDateKey(itemInstant(item), timeZone);
      next.set(key, (next.get(key) ?? 0) + 1);
    }
    return next;
  }, [items, timeZone]);
  return (
    <View
      accessibilityLabel="Month date navigator"
      style={styles.monthNavigator}
    >
      <MonthHeader month={month} onChange={onChangeMonth} />
      <View style={styles.weekdays}>
        {["S", "M", "T", "W", "T", "F", "S"].map((label, index) => (
          <Text key={`${label}${index}`} style={styles.weekday}>
            {label}
          </Text>
        ))}
      </View>
      <View style={styles.monthGrid}>
        {monthGrid(month).map((date) => {
          const key = dayKey(date);
          const count = counts.get(key) ?? 0;
          const inMonth = date.getMonth() === month.getMonth();
          const selected = key === selectedDate;
          const today = key === todayKey;
          return (
            <Pressable
              key={key}
              accessibilityRole="button"
              accessibilityLabel={`${formatDate(date, DateStyle.day)}, ${count} items`}
              accessibilityState={{ selected }}
              onPress={() => onSelectDate(date, key)}
              style={[
                styles.dayCell,
                today && styles.dayToday,
                selected && styles.daySelected,
              ]}
            >
              <Text
                maxFontSizeMultiplier={CALENDAR_FONT_SCALE_MAX}
                style={[
                  styles.dayNumber,
                  !inMonth && styles.dayMuted,
                  selected && styles.dayNumberSelected,
                ]}
              >
                {date.getDate()}
              </Text>
              {count ? <View style={styles.dayDot} /> : null}
            </Pressable>
          );
        })}
      </View>
    </View>
  );
}
function MonthHeader({
  month,
  onChange,
}: {
  month: Date;
  onChange(date: Date): void;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  return (
    <View style={styles.monthHeader}>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel="Previous month"
        onPress={() =>
          onChange(new Date(month.getFullYear(), month.getMonth() - 1, 1))
        }
        style={styles.iconButton}
      >
        <Icon name="chevron-left" size={20} color={colors.textPrimary} />
      </Pressable>
      <Text style={styles.monthTitle}>
        {formatDate(month, DateStyle.monthYear)}
      </Text>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel="Next month"
        onPress={() =>
          onChange(new Date(month.getFullYear(), month.getMonth() + 1, 1))
        }
        style={styles.iconButton}
      >
        <Icon name="chevron-right" size={20} color={colors.textPrimary} />
      </Pressable>
    </View>
  );
}
function EmptyState({
  connected,
  dateFiltered,
}: {
  connected: boolean;
  dateFiltered: boolean;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  return (
    <View style={styles.empty}>
      <Icon
        name="calendar-blank"
        size={19}
        color={colors.textTertiary}
      />
      <Text
        maxFontSizeMultiplier={CALENDAR_FONT_SCALE_MAX}
        style={styles.emptyTitle}
      >
        {connected
          ? dateFiltered
            ? "Nothing planned for this date"
            : "Nothing planned"
          : "Calendar offline"}
      </Text>
    </View>
  );
}

function EditorModal({
  value,
  serverId,
  onClose,
  onSaved,
  onError,
}: {
  value: ServerItem | "new" | null;
  serverId?: string;
  onClose(): void;
  onSaved(): void;
  onError(error: unknown): void;
}) {
  const colors = useAppColors();
  const { state: brainState } = useBrain();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const existing = value && value !== "new" ? value : null;
  const [title, setTitle] = useState("");
  const [kind, setKind] = useState<CalendarKind>("reminder");
  const [atDate, setAtDate] = useState("");
  const [atTime, setAtTime] = useState("");
  const [endDate, setEndDate] = useState("");
  const [endTime, setEndTime] = useState("");
  const [atOccurrence, setAtOccurrence] = useState<0 | 1 | null>(null);
  const [endOccurrence, setEndOccurrence] = useState<0 | 1 | null>(null);
  const [timezone, setTimezone] = useState(
    Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
  );
  const [recurrence, setRecurrence] = useState<CalendarRecurrence>("none");
  const [notes, setNotes] = useState("");
  const [instruction, setInstruction] = useState("");
  const [summary, setSummary] = useState("");
  const [saving, setSaving] = useState(false);
  const editorSessionId =
    value === "new"
      ? "new"
      : value
        ? `${value.serverId}:${value.id}`
        : null;
  useEffect(() => {
    if (!value) return;
    const initial = existing
      ? itemInstant(existing)
      : new Date(Date.now() + 3600000).toISOString();
    setTitle(existing?.title ?? "");
    setKind(existing?.kind ?? "reminder");
    const zone =
      existing?.timezone ??
      Intl.DateTimeFormat().resolvedOptions().timeZone ??
      "UTC";
    const atFields = localFieldsFromInstant(initial, zone);
    const endFields = localFieldsFromInstant(
      existing?.end_at ?? new Date(Date.parse(initial) + 3600000).toISOString(),
      zone,
    );
    setAtDate(atFields.date);
    setAtTime(atFields.time);
    setEndDate(endFields.date);
    setEndTime(endFields.time);
    setAtOccurrence(null);
    setEndOccurrence(null);
    setTimezone(zone);
    setRecurrence(existing?.recurrence ?? "none");
    setNotes(existing?.notes ?? "");
    setInstruction(existing?.action_instruction ?? "");
    setSummary(existing?.summary ?? "");
  }, [editorSessionId]);
  const atResolution = useMemo(
    () => resolveLocalDateTime(atDate, atTime, timezone),
    [atDate, atTime, timezone],
  );
  const endResolution = useMemo(
    () => resolveLocalDateTime(endDate, endTime, timezone),
    [endDate, endTime, timezone],
  );
  if (!value) return null;
  const atInstant = chosenInstant(atResolution, atOccurrence);
  const endInstant = chosenInstant(endResolution, endOccurrence);
  const timeValidation =
    atResolution.status === "resolved" ||
    (atResolution.status === "ambiguous" && atOccurrence !== null)
      ? kind !== "event" ||
        endResolution.status === "resolved" ||
        (endResolution.status === "ambiguous" && endOccurrence !== null)
        ? kind !== "event" ||
          !atInstant ||
          !endInstant ||
          endInstant > atInstant
          ? ""
          : "Event end must be after its start."
        : endResolution.message
      : atResolution.message;
  const save = async () => {
    if (!serverId) return;
    setSaving(true);
    try {
      if (!title.trim()) throw new Error("Title is required.");
      if (!atInstant) throw new Error(resolutionMessage(atResolution));
      if (kind === "event" && !endInstant)
        throw new Error(resolutionMessage(endResolution));
      if (kind === "event" && endInstant! <= atInstant)
        throw new Error("Event end must be after its start.");
      const sourceThreadId = existing?.source_thread_id ??
        (serverId ? brainState.byServer[serverId]?.chat_thread_id : undefined);
      if (kind === "scheduled_action" && !sourceThreadId) {
        throw new Error("Scheduled Work requires an active Brain conversation on this server.");
      }
      const payload: any = {
        ...(existing ?? {}),
        title: title.trim(),
        kind,
        timezone: timezone.trim(),
        recurrence,
        notes: notes.trim(),
        summary: kind === "scheduled_action" ? summary.trim() : undefined,
        action_instruction:
          kind === "scheduled_action" ? instruction.trim() : undefined,
        source_thread_id:
          kind === "scheduled_action" ? sourceThreadId : undefined,
      };
      // Runs are the daemon's history, never part of an edit.
      delete payload.runs;
      delete payload.start_at;
      delete payload.end_at;
      delete payload.notify_at;
      delete payload.due_at;
      if (kind === "event") {
        payload.start_at = atInstant;
        payload.end_at = endInstant;
      } else if (kind === "reminder") payload.notify_at = atInstant;
      else payload.due_at = atInstant;
      if (existing) await wsClient.updateCalendarItem(serverId, payload);
      else await wsClient.createCalendarItem(serverId, payload);
      onSaved();
    } catch (e) {
      onError(e);
    } finally {
      setSaving(false);
    }
  };
  return (
    <Modal visible transparent animationType="slide" onRequestClose={onClose}>
      <KeyboardAvoidingView
        behavior={Platform.OS === "ios" ? "padding" : "height"}
        style={styles.modalBackdrop}
      >
        <SafeAreaView edges={["bottom", "left", "right"]} style={styles.editor}>
          <View style={styles.editorHeader}>
            <Pressable onPress={onClose} style={styles.headerAction}>
              <Text style={styles.secondaryText}>Cancel</Text>
            </Pressable>
            <Text style={styles.editorTitle}>
              {existing ? "Edit item" : "New item"}
            </Text>
            <Pressable
              disabled={saving || !serverId}
              onPress={() => void save()}
              style={styles.headerAction}
            >
              <Text style={styles.saveText}>{saving ? "Saving…" : "Save"}</Text>
            </Pressable>
          </View>
          <ScrollView
            style={styles.editorScroll}
            keyboardShouldPersistTaps="handled"
            keyboardDismissMode="on-drag"
            contentContainerStyle={styles.form}
          >
            <Field
              label="Title"
              value={title}
              onChangeText={setTitle}
              placeholder="What is the commitment?"
            />
            <Text style={styles.fieldLabel}>Kind</Text>
            <View style={styles.chips}>
              {(
                [
                  "event",
                  "reminder",
                  "deadline",
                  "scheduled_action",
                ] as CalendarKind[]
              ).map((option) => (
                <Chip
                  key={option}
                  selected={kind === option}
                  label={kindLabel[option]}
                  onPress={() => setKind(option)}
                />
              ))}
            </View>
            <Text style={styles.fieldLabel}>
              {kind === "event"
                ? "Starts"
                : kind === "reminder"
                  ? "Notify at"
                  : "Due at"}
            </Text>
            <View style={styles.dateTimeRow}>
              <Field
                label="Local date"
                value={atDate}
                onChangeText={(text) => {
                  setAtDate(text);
                  setAtOccurrence(null);
                }}
                placeholder="2026-07-14"
                autoCapitalize="none"
                keyboardType="numbers-and-punctuation"
                containerStyle={styles.dateField}
              />
              <Field
                label="Local time"
                value={atTime}
                onChangeText={(text) => {
                  setAtTime(text);
                  setAtOccurrence(null);
                }}
                placeholder="18:20"
                autoCapitalize="none"
                keyboardType="numbers-and-punctuation"
                containerStyle={styles.timeField}
              />
            </View>
            <AmbiguityChoice
              resolution={atResolution}
              selected={atOccurrence}
              onSelect={setAtOccurrence}
              timezone={timezone}
            />
            {kind === "event" ? (
              <>
                <Text style={styles.fieldLabel}>Ends</Text>
                <View style={styles.dateTimeRow}>
                  <Field
                    label="Local date"
                    value={endDate}
                    onChangeText={(text) => {
                      setEndDate(text);
                      setEndOccurrence(null);
                    }}
                    placeholder="2026-07-14"
                    autoCapitalize="none"
                    keyboardType="numbers-and-punctuation"
                    containerStyle={styles.dateField}
                  />
                  <Field
                    label="Local time"
                    value={endTime}
                    onChangeText={(text) => {
                      setEndTime(text);
                      setEndOccurrence(null);
                    }}
                    placeholder="19:20"
                    autoCapitalize="none"
                    keyboardType="numbers-and-punctuation"
                    containerStyle={styles.timeField}
                  />
                </View>
                <AmbiguityChoice
                  resolution={endResolution}
                  selected={endOccurrence}
                  onSelect={setEndOccurrence}
                  timezone={timezone}
                />
              </>
            ) : null}
            <Field
              label="Timezone"
              value={timezone}
              onChangeText={setTimezone}
              placeholder="Asia/Shanghai"
              autoCapitalize="none"
            />
            <Text style={styles.fieldLabel}>Repeats</Text>
            <View style={styles.chips}>
              {(
                ["none", "daily", "weekly", "weekdays"] as CalendarRecurrence[]
              ).map((option) => (
                <Chip
                  key={option}
                  selected={recurrence === option}
                  label={option[0].toUpperCase() + option.slice(1)}
                  onPress={() => setRecurrence(option)}
                />
              ))}
            </View>
            <Field
              label="Notes"
              value={notes}
              onChangeText={setNotes}
              multiline
              placeholder="Optional context"
            />
            {kind === "scheduled_action" ? (
              <Field
                label="Summary"
                value={summary}
                onChangeText={setSummary}
                maxLength={120}
                placeholder="What it does, in one line"
              />
            ) : null}
            {kind === "scheduled_action" ? (
              <Field
                label="Action instruction"
                value={instruction}
                onChangeText={setInstruction}
                multiline
                placeholder="What should Mewla do when the time comes?"
              />
            ) : null}
            <View style={styles.resolvedCard}>
              <Text style={styles.resolvedLabel}>Resolved time</Text>
              <Text
                style={
                  timeValidation ? styles.validationError : styles.resolved
                }
              >
                {timeValidation ||
                  (atInstant
                    ? formatResolvedInstant(atInstant, timezone)
                    : resolutionMessage(atResolution))}
              </Text>
              {!timeValidation && kind === "event" && endInstant ? (
                <Text style={styles.resolved}>
                  Ends {formatResolvedInstant(endInstant, timezone)}
                </Text>
              ) : null}
            </View>
          </ScrollView>
        </SafeAreaView>
      </KeyboardAvoidingView>
    </Modal>
  );
}
const Field = React.memo(function Field(
  props: React.ComponentProps<typeof TextInput> & {
    label: string;
    containerStyle?: object;
  },
) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const { label, containerStyle, ...input } = props;
  return (
    <View style={[styles.field, containerStyle]}>
      <Text style={styles.fieldLabel}>{label}</Text>
      <TextInput
        {...input}
        accessibilityLabel={label}
        placeholderTextColor={colors.textTertiary}
        style={[
          styles.input,
          input.multiline ? styles.multiline : styles.singleLine,
        ]}
      />
    </View>
  );
});
function AmbiguityChoice({
  resolution,
  selected,
  onSelect,
  timezone,
}: {
  resolution: LocalDateTimeResolution;
  selected: 0 | 1 | null;
  onSelect(value: 0 | 1): void;
  timezone: string;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  if (resolution.status !== "ambiguous") return null;
  return (
    <View style={styles.ambiguity}>
      <Text style={styles.validationError}>{resolution.message}</Text>
      <View style={styles.chips}>
        {resolution.instants.map((instant, index) => (
          <Chip
            key={instant}
            selected={selected === index}
            label={`${index === 0 ? "First" : "Second"} · ${formatDate(instant, DateStyle.time, { timeZone: timezone, timeZoneName: "short" })}`}
            onPress={() => onSelect(index as 0 | 1)}
          />
        ))}
      </View>
    </View>
  );
}
function Chip({
  selected,
  label,
  onPress,
}: {
  selected: boolean;
  label: string;
  onPress(): void;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  return (
    <Pressable
      onPress={onPress}
      style={[styles.chip, selected && styles.chipSelected]}
    >
      <Text style={[styles.chipText, selected && styles.chipTextSelected]}>
        {label}
      </Text>
    </Pressable>
  );
}
function chosenInstant(
  resolution: LocalDateTimeResolution,
  occurrence: 0 | 1 | null,
) {
  if (resolution.status === "resolved") return resolution.instant;
  if (resolution.status === "ambiguous" && occurrence !== null)
    return resolution.instants[occurrence];
  return null;
}
function resolutionMessage(resolution: LocalDateTimeResolution) {
  return resolution.status === "resolved"
    ? "Time resolved."
    : resolution.message;
}
function dayKey(date: Date) {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}
function dateFromKey(key: string) {
  return new Date(`${key}T12:00:00`);
}
function monthGrid(month: Date) {
  const first = new Date(month.getFullYear(), month.getMonth(), 1);
  const start = new Date(first);
  start.setDate(1 - first.getDay());
  return Array.from({ length: 42 }, (_, index) => {
    const d = new Date(start);
    d.setDate(start.getDate() + index);
    return d;
  });
}

function createStyles(colors: any) {
  return StyleSheet.create({
    screen: { flex: 1, backgroundColor: colors.bgPrimary },
    calendarHeaderActions: { flexDirection: "row", alignItems: "center", gap: Spacing.sm },
    // Notice and agenda share one centered column on wide screens.
    notice: {
      width: "100%",
      maxWidth: ContentWidth.wide - 2 * Spacing.lg,
      alignSelf: "center",
      marginHorizontal: Spacing.lg,
      marginTop: Spacing.sm,
    },
    errorText: { ...TypeScale.compact, color: colors.statusFailed },
    content: {
      width: "100%",
      maxWidth: ContentWidth.wide,
      alignSelf: "center",
      paddingHorizontal: Spacing.lg,
      paddingTop: Spacing.md,
      paddingBottom: 40,
      gap: Spacing.md,
    },
    // Desktop web: the month beside the agenda, as a desktop calendar is laid out.
    contentDesktop: {
      flexDirection: "row",
      alignItems: "flex-start",
      gap: Spacing.xl,
    },
    monthPane: { width: 320 },
    agendaPane: { flex: 1, minWidth: 0, gap: Spacing.md },
    agendaHeading: { gap: 2 },
    agendaTitleRow: {
      minHeight: 44,
      flexDirection: "row",
      alignItems: "center",
      gap: Spacing.sm,
    },
    agendaTitle: { ...TypeScale.heading, flex: 1, color: colors.textPrimary },
    agendaAnchor: { ...TypeScale.caption, color: colors.textSecondary },
    todayButton: {
      minWidth: 56,
      minHeight: 44,
      paddingHorizontal: Spacing.sm,
      alignItems: "center",
      justifyContent: "center",
      borderRadius: Radii.sm,
      backgroundColor: colors.surfaceSubtle,
    },
    todayButtonText: { ...TypeScale.label, color: colors.accent },
    agendaStatus: {
      minHeight: 64,
      paddingVertical: Spacing.md,
      flexDirection: "row",
      alignItems: "center",
      gap: Spacing.sm,
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: colors.borderSubtle,
    },
    section: { gap: 4 },
    sectionTitle: {
      ...TypeScale.heading,
      color: colors.textPrimary,
      marginBottom: 8,
    },
    row: {
      minHeight: 72,
      paddingVertical: 10,
      paddingHorizontal: 8,
      flexDirection: "row",
      alignItems: "center",
      gap: 12,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    rowPressed: { backgroundColor: colors.surfacePressed },
    timeRail: {
      width: 68,
      alignSelf: "stretch",
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
    },
    rowTime: { ...TypeScale.caption, color: colors.textSecondary, width: 58 },
    // A plain hairline between time and title; state lives in the mark.
    kindRail: {
      width: StyleSheet.hairlineWidth * 2,
      alignSelf: "stretch",
      backgroundColor: colors.border,
    },
    status: { flexDirection: "row", alignItems: "center", gap: 4 },
    rowMain: { flex: 1, gap: 5 },
    rowTitle: { ...TypeScale.body, color: colors.textPrimary },
    meta: {
      flexDirection: "row",
      alignItems: "center",
      flexWrap: "wrap",
      gap: 4,
    },
    metaText: { ...TypeScale.caption, color: colors.textTertiary },
    metaSummary: { flexShrink: 1 },
    // One line: the state, then what the item does, cut to fit.
    rowMeta: { flexDirection: "row", alignItems: "center", gap: 4 },
    metaDot: { color: colors.textTertiary },
    empty: {
      minHeight: 52,
      paddingVertical: Spacing.sm,
      flexDirection: "row",
      alignItems: "center",
      gap: Spacing.sm,
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: colors.borderSubtle,
    },
    emptyTitle: { ...TypeScale.compact, color: colors.textSecondary },
    emptyBody: {
      ...TypeScale.body,
      color: colors.textTertiary,
      textAlign: "center",
    },
    monthHeader: {
      height: 48,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
    },
    monthNavigator: {
      paddingBottom: Spacing.sm,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    monthTitle: { ...TypeScale.heading, color: colors.textPrimary },
    iconButton: {
      width: 44,
      height: 44,
      alignItems: "center",
      justifyContent: "center",
    },
    weekdays: { flexDirection: "row" },
    weekday: {
      ...TypeScale.micro,
      color: colors.textTertiary,
      width: "14.285%",
      textAlign: "center",
    },
    monthGrid: { flexDirection: "row", flexWrap: "wrap" },
    dayCell: {
      width: "14.285%",
      height: 52,
      alignItems: "center",
      justifyContent: "center",
      borderRadius: Radii.sm,
      gap: 3,
    },
    daySelected: { backgroundColor: colors.surfaceSubtle },
    dayToday: {
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.accent,
    },
    dayNumber: { ...TypeScale.compact, color: colors.textPrimary },
    dayNumberSelected: { color: colors.accent },
    dayMuted: { color: colors.textTertiary },
    dayDot: {
      width: 4,
      height: 4,
      borderRadius: 2,
      backgroundColor: colors.accent,
    },
    modalBackdrop: {
      flex: 1,
      justifyContent: "flex-end",
      backgroundColor: colors.modalBackdrop,
    },
    secondaryText: { ...TypeScale.label, color: colors.textPrimary },
    editor: {
      flex: 1,
      marginTop: Spacing.xl,
      backgroundColor: colors.bgSurface,
      borderTopLeftRadius: Radii.lg,
      borderTopRightRadius: Radii.lg,
      overflow: "hidden",
    },
    editorHeader: {
      height: 58,
      paddingHorizontal: 8,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    editorTitle: { ...TypeScale.heading, color: colors.textPrimary },
    headerAction: {
      minWidth: 70,
      minHeight: 44,
      alignItems: "center",
      justifyContent: "center",
    },
    saveText: { ...TypeScale.label, color: colors.accent },
    editorScroll: { flex: 1 },
    form: { padding: 16, paddingBottom: 50, gap: 14 },
    field: { gap: 6 },
    dateTimeRow: { flexDirection: "row", gap: 10 },
    dateField: { flex: 1.35 },
    timeField: { flex: 1 },
    fieldLabel: { ...TypeScale.label, color: colors.textSecondary },
    input: {
      ...TypeScale.body,
      color: colors.textPrimary,
      borderWidth: 1,
      borderColor: colors.borderSubtle,
      borderRadius: Radii.sm,
      paddingHorizontal: 12,
      backgroundColor: colors.bgPrimary,
    },
    singleLine: {
      height: 48,
      paddingVertical: 0,
      textAlignVertical: "center",
    },
    multiline: {
      minHeight: 92,
      paddingVertical: 10,
      textAlignVertical: "top",
    },
    ambiguity: { gap: 8 },
    validationError: { ...TypeScale.compact, color: colors.statusFailed },
    resolvedCard: {
      gap: 4,
      padding: 12,
      borderRadius: Radii.sm,
      backgroundColor: colors.surfaceSubtle,
    },
    resolvedLabel: { ...TypeScale.label, color: colors.textPrimary },
    chips: { flexDirection: "row", flexWrap: "wrap", gap: 8 },
    chip: {
      minHeight: 40,
      paddingHorizontal: 12,
      alignItems: "center",
      justifyContent: "center",
      borderRadius: Radii.pill,
      backgroundColor: colors.surfaceSubtle,
      borderWidth: 1,
      borderColor: "transparent",
    },
    chipSelected: { borderColor: colors.accent },
    chipText: { ...TypeScale.caption, color: colors.textSecondary },
    chipTextSelected: {
      color: colors.accent,
      fontFamily: Typography.uiFontMedium,
    },
    resolved: { ...TypeScale.caption, color: colors.textTertiary },
  });
}
