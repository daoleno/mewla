import type { CalendarItem } from "../store/calendar";
import { alertKey, calendarAlertPlan } from "./calendarAlerts";
import { warnOnce } from "./warnOnce";

export type CalendarNotificationRequest = {
  title: string;
  body: string;
  itemId: string;
  serverId: string;
  trigger: string;
  /** The alert's identity, `<item>@<instant ms>`; the daemon's push for the same alert carries it too. */
  alertKey: string;
};
export type CalendarNotificationDependencies = {
  getStored(): Promise<string | null>;
  setStored(value: string): Promise<void>;
  schedule(request: CalendarNotificationRequest): Promise<string>;
  cancel(id: string): Promise<void>;
  ensureChannel(): Promise<void>;
  /** Anything besides the item that changes what is scheduled, such as the strong-alerts setting. */
  variant?(): string;
};
type StoredNotification = { id: string; fingerprint: string; trigger: string };
type NotificationMap = Record<string, StoredNotification>;

export async function reconcileCalendarNotifications(
  serverId: string,
  items: CalendarItem[],
  deps: CalendarNotificationDependencies,
  now = Date.now(),
) {
  await deps.ensureChannel();
  const current = await readMap(deps);
  const variant = deps.variant?.() ?? "";
  const wanted = new Set<string>();
  for (const { key, item, trigger, fingerprint, title, body, at } of calendarAlertPlan(
    serverId,
    items,
    now,
  )) {
    wanted.add(key);
    const existing = current[key];
    const stamped = fingerprint + variant;
    if (existing?.fingerprint === stamped && existing.trigger === trigger) {
      continue;
    }
    if (existing?.id) {
      try {
        await deps.cancel(existing.id);
      } catch (error) {
        // The old reminder may still fire next to its replacement.
        warnOnce("calendar.notifications", error);
      }
    }
    const id = await deps.schedule({
      title,
      body,
      itemId: item.id,
      serverId,
      trigger,
      alertKey: alertKey(item.id, at),
    });
    current[key] = { id, fingerprint: stamped, trigger };
  }
  for (const [key, record] of Object.entries(current)) {
    if (!key.startsWith(`${serverId}:`) || wanted.has(key)) continue;
    try {
      await deps.cancel(record.id);
    } catch (error) {
      // Keep the record: forgetting a reminder that is still scheduled would
      // leave it firing with nothing able to cancel it. The next sync retries.
      warnOnce("calendar.notifications", error);
      continue;
    }
    delete current[key];
  }
  await deps.setStored(JSON.stringify(current));
}

async function readMap(
  deps: Pick<CalendarNotificationDependencies, "getStored">,
): Promise<NotificationMap> {
  try {
    const parsed = JSON.parse((await deps.getStored()) || "{}") as Record<
      string,
      StoredNotification | string
    >;
    return Object.fromEntries(
      Object.entries(parsed).map(([key, value]) => [
        key,
        typeof value === "string"
          ? { id: value, fingerprint: "", trigger: "" }
          : value,
      ]),
    );
  } catch (error) {
    // An unreadable record means every reminder is scheduled again.
    warnOnce("calendar.notifications", error);
    return {};
  }
}
