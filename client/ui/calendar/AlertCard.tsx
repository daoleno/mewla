import { effect, signal, Text, View } from "nsh";
import { Button } from "nsh/base";
import { kindLabel } from "../../core/calendar/model";
import { AppSheet } from "../shell/AppSheet";
import { openedInCalendar, shownAlert } from "./alerts";
import { clock, ensureZones } from "./clock";
import { openCalendarItem } from "../shell/nav";
import { calendar } from "./state";

/**
 * The calendar alert card (v0.3.1's CalendarAlertHost): the item's kind, title, time and notes
 * over the app until the person answers. Neither Back, the backdrop nor Escape closes it.
 */
export function CalendarAlertCard() {
  const busy = signal(false);
  const failure = signal("");
  effect(() => {
    const zone = shownAlert()?.item.timezone;
    failure.set("");
    if (zone) void ensureZones([zone]).catch(() => {});
  });
  const item = () => shownAlert()?.item ?? null;
  const kind = () => {
    const shown = shownAlert();
    return shown ? `${kindLabel[shown.item.kind]}${shown.more > 0 ? ` · ${shown.more} more waiting` : ""}` : "";
  };
  const when = () => {
    const shown = item();
    return shown ? clock().format.dateTime(Date.parse(shown.next_at), shown.timezone, true) : "";
  };
  const notes = () => item()?.notes?.trim() ?? "";
  const act = async (run: (id: string) => Promise<unknown> | undefined) => {
    const shown = item();
    if (!shown || busy()) return;
    busy.set(true);
    failure.set("");
    try {
      await run(shown.id);
    } catch (error) {
      failure.set(error instanceof Error && error.message ? error.message : "Could not reach your computer. Try again.");
    } finally {
      busy.set(false);
    }
  };
  const open = () => {
    const shown = item();
    if (!shown) return;
    openedInCalendar(shown);
    openCalendarItem(shown.id);
  };
  return (
    <AppSheet open={Boolean(shownAlert())} onOpenChange={() => {}} closeOnBackdropPress={false} closeOnEscape={false}
      popupClass="rounded-t-2xl bg-background px-5 pt-5" backdropClass="bg-black/40">
      <View testID="calendar-alert-card" class="gap-3 pb-8">
        <Text testID="alert-kind" class="text-sm font-semibold text-primary" role="header">{kind()}</Text>
        <Text testID="alert-title" class="text-2xl font-semibold">{item()?.title ?? ""}</Text>
        <Text testID="alert-when" class="text-lg text-muted-foreground">{when()}</Text>
        {notes() ? <Text testID="alert-notes" class="text-base">{notes()}</Text> : null}
        {failure() ? (
          <View testID="alert-problem" class="gap-1 rounded-xl border border-border bg-card px-3 py-2.5">
            <Text class="text-sm font-semibold text-destructive">That did not go through</Text>
            <Text class="text-sm text-muted-foreground">{failure()}</Text>
          </View>
        ) : null}
        <View class="mt-2 gap-2.5">
          <Button testID="alert-done" disabled={busy()} onPress={() => void act((id) => calendar()?.done(id))}
            class={busy() ? "items-center rounded-full bg-primary py-3 opacity-50" : "items-center rounded-full bg-primary py-3 active:opacity-80"}>
            <Text class="text-base font-semibold text-primary-foreground">Done</Text>
          </Button>
          <View class="flex-row gap-2.5">
            <Button testID="alert-snooze5" disabled={busy()} onPress={() => void act((id) => calendar()?.snooze(id, 5))} class="flex-1 items-center rounded-full border border-border py-3 active:bg-accent">
              <Text class="text-base">Snooze 5 min</Text>
            </Button>
            <Button testID="alert-snooze15" disabled={busy()} onPress={() => void act((id) => calendar()?.snooze(id, 15))} class="flex-1 items-center rounded-full border border-border py-3 active:bg-accent">
              <Text class="text-base">Snooze 15 min</Text>
            </Button>
          </View>
          <Button testID="alert-open" disabled={busy()} onPress={open} class="items-center py-2.5 active:opacity-70">
            <Text class="text-base text-primary">Open in Calendar</Text>
          </Button>
        </View>
      </View>
    </AppSheet>
  );
}
