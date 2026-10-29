import { useRouter } from "expo-router";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Modal, ScrollView, StyleSheet, View } from "react-native";
import { useAppTheme } from "../../constants/tokens";
import {
  alertKey,
  nextAlertDueAt,
  openCalendarAlerts,
  pendingAlertAt,
} from "../../services/calendarAlerts";
import { dismissCalendarAlert } from "../../services/calendarNotifications";
import { formatCalendarTime, kindLabel } from "../../services/calendarPresentation";
import { wsClient } from "../../services/websocket";
import { selectCurrentServerCalendar, useCalendar, type CalendarItem } from "../../store/calendar";
import { useCurrentServer } from "../../store/currentServer";
import { AppText } from "../ui/AppText";
import { Button } from "../ui/Button";

/**
 * The alert card: covers the screen with the item that needs an answer, on
 * every platform, until the person acts. What it shows comes from the synced
 * Calendar and this phone's clock, not from how an alert was delivered, so it
 * is the same card after a push, a local alarm, a tap on the notification or a
 * restart, and it goes away on every phone once one of them answers.
 */
export function CalendarAlertHost() {
  const { currentServerId } = useCurrentServer();
  const calendar = useCalendar();
  const router = useRouter();
  const items = selectCurrentServerCalendar(calendar, currentServerId)?.items;
  const [now, setNow] = useState(() => Date.now());
  // Alerts the person opened in Calendar instead; the card does not return for them.
  const [opened, setOpened] = useState<string[]>([]);

  useEffect(() => {
    setOpened([]);
  }, [currentServerId]);

  useEffect(() => {
    setNow(Date.now());
    const due = items ? nextAlertDueAt(items, Date.now()) : null;
    if (due === null) return;
    const timer = setTimeout(() => setNow(Date.now()), Math.min(due - Date.now() + 250, 2 ** 30));
    return () => clearTimeout(timer);
  }, [items]);

  const open = useMemo(
    () =>
      (items ? openCalendarAlerts(items, now) : []).filter(
        (item) => !opened.includes(alertIdentity(item)),
      ),
    [items, now, opened],
  );
  const item = open[0];
  const serverId = currentServerId;

  const openInCalendar = useCallback(
    (target: CalendarItem) => {
      setOpened((keys) => [...keys, alertIdentity(target)]);
      router.push({ pathname: "/calendar", params: { id: target.id, serverId: serverId ?? undefined } });
    },
    [router, serverId],
  );

  if (!item || !serverId) return null;
  return (
    <CalendarAlertCard
      key={alertIdentity(item)}
      item={item}
      more={open.length - 1}
      onDone={async () => {
        await wsClient.doneCalendarItem(serverId, item.id);
        await dismissCalendarAlert(item.id);
      }}
      onSnooze={async (minutes) => {
        await wsClient.snoozeCalendarItem(serverId, item.id, minutes);
        await dismissCalendarAlert(item.id);
      }}
      onOpen={() => openInCalendar(item)}
    />
  );
}

function alertIdentity(item: CalendarItem): string {
  const at = item.alerted_at ? Date.parse(item.alerted_at) : pendingAlertAt(item);
  return alertKey(item.id, at ?? Date.parse(item.next_at));
}

export function CalendarAlertCard({
  item,
  more,
  onDone,
  onSnooze,
  onOpen,
}: {
  item: CalendarItem;
  more: number;
  onDone(): Promise<void>;
  onSnooze(minutes: number): Promise<void>;
  onOpen(): void;
}) {
  const { colors } = useAppTheme();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const act = async (run: () => Promise<void>) => {
    setBusy(true);
    setError("");
    try {
      await run();
    } catch (value) {
      setError(value instanceof Error ? value.message : "Could not reach your computer. Try again.");
    } finally {
      setBusy(false);
    }
  };
  const notes = item.notes?.trim();
  return (
    <Modal transparent animationType="fade" visible onRequestClose={() => {}}>
      <View style={styles.backdrop}>
        <View
          accessibilityViewIsModal
          testID="calendar-alert-card"
          style={[styles.card, { backgroundColor: colors.bgPrimary }]}
        >
          <ScrollView contentContainerStyle={styles.content}>
            <AppText variant="caption" tone="accent" accessibilityRole="header">
              {kindLabel[item.kind]}
              {more > 0 ? ` · ${more} more waiting` : ""}
            </AppText>
            <AppText variant="display">{item.title}</AppText>
            <AppText variant="heading" tone="secondary">
              {formatCalendarTime(item)}
            </AppText>
            {notes ? <AppText>{notes}</AppText> : null}
            {error ? (
              <AppText tone="danger" accessibilityRole="alert">
                {error}
              </AppText>
            ) : null}
            <View style={styles.actions}>
              <Button label="Done" variant="filled" size="lg" block disabled={busy} onPress={() => void act(onDone)} />
              <View style={styles.row}>
                <Button label="Snooze 5 min" variant="tinted" block disabled={busy} style={styles.flex} onPress={() => void act(() => onSnooze(5))} />
                <Button label="Snooze 15 min" variant="tinted" block disabled={busy} style={styles.flex} onPress={() => void act(() => onSnooze(15))} />
              </View>
              <Button label="Open in Calendar" variant="plain" block disabled={busy} onPress={onOpen} />
            </View>
          </ScrollView>
        </View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  backdrop: { flex: 1, backgroundColor: "rgba(0,0,0,0.55)", justifyContent: "center", alignItems: "center", padding: 20 },
  card: { maxWidth: 520, width: "100%", maxHeight: "92%", borderRadius: 28 },
  content: { padding: 28, gap: 14 },
  actions: { gap: 10, marginTop: 14 },
  row: { flexDirection: "row", gap: 10 },
  flex: { flex: 1 },
});
