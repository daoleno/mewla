import { host, signal, Storage } from "nsh";
import type { ArmedAlert, NativeAlertAction } from "../../core/calendar/alerts";
import { CalendarModel, type AlertHost } from "../../core/calendar/calendar";
import { zonesOf, type CalendarItem } from "../../core/calendar/model";
import { onTransport } from "../session";
import { clock, ensureZones } from "./clock";

/** The current computer's Calendar; null without a computer. */
export const calendar = signal<CalendarModel | null>(null);
export const calendarItems = signal<CalendarItem[]>([]);
/** The first snapshot arrived: before it, "no items" is not "nothing planned". */
export const calendarLoaded = signal(false);

/** The phone's own alerts: Android's exact alarms and lock-screen alert, iOS's local notifications. */
const alertHost: AlertHost | null = host.available()
  ? {
      schedule: async (alerts: ArmedAlert[]) => void (await host.call("calendar.alertsSchedule", { alerts })),
      cancelItem: async (itemId: string) => void (await host.call("calendar.alertsCancelItem", { itemId })),
      takeActions: async () => ((await host.call<NativeAlertAction[] | null>("calendar.alertsTakeActions")) ?? []),
    }
  : null;

onTransport((transport, server) => {
  const model = new CalendarModel(server.id, transport, alertHost);
  calendar.set(model);
  const stop = model.onChange(() => {
    calendarItems.set([...model.items]);
    calendarLoaded.set(model.loaded);
    void ensureZones(zonesOf(model.items, clock().zones.device));
  });
  return () => {
    stop();
    model.close();
    calendar.set(null);
    calendarItems.set([]);
    calendarLoaded.set(false);
  };
});

/** A computer is gone and none took its place: its alerts must not fire on this phone. */
export function clearCalendarAlerts(): void {
  void alertHost?.schedule([]).catch(() => {});
}

const STRONG_KEY = "mewla.calendar.strong_alerts.v1";
/** "Strong calendar alerts": an alarm-style channel and the lock-screen alert. On unless turned off. */
export const strongAlerts = signal(true);
const strongListeners = new Set<() => void>();

export async function loadStrongAlerts(): Promise<boolean> {
  const value = (await Storage.getItem(STRONG_KEY).catch(() => null)) !== "off";
  strongAlerts.set(value);
  if (alertHost) void host.call("calendar.alertsSetStrong", { strong: value }).catch(() => {});
  return value;
}

/** Changes the setting: the computer is told (it decides how its pushes arrive) and the alerts are armed again. */
export async function setStrongAlerts(value: boolean): Promise<void> {
  strongAlerts.set(value);
  if (alertHost) await host.call("calendar.alertsSetStrong", { strong: value }).catch(() => {});
  await Storage.setItem(STRONG_KEY, value ? "on" : "off");
  strongListeners.forEach((listener) => listener());
  calendar()?.refresh();
}

export function onStrongAlertsChange(listener: () => void): () => void {
  strongListeners.add(listener);
  return () => strongListeners.delete(listener);
}
