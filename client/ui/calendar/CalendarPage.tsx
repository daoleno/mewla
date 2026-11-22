import { Scroll, signal, Text, View } from "nsh";
import type { JSX } from "nsh";
import { Button } from "nsh/base";
import type { NavStackState } from "nsh/base";
import { agenda, dayKey, dayRows, monthMarks, type AgendaRow } from "../../core/calendar/agenda";
import { monthTitle } from "../../core/calendar/format";
import { activeRun } from "../../core/calendar/model";
import { pageColumnWidth } from "../../core/web/places";
import { dense, density } from "../kit/density";
import { Section } from "../kit/kit";
import { Segmented } from "../kit/Segmented";
import { createTabPager, TabPages } from "../kit/TabPager";
import { Icon } from "../kit/Icon";
import { NavPage } from "../shell/NavPage";
import type { Route } from "../shell/routes";
import { connection, workers } from "../session";
import { clock } from "./clock";
import { CalendarEditor, openEditor } from "./Editor";
import { ReminderBanner } from "./ReminderBanner";
import { AgendaRowView, NowLine, TileRow } from "./rows";
import { calendar, calendarItems, calendarLoaded } from "./state";
import { TriggersRow } from "./TriggerPage";

const showMonth = signal(false);
/** A day picked on the month: the list under it shows that day. */
const pickedDay = signal<string | null>(null);
const viewedMonth = signal<{ year: number; month: number } | null>(null);
/** Earlier days, folded into one row at the end until opened. */
const earlierOpen = signal(false);
/** Tomorrow shows its first rows and "N more" until opened. */
const tomorrowOpen = signal(false);
const TOMORROW_ROWS = 5;

/** Month on a day (a day key): the Mac's proof script shows a picked day this way. */
export function showCalendarDay(key: string): void {
  showMonth.set(true);
  pickedDay.set(key);
  const [year, month] = key.split("-").map(Number);
  viewedMonth.set({ year: year!, month: month! });
}

type EarlierDay = { key: string; rows: AgendaRow[] };
type Line = { key: string; kind: "now" } | { key: string; kind: "row"; row: AgendaRow };

/** A day's card: its rows, the now line among Today's, and "N more" at the end when it is folded. */
function DayCard(props: { testID: string; title?: string; flush?: boolean; rows: () => AgendaRow[]; now?: boolean; date?: boolean; more?: () => number; onMore?: () => void; onOpen: (row: AgendaRow) => void }) {
  const lines = (): Line[] => {
    const lines: Line[] = props.rows().map((row) => ({ key: row.key, kind: "row", row }));
    if (props.now) {
      // Before the first row still to come: what ran (or was carried over) is above it.
      const next = props.rows().findIndex((row) => !row.carried && row.at > clock().now);
      lines.splice(next < 0 ? lines.length : next, 0, { key: "now", kind: "now" });
    }
    return lines;
  };
  return (
    // Flush: right under the headline (or the Triggers row), with no gap of its own.
    <Section title={props.title} testID={props.testID} flush={props.flush}>
      <Each of={lines()} key={(line: Line) => line.key}>{(line: () => Line, index: () => number) => {
        // A line keeps its kind (its key says which); the row it shows follows the list.
        const row = () => (line() as { row: AgendaRow }).row;
        return (
          <If when={line().kind === "now"} else={<AgendaRowView row={row} first={() => index() === 0} date={props.date} onPress={() => props.onOpen(row())} />}>
            <NowLine first={() => index() === 0} />
          </If>
        );
      }}</Each>
      <If when={(props.more?.() ?? 0) > 0}>
        <Button testID={`${props.testID}-more`} onPress={props.onMore} class="border-t border-border py-3 active:bg-accent" style={{ paddingLeft: 66 }}>
          <Text class="text-sm text-muted-foreground">{`${props.more!()} more`}</Text>
        </Button>
      </If>
    </Section>
  );
}

/** Six weeks from the Sunday before the 1st: a dot on days with one-off items (amber where one failed), today ringed, the picked day filled. */
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
    const marks = monthMarks(calendarItems(), clock());
    const today = dayKey(clock());
    return Array.from({ length: 42 }, (_, i) => {
      const d = new Date(start + i * 86_400_000);
      const key = d.toISOString().slice(0, 10);
      return { key, day: d.getUTCDate(), year: d.getUTCFullYear(), month: d.getUTCMonth() + 1, inMonth: d.getUTCMonth() === month - 1, mark: marks.get(key), today: key === today, past: key < today };
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
  const dayText = (d: { key: string; inMonth: boolean; past: boolean; today: boolean }) =>
    pickedDay() === d.key ? "text-sm font-bold text-primary-foreground" : d.today ? "text-sm font-bold" : !d.inMonth ? "text-sm text-faint" : d.past ? "text-sm text-muted-foreground" : "text-sm";
  const circle = (d: { key: string; today: boolean }) =>
    pickedDay() === d.key ? "size-[34px] items-center justify-center rounded-full bg-primary" : d.today ? "size-[34px] items-center justify-center rounded-full border-[1.5px] border-foreground" : "size-[34px] items-center justify-center";
  return (
    <View testID="calendar-month" accessibilityLabel="Month date navigator" class={`overflow-hidden bg-card px-2 pb-2.5 pt-1.5 ${dense({ phone: "rounded-(--radius-group)", window: "rounded-[14px]" })}`}>
      <View class="flex-row items-center justify-between px-1 pb-1.5">
        <Button accessibilityLabel="Previous month" onPress={() => step(-1)} class="size-9 items-center justify-center active:opacity-70"><Icon name="chevron-left" size={18} tone="ink" /></Button>
        <Text class="text-base font-semibold">{monthTitle(view().year, view().month)}</Text>
        <Button accessibilityLabel="Next month" onPress={() => step(1)} class="size-9 items-center justify-center active:opacity-70"><Icon name="chevron-right" size={18} tone="ink" /></Button>
      </View>
      <View class="flex-row">{["S", "M", "T", "W", "T", "F", "S"].map((d) => <Text class="flex-1 pb-1 text-center text-xs text-muted-foreground">{d}</Text>)}</View>
      <View class="flex-row flex-wrap">
        {days().map((d) => (
          <Button testID={`day-${d.key}`} accessibilityLabel={`${d.day}${d.mark ? ", has items" : ""}`} onPress={() => pick(d)} class="items-center py-[3px]" style={{ width: "14.28%" }}>
            <View class={circle(d)}><Text class={dayText(d)} style={{ fontVariant: ["tabular-nums"] }}>{String(d.day)}</Text></View>
            <View class={d.mark === "failed" ? "mt-0.5 size-[5px] rounded-full bg-warning" : d.mark ? "mt-0.5 size-[5px] rounded-full bg-muted-foreground" : "mt-0.5 size-[5px]"} />
          </Button>
        ))}
      </View>
    </View>
  );
}

function Empty(props: { day: () => string | null }) {
  return (
    <View testID="calendar-empty" class={`mt-3 items-center gap-2 bg-card px-6 py-10 ${dense({ phone: "rounded-(--radius-group)", window: "rounded-[14px]" })}`}>
      <Text class="text-lg font-semibold">{!calendarLoaded() ? (connection() === "connected" ? "Loading calendar…" : "Calendar offline") : "Nothing on this day"}</Text>
      <If when={!calendarLoaded() && connection() !== "connected"}><Text class="text-center text-base text-muted-foreground">It shows when your computer is back.</Text></If>
    </View>
  );
}

/** The page's column: the phone's 16 gutters, the Mac's w-cal column. */
function Column(props: { testID: string; children: JSX.Element }) {
  return (
    <Scroll keyboardShouldPersistTaps="handled" testID={props.testID} class="flex-1 bg-background" contentClass={dense({ phone: "px-4 pt-1", window: "px-6 pt-1" })} contentStyle={{ paddingBottom: 40 }}>
      <View class="w-full self-center" style={{ maxWidth: pageColumnWidth("page", density() !== "phone") ?? undefined }}>{props.children}</View>
    </Scroll>
  );
}

/** "12 today" and the date (w-cal): today's own rows, not what earlier days left open. */
function Headline() {
  const count = () => agenda(calendarItems(), clock()).todayCount;
  return (
    <View testID="calendar-headline" class="mb-3 mt-2 flex-row flex-wrap items-baseline gap-2 px-0.5">
      <Text role="header" class={dense({ phone: "text-[30px] font-bold tracking-display", window: "text-[32px] font-bold tracking-display" })}>{count() ? `${count()} today` : "Nothing today"}</Text>
      <Text class="text-[15px] text-muted-foreground">{clock().format.day(clock().now, clock().zones.device)}</Text>
    </View>
  );
}

/**
 * Today (option 2, owner 2026-11-22): the headline, the one Triggers row, today's card (what ran and how it went, the
 * now line, what is still to come), Tomorrow, Later, and earlier days folded into one row at the end.
 */
function Today(props: { onOpen: (row: AgendaRow) => void; onTriggers: () => void }) {
  const a = () => agenda(calendarItems(), clock());
  const tomorrow = () => (tomorrowOpen() ? a().tomorrow : a().tomorrow.slice(0, TOMORROW_ROWS));
  const earlierLine = () => [`${a().done} done`, a().failed ? `${a().failed} failed` : ""].filter(Boolean).join(" · ");
  return (
    <Column testID="agenda">
      <If when={calendarLoaded()} else={<Empty day={() => null} />}>
        <Headline />
        <TriggersRow onOpen={props.onTriggers} />
        <DayCard testID="calendar-day-today" flush rows={() => a().today} now onOpen={props.onOpen} />
        <If when={a().tomorrow.length}>
          <DayCard testID="calendar-day-tomorrow" title="Tomorrow" rows={tomorrow} onOpen={props.onOpen}
            more={() => a().tomorrow.length - tomorrow().length} onMore={() => tomorrowOpen.set(true)} />
        </If>
        <If when={a().later.length}><DayCard testID="calendar-day-later" title="Later" rows={() => a().later} date onOpen={props.onOpen} /></If>
        <If when={a().earlier.length}>
          <Section testID="calendar-earlier">
            <TileRow testID="calendar-earlier-row" icon="check-circle" first={() => true} title={() => "Earlier"} line={earlierLine}
              chevron={earlierOpen() ? "up" : "down"} onPress={() => earlierOpen.set(!earlierOpen())}
              accessibilityLabel={() => `Earlier, ${earlierLine()}`} />
          </Section>
          <If when={earlierOpen()}>
            <Each of={a().earlier} key={(day: EarlierDay) => day.key}>{(day: () => EarlierDay) => (
              <DayCard testID={`calendar-day-${day().key}`} title={clock().format.day(day().rows[0]!.at, clock().zones.device)} rows={() => day().rows} onOpen={props.onOpen} />
            )}</Each>
          </If>
        </If>
      </If>
    </Column>
  );
}

/** Month: the month over the picked day's card (today's when none is picked). A past day shows only what is known. */
function MonthPage(props: { onOpen: (row: AgendaRow) => void }) {
  const day = () => pickedDay() ?? dayKey(clock());
  const rows = () => dayRows(calendarItems(), day(), clock());
  const title = () => {
    const c = clock();
    return day() === dayKey(c) ? "Today" : c.format.day(Date.parse(`${day()}T12:00:00Z`), "UTC");
  };
  return (
    <Column testID="agenda-month">
      <View class="mt-2"><Month /></View>
      <If when={rows().length} else={<View><View class="mb-2 mt-6 px-4"><Text role="header" class="text-caption font-semibold text-muted-foreground">{title()}</Text></View><Empty day={pickedDay} /></View>}>
        <DayCard testID={`calendar-day-${day()}`} title={title()} rows={rows} now={day() === dayKey(clock())} onOpen={props.onOpen} />
      </If>
    </Column>
  );
}

export function CalendarPage(props: { nav: NavStackState<Route>; openSession: (sessionId: string) => void }) {
  // Today ⇄ Month switches in place (SPEC §3.5); Today is the whole agenda again, no day picked. The two are
  // pages side by side that follow the finger (kit/TabPager.tsx).
  const show = (view: "today" | "month") => {
    if (view === "today") pickedDay.set(null);
    showMonth.set(view === "month");
  };
  const pager = createTabPager({ keys: ["today", "month"] as const, value: () => (showMonth() ? "month" : "today"), onChange: show });
  // A running row opens its Session while the computer lists it; a finished run its run page; the rest the item.
  const open = (row: AgendaRow) => {
    const run = row.run ?? (row.item.status === "running" ? activeRun(row.item) : undefined);
    const session = run?.status === "running" ? run.worker_session : undefined;
    if (session && workers().some((worker) => worker.id === session)) props.openSession(session);
    else if (run && run.status !== "running") props.nav.push({ name: "run", itemId: row.item.id, runId: run.id });
    else props.nav.push({ name: "item", id: row.item.id });
  };
  return (
    // The mock's header: Back · "Calendar" · the Today | Month control · +.
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
          today: () => <Today onOpen={open} onTriggers={() => props.nav.push({ name: "triggers" })} />,
          month: () => <MonthPage onOpen={open} />,
        }} />
        <CalendarEditor />
      </View>
    </NavPage>
  );
}
