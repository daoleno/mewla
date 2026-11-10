import { describe, expect, open, test } from "nsh/test";
import dashboard from "../../../core/resources/fixtures/dashboard-v2.json";

const HARNESS = "../testing/Harness.tsx";

function computer(answer: (n: number) => unknown, state = "connected") {
  let n = 0;
  return {
    calls: () => n,
    options: {
      clock: {},
      host: {
        params: { page: { name: "resources" }, state },
        handlers: { "app.daemon": ({ type }: { type: string }) => (type === "get_resource_telemetry" ? answer(n++) : { error: `unexpected ${type}` }) },
      },
    },
  };
}

const fresh = () => ({ ...dashboard, sampled_at: new Date().toISOString() });

describe("Resources", () => {
  test("shows the machine: headline, the four tiles and who uses it", async () => {
    const c = computer(() => fresh());
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:resources-headline");
    for (const tile of ["tile-cpu", "tile-memory", "tile-disk", "tile-pressure", "consumers"]) expect(app.shows(`id:${tile}`)).toBe(true);
    expect(app.texts().join(" ")).toContain("Using this machine");
    expect(app.texts().join(" ")).toContain("CPU 100% is one core.");
  });

  test("reads every five seconds while the app is in front, and stops in the background", async () => {
    const c = computer(() => fresh());
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:resources-headline");
    expect(c.calls()).toBe(1);
    await app.wait(5_100);
    expect(c.calls()).toBe(2);
    await app.setLifecycle("background");
    await app.wait(20_000);
    expect(c.calls()).toBe(2);
    await app.setLifecycle("active");
    await app.wait(100);
    expect(c.calls()).toBe(3);
  });

  test("a failed refresh keeps the last reading and says so; the next one clears it", async () => {
    const c = computer((n) => (n === 1 ? { error: "Resource telemetry timed out." } : fresh()));
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:resources-headline");
    await app.wait(5_100);
    await app.waitFor("id:resources-refresh-failed");
    expect(app.shows("id:tile-cpu")).toBe(true);
    await app.wait(5_100);
    expect(app.shows("id:resources-refresh-failed")).toBe(false);
  });

  test("offline without a reading says when resources come back, and asks nothing", async () => {
    const c = computer(() => fresh(), "waiting");
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Server offline");
    expect(c.calls()).toBe(0);
    await app.host.send("app.daemonState", "connected");
    await app.waitFor("id:resources-headline");
  });

  test("a computer that reports nothing offers to try again", async () => {
    const c = computer((n) => (n === 0 ? { error: "The first resource sample is not available yet" } : fresh()));
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Resources unavailable");
    expect(app.texts()).toContain("The first resource sample is not available yet");
    await app.press("id:resources-failed-action");
    await app.waitFor("id:resources-headline");
  });

  test("a switch to another computer drops the reading", async () => {
    const c = computer((n) => (n === 0 ? fresh() : { error: "not yet" }));
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:resources-headline");
    await app.host.send("app.daemonSwitch", { id: "s2", name: "Laptop" });
    await app.waitFor("text:Resources unavailable");
    expect(app.shows("id:tile-cpu")).toBe(false);
  });
});
