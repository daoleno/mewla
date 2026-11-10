import { effect, lifecycle } from "nsh";
import { AlertDialog } from "nsh/base";
import { nextAlertDueAt, openAlerts } from "../../core/calendar/alerts";
import { kindLabel, type CalendarItem } from "../../core/calendar/model";
import { clock, ensureZones } from "./clock";
import { showCalendarItem } from "./routes";
import { calendar, calendarItems } from "./state";

/**
 * A calendar alert while the app is in front: the system's own sheet with Done, Snooze
 * and Open (the phone's notification stays quiet then; the host marks the alert fired).
 * Each alert asks once per opening of the app; answered elsewhere, it is gone.
 */
const asked = new Set<string>();
let showing = false;
let timer: ReturnType<typeof setTimeout> | null = null;

const keyOf = (item: CalendarItem) => `${item.id}@${item.alerted_at ?? ""}@${item.snoozed_until ?? ""}`;

async function ask(item: CalendarItem): Promise<void> {
  showing = true;
  asked.add(keyOf(item));
  await ensureZones([item.timezone]).catch(() => {});
  const c = clock();
  const when = c.format.dateTime(Date.parse(item.next_at), item.timezone, true);
  const more = openAlerts(calendarItems(), Date.now()).length - 1;
  try {
    const { action } = await AlertDialog.show({
      title: item.title,
      description: [`${kindLabel[item.kind]}${more > 0 ? ` · ${more} more waiting` : ""}`, when, item.notes?.trim()].filter(Boolean).join("\n"),
      presentation: "actionSheet",
      closeOnBackdropPress: false,
      actions: [
        { id: "done", label: "Done" },
        { id: "snooze5", label: "Snooze 5 min" },
        { id: "snooze15", label: "Snooze 15 min" },
        { id: "open", label: "Open in Calendar" },
        { id: "later", label: "Later", role: "cancel" },
      ],
    });
    const model = calendar();
    if (action === "done") await model?.done(item.id);
    else if (action === "snooze5") await model?.snooze(item.id, 5);
    else if (action === "snooze15") await model?.snooze(item.id, 15);
    else if (action === "open") showCalendarItem(item.id);
  } catch (error) {
    await AlertDialog.show({ title: "Could not reach your computer. Try again.", description: error instanceof Error ? error.message : "", actions: [{ id: "ok", label: "OK" }] }).catch(() => {});
    asked.delete(keyOf(item));
  } finally {
    showing = false;
    check();
  }
}

function check(): void {
  if (timer) clearTimeout(timer);
  timer = null;
  const items = calendarItems();
  const now = Date.now();
  if (lifecycle() === "active" && !showing) {
    const next = openAlerts(items, now).find((item) => !asked.has(keyOf(item)));
    if (next) return void ask(next);
  }
  const due = nextAlertDueAt(items, now);
  if (due !== null) timer = setTimeout(check, Math.min(due - now + 500, 2_147_000_000));
}

export function startCalendarAlerts(): void {
  effect(() => {
    calendarItems();
    if (lifecycle() !== "active") asked.clear();
    check();
  });
}
