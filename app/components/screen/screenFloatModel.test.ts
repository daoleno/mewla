import { describe, expect, test } from "bun:test";
import type { PhoneStatus } from "../../modules/mewla-phone/src";
import { encodeWorkerRouteId } from "../../services/workerRouteId";
import {
  displayShown,
  displaySource,
  floatSources,
  macSource,
  phoneOwner,
  phoneSource,
  pickSource,
  routeOwner,
  screenTarget,
} from "./screenFloatModel";

const workers = [
  { id: "%1", name: "Writer" },
  { id: "%2", name: "Coder" },
  { id: "%3", name: "Coder" },
];
const phoneStatus = { enabled: true, supported: true, paired: true, developer_options: true, wireless_debugging: true, wifi: true, helper: true, state: "ready", w: 720, h: 1600, agent: "Writer", handoff: null, self_healing: true } as unknown as PhoneStatus;
const display = { id: "%1", width: 1280, height: 800, control: "agent" as const, apps: 1 };

describe("route owner", () => {
  test("a Session is its Worker, a Work item is the Worker it runs on, other pages have none", () => {
    expect(routeOwner(`/terminal/${encodeWorkerRouteId("%152")}`, () => undefined)).toBe("%152");
    expect(routeOwner("/work/abc", (id) => (id === "abc" ? "%7" : undefined))).toBe("%7");
    expect(routeOwner("/work/none", () => undefined)).toBeNull();
    expect(routeOwner("/list", () => "%1")).toBeNull();
    expect(routeOwner("/terminal/garbage", () => undefined)).toBeNull();
    expect(routeOwner("/settings/phone", () => "%1")).toBeNull();
  });
});

describe("phone source", () => {
  test("is the owner when its name or id matches one Worker, ownerless otherwise", () => {
    expect(phoneOwner("%2", workers)).toBe("%2");
    expect(phoneOwner("Writer", workers)).toBe("%1");
    expect(phoneOwner("Coder", workers)).toBeNull();
    expect(phoneOwner("An agent", workers)).toBeNull();
  });
  test("exists only while an agent works the phone and its screen has a size", () => {
    expect(phoneSource(phoneStatus, workers)?.owner).toBe("%1");
    expect(phoneSource({ ...phoneStatus, agent: null }, workers)).toBeNull();
    expect(phoneSource({ ...phoneStatus, state: "stopped" }, workers)).toBeNull();
    expect(phoneSource({ ...phoneStatus, w: 0, h: 0 }, workers)).toBeNull();
    expect(phoneSource(null, workers)).toBeNull();
  });
});

describe("display shown", () => {
  const run = { running: true, shown: false, holding: false };
  test("appears while its Worker runs and used it a moment ago", () => {
    expect(displayShown({ idle_s: 5 }, run)).toBe(true);
    expect(displayShown({ idle_s: 61 }, run)).toBe(false);
    expect(displayShown({}, run)).toBe(true);
    expect(displayShown({ idle_s: 5 }, { ...run, running: false })).toBe(false);
  });
  test("stays up between actions while the Worker runs, not for ever", () => {
    expect(displayShown({ idle_s: 300 }, { ...run, shown: true })).toBe(true);
    expect(displayShown({ idle_s: 20 * 60 }, { ...run, shown: true })).toBe(false);
    expect(displayShown({ idle_s: 300 }, { ...run, shown: true, running: false })).toBe(false);
  });
  test("a person holding it keeps it up; a Stop takes it away", () => {
    expect(displayShown({ idle_s: 3600 }, { running: false, shown: true, holding: true })).toBe(true);
    expect(displayShown({ stopped: true, idle_s: 0 }, { ...run, holding: true })).toBe(false);
  });
});

describe("which screens the window may show", () => {
  const phone = phoneSource(phoneStatus, workers)!;
  const mine = displaySource(display, "Writer");
  const other = displaySource({ ...display, id: "%2" });
  const mac = macSource({ worker: "%2", since: "" });
  const all = [phone, mine, other, mac];

  test("a Session shows its Worker's screens only", () => {
    expect(floatSources(all, { pathname: "/terminal/x", owner: "%1" }).map((s) => s.key)).toEqual(["phone", "display:%1"]);
    expect(floatSources(all, { pathname: "/terminal/x", owner: "%2" }).map((s) => s.key)).toEqual(["display:%2", "mac:%2"]);
  });
  test("a screen of an unknown Worker shows in every Session", () => {
    const loose = { ...phone, owner: null };
    expect(floatSources([loose], { pathname: "/terminal/x", owner: "%9" })).toHaveLength(1);
  });
  test("a page that belongs to no Worker shows them all", () => {
    expect(floatSources(all, { pathname: "/list", owner: null })).toHaveLength(4);
  });
  test("a screen hides on its own full view", () => {
    expect(floatSources(all, { pathname: "/phone", owner: null }).map((s) => s.key)).toEqual(["display:%1", "display:%2", "mac:%2"]);
    expect(floatSources(all, { pathname: "/screen", owner: null }).map((s) => s.key)).toEqual(["phone"]);
    expect(floatSources(all, { pathname: "/screenshot-demo", owner: null })).toHaveLength(4);
  });
  test("the picked one shows while it is there, else the first", () => {
    expect(pickSource(all, "display:%2")?.key).toBe("display:%2");
    expect(pickSource(all, "gone")?.key).toBe("phone");
    expect(pickSource([], null)).toBeNull();
  });
});

describe("what opens a screen", () => {
  test("a display opens its viewer route; a Mac only watches that Worker, it never takes the screen", () => {
    expect(screenTarget(displaySource(display))).toEqual({ watch: false, route: { path: "/display/viewer", purpose: "mewla-display-view", params: { worker: "%1" } } });
    expect(screenTarget(macSource({ worker: "%2", since: "" }))).toEqual({ watch: true, worker: "%2" });
    expect(screenTarget({ kind: "phone", owner: "%1" })).toBeNull();
  });
});
