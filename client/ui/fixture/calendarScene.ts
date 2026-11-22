// Calendar's real-shaped fixture (owner 2026-11-22): invented English topics in the shape of a real Calendar, for the
// shots and the Mac's captures. 56 items (repeats Brain runs, one Mewla runs itself, reminders, an event; done,
// failed, cancelled, running) and 18 Triggers (five noticed something today, one whose check keeps failing).
// A scene pins the fixture's clock: `busy` at 10:44 or 22:00, or `quiet`. Imported before the fixture app reads
// its scenario (FixtureApp's and MacFixtureApp's first imports), never by the app.

export type CalendarScene = { kind: "busy" | "quiet"; at: string };

let scene: CalendarScene | null = null;

/** The scene the fixture's scenario takes its Calendar and Triggers from; null keeps the mock's. */
export function calendarScene(): CalendarScene | null {
  return scene;
}

/**
 * Sets the scene and moves the fixture's clock to today at `at` (local time): `Date.now()` and `new Date()` run on
 * from there. The fixture's own data is built from that clock.
 */
export function useCalendarScene(next: CalendarScene): void {
  scene = next;
  const Real = Date;
  const [h, m] = next.at.split(":").map(Number);
  const target = new Real(Real.now());
  target.setHours(h, m, 0, 0);
  const offset = target.getTime() - Real.now();
  class Pinned extends Real {
    constructor(...args: unknown[]) {
      if (args.length === 0) super(Real.now() + offset);
      else super(...(args as [number]));
    }
    static now(): number {
      return Real.now() + offset;
    }
  }
  (globalThis as { Date: DateConstructor }).Date = Pinned as unknown as DateConstructor;
}

const MIN = 60_000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;

function zone(): string {
  try { return new Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC"; } catch { return "UTC"; }
}

/** Day `days` from today at hh:mm, local time. */
function at(hhmm: string, days = 0): Date {
  const [h, m] = hhmm.split(":").map(Number);
  const d = new Date();
  d.setDate(d.getDate() + days);
  d.setHours(h, m, 0, 0);
  return d;
}

const iso = (d: Date | number) => new Date(d).toISOString();

type Item = Record<string, unknown>;
let serial = 0;
const id = (prefix: string) => `${prefix}-${String(++serial).padStart(3, "0")}`;

function base(title: string, kind: string, when: Date, extra: Item = {}): Item {
  const key = kind === "reminder" ? "notify_at" : kind === "event" ? "start_at" : "due_at";
  return {
    id: id("cal"), title, kind, status: "scheduled", [key]: iso(when), next_at: iso(when), timezone: zone(), recurrence: "none",
    created_at: iso(Date.now() - 40 * DAY), updated_at: iso(Date.now() - DAY), revision: 3, ...extra,
  };
}

function run(scheduled: Date, status: string, extra: Item = {}): Item {
  const started = scheduled.getTime() + 1000;
  return {
    id: id("run"), scheduled_for: iso(scheduled), started_at: iso(started), status,
    ...(status === "running" ? { worker_session: "mh-fixture-running" } : { finished_at: iso(started + 6 * MIN) }), ...extra,
  };
}

/** A repeat Brain runs every day at hh:mm: its last ten runs, today's as the clock has it, next time after now. */
function daily(title: string, hhmm: string, results: string[], today?: Item, long = false): Item {
  const now = Date.now();
  const slotToday = at(hhmm);
  const runs: Item[] = [];
  for (let days = -9; days < 0; days++) runs.push(run(at(hhmm, days), "completed", { result_preview: results[(days + 9) % results.length] }));
  let next = slotToday;
  if (slotToday.getTime() <= now) {
    // A long job is still at it within its first hour; the rest are done in minutes.
    const running = long && now - slotToday.getTime() < HOUR;
    runs.push(run(slotToday, running ? "running" : today?.status === "failed" ? "failed" : "completed", running ? {} : today ?? { result_preview: results[0] }));
    if (!running) next = at(hhmm, 1);
  }
  const live = runs.at(-1)!.status === "running";
  return base(title, "scheduled_action", next, {
    recurrence: "daily", status: live ? "running" : "scheduled", runs: runs.slice(-10), summary: title, action_instruction: `${title}.`, action_cwd: "/home/me/notes",
  });
}

/** A one-off Brain ran on an earlier day: done, or failed and why. */
function ranOnce(title: string, when: Date, failed?: string, result?: string): Item {
  return base(title, "scheduled_action", when, {
    status: failed ? "failed" : "completed", ...(failed ? { failure_reason: failed } : {}), summary: title,
    runs: [run(when, failed ? "failed" : "completed", failed ? { failure_reason: failed } : { result_preview: result ?? "Done as asked" })],
  });
}

function busyItems(): Item[] {
  const items: Item[] = [];
  // Repeats Brain runs, and their runs; the thermostat's run today went wrong and its agent said so (`outcome`).
  items.push(daily("Weather and commute digest", "06:50", ["Rain after 17:00", "Clear all day", "Trains on time"]));
  items.push(daily("Garden sensor review", "07:40", ["Soil dry in bed 2", "All beds fine", "Sensor 4 battery low"]));
  items.push(daily("Library update check", "09:10", ["Nothing new", "One minor update, applied"]));
  items.push(daily("Spare room thermostat check", "09:10", ["Holding 19°C", "Holding 20°C"],
    Date.now() > at("09:10").getTime() ? { outcome: "failed", result_preview: "The thermostat did not report in." } : undefined));
  items.push(daily("Photo archive sorting (09:50)", "09:50", ["212 sorted, 3 duplicates", "180 sorted"], undefined, true));
  items.push(daily("Photo archive sorting (15:20)", "15:20", ["96 sorted", "140 sorted, 1 unreadable"], undefined, true));
  items.push(daily("Photo archive sorting (21:40)", "21:40", ["64 sorted", "77 sorted"], undefined, true));
  items.push(daily("Plant journal: today's photos", "21:10", ["6 photos filed", "Nothing new today", "3 photos filed"]));
  // The weekly review, on today's weekday.
  const weekly = at("18:00");
  const weeklyRan = weekly.getTime() <= Date.now();
  items.push(base("Weekly reading review", "scheduled_action", weeklyRan ? at("18:00", 7) : weekly, {
    recurrence: "weekly", summary: "Weekly reading review",
    runs: [...[-21, -14, -7].map((d) => run(at("18:00", d), "completed", { result_preview: "Read 4 of 6 saved articles" })), ...(weeklyRan ? [run(weekly, "completed", { result_preview: "Read 5 of 7 saved articles" })] : [])],
  }));
  // Mewla's own morning brief: a daily reminder Mewla acts on itself.
  items.push(base("Morning brief", "reminder", at("07:15", at("07:15").getTime() <= Date.now() ? 1 : 0), { recurrence: "daily", owner: "watch_brief" }));
  // Today's one-offs.
  items.push(ranOnce("Search index: overnight consistency check", at("02:15"), undefined, "All 4 restores matched"));
  items.push(ranOnce("Invoices: reconcile last month", at("08:05"), undefined, "412 matched, 5 left"));
  // Later.
  items.push(base("Rotate the shared Wi-Fi password", "scheduled_action", at("11:20", 11), { summary: "Rotate the shared Wi-Fi password" }));
  items.push(base("Renew the car insurance", "reminder", at("09:40", 72)));
  // Earlier days: 24 done, 5 failed.
  const lost = "Mewla restarted while this ran and lost its agent.";
  const silent = "The agent finished without writing its result.";
  for (let unit = 1; unit <= 10; unit++) {
    const failed = unit === 4 ? silent : unit === 9 ? lost : undefined;
    items.push(ranOnce(`Language lesson, unit ${unit}`, at("06:45", -29 + unit), failed, `Unit ${unit} notes saved`));
  }
  for (const [day, n] of [[-12, 1], [-11, 2], [-10, 3]] as const) items.push(ranOnce(`Fence repair quotes: day ${n}`, at("13:00", day), undefined, `${n + 1} quotes so far`));
  items.push(base("Dentist", "event", at("15:30", -20), { status: "completed", end_at: iso(at("16:15", -20)) }));
  items.push(base("Leave for the dentist", "reminder", at("15:15", -20), { status: "completed" }));
  items.push(base("Renew the library card", "reminder", at("11:30", -3), { status: "completed" }));
  items.push(base("Return the drill", "reminder", at("18:00", -7), { status: "completed" }));
  items.push(base("Hand over the spare key", "reminder", at("17:30", -6), { status: "completed" }));
  items.push(base("Clean the gutters", "reminder", at("10:15", -18), { status: "completed" }));
  items.push(base("Pick up the dry cleaning", "reminder", at("17:45", -1), { status: "completed" }));
  items.push(ranOnce("Passport photo check", at("12:10", -5), undefined, "Meets the size rules"));
  items.push(ranOnce("Recipe box: sort by season", at("19:45", -9), undefined, "84 recipes sorted"));
  items.push(ranOnce("Holiday price watch", at("07:20", -2), undefined, "Prices down 6%"));
  items.push(ranOnce("Bookshelf inventory", at("16:05", -13), undefined, "312 books listed"));
  items.push(ranOnce("Warranty scan", at("14:45", -8), undefined, "Two warranties end this year"));
  items.push(ranOnce("Tax receipts sorted", at("09:05", -22), undefined, "All receipts filed"));
  items.push(ranOnce("Car service booking", at("08:40", -14), silent));
  items.push(ranOnce("Router reboot test", at("23:10", -16), lost));
  items.push(ranOnce("Wi-Fi speed log", at("22:40", -4), "The router did not answer."));
  // Cancelled: never shown.
  for (const [title, days] of [["Paint the fence", -50], ["Sort the garage", -41], ["Buy a new kettle", -37], ["Order seeds", -33], ["Clean the oven", -30], ["Call the plumber", -27], ["Book a haircut", -26], ["Check the smoke alarms", -21], ["Fix the gate latch", -19], ["Update the photo frame", -15], ["Plan the picnic", -11], ["Swap the winter tyres", -9], ["Back up the old phone", -6]] as const) {
    items.push(base(title, days % 2 ? "reminder" : "scheduled_action", at("12:00", days), { status: "cancelled", cancelled_at: iso(at("12:00", days + 1)) }));
  }
  return items;
}

function quietItems(): Item[] {
  // Tomorrow's weekday, so nothing falls today.
  return [
    base("Water the plants", "reminder", at("08:00", 1), { recurrence: "weekly" }),
    base("Renew the car insurance", "reminder", at("09:40", 72)),
  ];
}

type Firing = { seq: number; at: string; outputs: number; payload: string; event_id: string; handled_at: string };

function firings(times: [number, string, number?][]): Firing[] {
  return times.map(([days, hhmm, outputs], i) => {
    const when = at(hhmm, days);
    return { seq: i + 1, at: iso(when), outputs: outputs ?? 1, payload: "", event_id: `ev-${i}`, handled_at: iso(when.getTime() + 9 * MIN) };
  }).filter((f) => Date.parse(f.at) <= Date.now());
}

function trigger(title: string, every: number, fired: [number, string, number?][], extra: Item = {}): Item {
  const list = firings(fired);
  return {
    // Named by its title, so a capture can open one ("trg-home-server-disk-health").
    id: `trg-${title.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "")}`, title, check: `./checks/${title.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "")}.sh`, cwd: "/home/me/watch",
    interval_seconds: every, timeout_seconds: 120, instruction: `When ${title.toLowerCase()} changes, say what changed in the thread and open a Work if something needs doing.`,
    work_id: id("work"), created_at: iso(Date.now() - 6 * DAY), updated_at: iso(Date.now() - 2 * MIN), revision: 1,
    last_checked_at: iso(Date.now() - 2 * MIN), fire_seq: list.length, firings: list, state: "watching", ...extra,
  };
}

function busyTriggers(): Item[] {
  return [
    trigger("Home server disk health", 300, [[-2, "19:35", 2], [0, "06:05"]], {
      failing: true, failures: 3, failing_since: iso(Date.now() - 14 * MIN), last_failure: "the disk did not answer", state: "failing",
      instruction: "Read the disk report, say in the thread which drive is at risk, and open a Work to replace it if one is failing.",
    }),
    trigger("New issues on the garden app", 600, [[-1, "17:05"], [0, "01:20"], [0, "02:40"], [0, "04:15"], [0, "05:50"], [0, "07:30"], [0, "08:55"], [0, "10:18", 2]]),
    trigger("Standing desk price drop", 1800, [[-2, "13:40"], [0, "06:30"], [0, "08:10"], [0, "10:02"]]),
    trigger("Package tracking: the camera lens", 600, [[0, "09:34"]]),
    trigger("Forum mentions of the garden app", 720, [[-1, "20:15"], [0, "06:50"], [0, "09:05"]]),
    trigger("Shared family calendar changes", 600, [[0, "00:25"], [0, "07:48"]]),
    trigger("Library holds ready for pickup", 3600, [[-1, "15:12"]]),
    trigger("Router firmware updates", 43200, [[-1, "11:40"]]),
    trigger("Podcast feed: new episodes", 3600, [[-2, "09:25"]]),
    trigger("Recipe site comments", 600, [[-3, "12:30"]]),
    trigger("Train timetable changes", 120, [[-3, "18:52"]]),
    trigger("Domain renewal notices", 43200, [[-4, "14:05"]]),
    trigger("Air quality above 100", 600, [[-6, "07:42"]]),
    trigger("Neighbourhood bulletin", 900, [[-8, "16:20"]]),
    trigger("Bank statement arrives", 1800, []),
    trigger("Concert tickets on sale", 600, []),
    trigger("Passport appointment slots", 120, []),
    trigger("Bike shop restock", 600, []),
  ];
}

function quietTriggers(): Item[] {
  return [
    trigger("Library holds ready for pickup", 3600, [[-1, "16:31"]]),
    trigger("Router firmware updates", 43200, []),
    trigger("Air quality above 100", 600, [[-5, "08:15"]]),
  ];
}

/** The scene's Calendar items and Triggers, built on the pinned clock. */
export function calendarSceneData(current: CalendarScene): { calendar_items: Item[]; triggers: Item[] } {
  serial = 0;
  return current.kind === "quiet"
    ? { calendar_items: quietItems(), triggers: quietTriggers() }
    : { calendar_items: busyItems(), triggers: busyTriggers() };
}
