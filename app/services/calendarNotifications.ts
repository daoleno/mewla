import AsyncStorage from "@react-native-async-storage/async-storage";
import * as Notifications from "expo-notifications";
import { Platform } from "react-native";
import { getMewlaLiveModule } from "../modules/mewla-live/src";
import type { CalendarNativeAction } from "../modules/mewla-live/src";
import type { CalendarItem } from "../store/calendar";
import { calendarAlertPlan, calendarTriggerParts } from "./calendarAlerts";
import { strongCalendarAlerts } from "./calendarAlertSettings";
import {
  reconcileCalendarNotifications,
  type CalendarNotificationDependencies,
} from "./calendarNotificationReconciler";

const storageKey = "mewla.calendar.notification_ids.v1";
/** Heads-up and sound; the native alarm uses its own alarm-style channel. */
export const calendarChannelId = "mewla-calendar";
export type CalendarNotificationState =
  "granted" | "denied" | "undetermined" | "unavailable";

export async function calendarNotificationState(): Promise<CalendarNotificationState> {
  try {
    const permission = await Notifications.getPermissionsAsync();
    if (permission.status === "granted") return "granted";
    if (permission.status === "denied") return "denied";
    return "undetermined";
  } catch {
    return "unavailable";
  }
}

export async function requestCalendarNotifications(): Promise<CalendarNotificationState> {
  try {
    const permission = await Notifications.requestPermissionsAsync();
    if (permission.status === "granted") return "granted";
    if (permission.status === "denied") return "denied";
    return "undetermined";
  } catch {
    return "unavailable";
  }
}

/** Android's alarm code, when this build has it. */
function nativeAlerts() {
  const native = Platform.OS === "android" ? getMewlaLiveModule() : null;
  return native?.calendarAlertsSchedule ? native : null;
}

/**
 * Arms every upcoming alert on the phone itself, so it fires with the daemon
 * or the network late or gone. Android hands the set to its exact-alarm code;
 * iOS schedules Time Sensitive local notifications. The daemon's push is the
 * backup for what the phone has not synced.
 */
export async function syncCalendarNotifications(
  serverId: string,
  items: CalendarItem[],
): Promise<CalendarNotificationState> {
  const state = await calendarNotificationState();
  if (state !== "granted") return state;
  const native = nativeAlerts();
  if (native) {
    await ensureCalendarChannel();
    native.calendarAlertsSchedule!(
      JSON.stringify(
        calendarAlertPlan(serverId, items).map((entry) => ({
          key: `${entry.item.id}@${entry.at}`,
          itemId: entry.item.id,
          serverId,
          title: entry.title,
          body: entry.body,
          atMs: entry.at,
        })),
      ),
    );
    return state;
  }
  await reconcileCalendarNotifications(serverId, items, notificationDeps());
  return state;
}

export async function cancelCalendarNotifications(serverId: string) {
  const native = nativeAlerts();
  if (native) {
    native.calendarAlertsSchedule!("[]");
    return;
  }
  await reconcileCalendarNotifications(serverId, [], notificationDeps());
}

/**
 * Drops what this phone is still holding for an item the person just answered:
 * its armed alarm and the notification already on screen.
 */
export async function dismissCalendarAlert(itemId: string) {
  nativeAlerts()?.calendarAlertsCancelItem?.(itemId);
  try {
    for (const posted of await Notifications.getPresentedNotificationsAsync()) {
      if (posted.request.content.data?.calendar_id === itemId) {
        await Notifications.dismissNotificationAsync(posted.request.identifier);
      }
    }
  } catch {
    // Nothing on screen to drop.
  }
}

export function setCalendarAlertsForeground(foreground: boolean) {
  nativeAlerts()?.calendarAlertsSetForeground?.(foreground);
}

/** Done and Snooze taken on the lock-screen alert or its notification with the app closed. */
export function takeNativeCalendarActions(): CalendarNativeAction[] {
  const taken = nativeAlerts()?.calendarAlertsTakeActions?.();
  if (!taken) return [];
  try {
    const parsed = JSON.parse(taken);
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

async function ensureCalendarChannel() {
  if (Platform.OS !== "android") return;
  await Notifications.setNotificationChannelAsync(calendarChannelId, {
    name: "Calendar reminders",
    importance: Notifications.AndroidImportance.HIGH,
  });
}

function notificationDeps(): CalendarNotificationDependencies {
  return {
    getStored: () => AsyncStorage.getItem(storageKey),
    setStored: (value) => AsyncStorage.setItem(storageKey, value),
    schedule: (request) =>
      Notifications.scheduleNotificationAsync({
        // The alert's own identity: the daemon's push for it carries the same
        // key, which the foreground handler uses to show it once.
        identifier: request.alertKey,
        content: {
          title: request.title,
          body: request.body,
          sound: "default",
          // Time Sensitive breaks through Focus (the Preview identity has the
          // entitlement); without it iOS delivers an ordinary alert.
          interruptionLevel: strongCalendarAlerts() ? "timeSensitive" : "active",
          data: {
            screen: "calendar",
            calendar_id: request.itemId,
            server_id: request.serverId,
            alert_key: request.alertKey,
          },
        },
        // iOS: a calendar trigger (UNCalendarNotificationTrigger) at the alert's
        // wall clock in the zone it was armed in. Expo's DATE trigger would be a
        // time-interval one, counted from now.
        trigger:
          Platform.OS === "ios"
            ? {
                type: Notifications.SchedulableTriggerInputTypes.CALENDAR,
                repeats: false,
                ...calendarTriggerParts(
                  Date.parse(request.trigger),
                  Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
                ),
              }
            : {
                type: Notifications.SchedulableTriggerInputTypes.DATE,
                date: new Date(request.trigger),
                channelId: Platform.OS === "android" ? calendarChannelId : undefined,
              },
      }),
    cancel: (id) => Notifications.cancelScheduledNotificationAsync(id),
    ensureChannel: ensureCalendarChannel,
    variant: () => (strongCalendarAlerts() ? "strong" : "quiet"),
  };
}
