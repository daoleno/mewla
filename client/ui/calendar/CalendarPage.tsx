import { computed, List, signal, Text, View } from "nsh";
import type { JSX } from "nsh";
import { Button } from "nsh/base";
import { NavPage } from "../shell/NavPage";
import { Segmented } from "../kit/Segmented";
import { createTabPager, TabPages } from "../kit/TabPager";
import type { NavStackState } from "nsh/base";
import { monthTitle } from "../../core/calendar/format";
import { activeRun, groupAgenda, itemsOnDay, itemSummary, kindLabel, rowTime, statusLabel, type CalendarItem } from "../../core/calendar/model";
import { connection, workers } from "../session";
import { clock } from "./clock";
import { CalendarEditor, openEditor } from "./Editor";
import { StatusDot } from "./parts";
import { Icon } from "../kit/Icon";
import { RowTile } from "../kit/RowTile";
import { calendarLead } from "../../core/calendar/colour";
import { ReminderBanner } from "./ReminderBanner";
import type { Route } from "../shell/routes";
import { calendar, calendarItems, calendarLoaded } from "./state";

type AgendaRow = { kind: "section"; title: string } | { kind: "item"; item: CalendarItem };

const showMonth = signal(false);
/** A day picked on the month: the list shows that day only. */
const pickedDay = signal<string | null>(null);
const viewedMonth = signal<{ year: number; month: number } | null>(null);

/** The agenda's rows: one day's items (a day picked on the month), else every day's. */
function rows(day: string | null): AgendaRow[] {
  const items = calendarItems();
  const c = clock();
  if (day) return itemsOnDay(items, day, c).map((item) => ({ kind: "item", item }));
  // "Today" is the heading's word, so its section has no title of its own.
  return groupAgenda(items, c).flatMap((section): AgendaRow[] => [
    ...(section.title === "Today" ? [] : [{ kind: "section" as const, title: section.title }]),
    ...section.items.map((item) => ({ kind: "item" as const, item })),
  ]);
}

function ItemRow(props: { item: () => CalendarItem; onPress: () => void }) {
  const line = () => {
    const item = props.item();
    return `${statusLabel[item.status]} · ${itemSummary(item) || kindLabel[item.kind]}`;
  };
  return (
    <Button testID={`calendar-item-${props.item().id}`} onPress={props.onPress} accessibilityLabel={`${props.item().title}, ${itemSummary(props.item()) || kindLabel[props.item().kind]}, ${statusLabel[props.item().status]}`}
      class="flex-row gap-3 border-b border-border bg-card px-4 py-3 active:bg-accent">
      {/* Who acts (SPEC §10.4), its glyph on the neutral tile: bell, calendar or flag for you, flash for a job Brain runs, sync for a recurring check. */}
      <RowTile testID="calendar-item-lead" lead={calendarLead(props.item())} />
      <Text class="w-20 pt-0.5 text-sm text-muted-foreground">{rowTime(props.item(), clock())}</Text>
      <View class="flex-1 gap-1">
        <Text class="text-base line-clamp-2">{props.item().title}</Text>
        <View class="flex-row items-center gap-1.5">
          <StatusDot status={props.item().status} />
          <Text class="flex-1 text-sm text-muted-foreground line-clamp-1">{line()}</Text>
        </View>
      </View>
    </Button>
  );
}

/** Six weeks from the Sunday before the 1st; a dot on days with items, today outlined, the picked day filled. */
function Month() {
  const view = () => {
    const c = clock();
    const fallback = c.zones.wallClock(c.now, c.zones.device);
    return viewedMonth() ?? { year: fallback.year, month: fallback.month };
  };
  const days = () => {
    const { year, month } = view();
    const first = Date.UTC(year, month - 1, 1);
    const start = first - new Date(first).getUTCDay() * 86_400_000;
    const c = clock();
    const counts = new Map<string, number>();
    for (const i of calendarItems()) {
      if (i.status === "cancelled") continue;
      const key = c.zones.dateKey(Date.parse(i.next_at), c.zones.device);
      counts.set(key, (counts.get(key) ?? 0) + 1);
    }
    const today = c.zones.dateKey(c.now, c.zones.device);
    return Array.from({ length: 42 }, (_, i) => {
      const d = new Date(start + i * 86_400_000);
      const key = d.toISOString().slice(0, 10);
      const count = counts.get(key) ?? 0;
      return { key, day: d.getUTCDate(), year: d.getUTCFullYear(), month: d.getUTCMonth() + 1, inMonth: d.getUTCMonth() === month - 1, count, dot: count > 0, today: key === today };
    });
  };
  // A day of the month before or after moves the month to it.
  const pick = (d: { key: string; year: number; month: number; inMonth: boolean }) => {
    pickedDay.set(pickedDay() === d.key ? null : d.key);
    if (!d.inMonth) viewedMonth.set({ year: d.year, month: d.month });
  };
  const step = (delta: number) => {
    const { year, month } = view();
    const index = year * 12 + (month - 1) + delta;
    viewedMonth.set({ year: Math.floor(index / 12), month: (index % 12) + 1 });
  };
  return (
    <View testID="calendar-month" accessibilityLabel="Month date navigator" class="border-b border-border bg-card px-3 pb-3 pt-2">
      <View class="flex-row items-center justify-between py-1">
        <Button accessibilityLabel="Previous month" onPress={() => step(-1)} class="px-3 py-1.5"><Text class="text-lg">‹</Text></Button>
        <Text class="text-base font-semibold">{monthTitle(view().year, view().month)}</Text>
        <Button accessibilityLabel="Next month" onPress={() => step(1)} class="px-3 py-1.5"><Text class="text-lg">›</Text></Button>
      </View>
      <View class="flex-row">{["S", "M", "T", "W", "T", "F", "S"].map((d) => <Text class="flex-1 text-center text-xs text-muted-foreground">{d}</Text>)}</View>
      <View class="flex-row flex-wrap">
        {days().map((d) => (
          <Button testID={`day-${d.key}`} accessibilityLabel={`${d.day}, ${d.count} items`} onPress={() => pick(d)} class="items-center py-1" style={{ width: "14.28%" }}>
            <View class={pickedDay() === d.key ? "size-8 items-center justify-center rounded-full bg-primary" : d.today ? "size-8 items-center justify-center rounded-full border border-primary" : "size-8 items-center justify-center"}>
              <Text class={pickedDay() === d.key ? "text-sm text-primary-foreground" : d.inMonth ? "text-sm" : "text-sm text-muted-foreground"}>{String(d.day)}</Text>
            </View>
            <View class={d.dot ? "mt-0.5 size-1 rounded-full bg-muted-foreground" : "mt-0.5 size-1"} />
          </Button>
        ))}
      </View>
    </View>
  );
}

function Heading(props: { day: () => string | null }) {
  return (
    <View class="px-4 pb-1 pt-4">
      <If when={props.day()} else={<View accessibilityLabel={`Today, ${clock().format.longDay(clock().now, clock().zones.device)}, ${clock().format.time(clock().now, clock().zones.device, true)}`} class="gap-1">
          <Text class="text-2xl font-semibold">Today</Text>
          <Text class="text-sm text-muted-foreground">{`${clock().format.longDay(clock().now, clock().zones.device)} · ${clock().format.time(clock().now, clock().zones.device, true)}`}</Text>
        </View>}><View class="flex-row items-center justify-between">
          <Text class="text-lg font-semibold">{clock().format.longDay(Date.parse(`${props.day()}T12:00:00Z`), "UTC")}</Text>
          <Button testID="back-to-today" accessibilityLabel="Return to today" onPress={() => pickedDay.set(null)} class="px-2 py-1"><Text class="text-base text-primary">Today</Text></Button>
        </View></If>
    </View>
  );
}

function Empty(props: { day: () => string | null }) {
  return (
    <View testID="calendar-empty" class="items-center gap-2 px-6 py-16">
      <Text class="text-lg font-semibold">{!calendarLoaded() ? (connection() === "connected" ? "Loading calendar…" : "Calendar offline") : props.day() ? "Nothing planned for this date" : "Nothing planned"}</Text>
      <If when={!calendarLoaded() && connection() !== "connected"}><Text class="text-center text-base text-muted-foreground">It shows when your computer is back.</Text></If>
    </View>
  );
}

/** One page's agenda: its rows under its header (the month, the day's heading). */
function Agenda(props: { testID: string; day: () => string | null; header: () => JSX.Element; onOpen: (item: CalendarItem) => void }) {
  const agenda = computed(() => rows(props.day()));
  // The empty state is the footer while there are no rows, never the List's `empty` (core/listEmpty.test.ts).
  return (
    <List keyboardShouldPersistTaps="handled" testID={props.testID} class="flex-1" each={agenda()}
      keyExtractor={(row) => (row.kind === "section" ? `s:${row.title}` : `i:${row.item.id}`)}
      header={props.header}
      footer={<If when={agenda().length === 0}><Empty day={props.day} /></If>}>
      {(row) => {
        // Cells are recycled: what a cell shows follows the row it holds now. The clock makes new rows every
        // 30 s, so the cell is rebuilt only when it turns from a heading into an item or back: a tap at the
        // tick lands on the row, not on one being replaced.
        const section = computed(() => row().kind === "section");
        // The item row may read once more after the cell took a heading, before it goes: it keeps its last item.
        const item = computed<CalendarItem>((last) => (row().kind === "section" ? last! : (row() as { item: CalendarItem }).item));
        const title = computed<string>((last) => (row().kind === "section" ? (row() as { title: string }).title : last ?? ""));
        return (
          <View>
            <If when={section()} else={<ItemRow item={item} onPress={() => props.onOpen(item())} />}><View class="bg-background px-4 pb-1.5 pt-4"><Text class="text-sm font-semibold text-muted-foreground">{title()}</Text></View></If>
          </View>
        );
      }}
    </List>
  );
}

export function CalendarPage(props: { nav: NavStackState<Route>; openSession: (sessionId: string) => void }) {
  // Today ⇄ Month switches in place (SPEC §3.5); Today is the whole agenda again, no day picked. The two are
  // pages side by side that follow the finger (kit/TabPager.tsx): the agenda, and the month over its agenda.
  const show = (view: "today" | "month") => {
    if (view === "today") pickedDay.set(null);
    showMonth.set(view === "month");
  };
  const pager = createTabPager({ keys: ["today", "month"] as const, value: () => (showMonth() ? "month" : "today"), onChange: show });
  // A running item opens its Session while the computer lists it; the others, and a run whose Session
  // has gone, their page (the Expo app checked the same; parity #45).
  const open = (item: CalendarItem) => {
    const session = item.status === "running" ? activeRun(item)?.worker_session : undefined;
    if (session && workers().some((worker) => worker.id === session)) props.openSession(session);
    else props.nav.push({ name: "item", id: item.id });
  };
  return (
    // The mock's header: Back · "Calendar" · the Today | Month control · +. The system header holds the control at
    // its end beside +, so Back and the title lead on both platforms (no leading item for Android to move).
    <NavPage title="Calendar" testID="calendar-page"
      headerRight={
        <View class="flex-row items-center gap-1">
          <Segmented testID="calendar-view" accessibilityLabel="Calendar view" fit value={showMonth() ? "month" : "today"} onChange={show} pager={pager}
            options={[{ key: "today", label: "Today" }, { key: "month", label: "Month" }]} />
          <Button testID="add-item" accessibilityLabel="Add calendar item" disabled={!calendar()} onPress={() => openEditor(null)} class="size-11 items-center justify-center active:opacity-70 disabled:opacity-40">
            <Icon name="add" size={24} tone="ink" />
          </Button>
        </View>
      }>
      <View class="flex-1 bg-background">
        {/* About the whole Calendar, so above both pages. */}
        <ReminderBanner />
        <TabPages fill testID="calendar-pages" pager={pager} pages={{
          today: () => <Agenda testID="agenda" day={() => null} header={() => <Heading day={() => null} />} onOpen={open} />,
          month: () => <Agenda testID="agenda-month" day={pickedDay} header={() => <View><Month /><Heading day={pickedDay} /></View>} onOpen={open} />,
        }} />
        <CalendarEditor />
      </View>
    </NavPage>
  );
}
