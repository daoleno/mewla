import { describe, expect, open, test } from "nsh/test";
import { answering } from "../../testing/fixtures";
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
  test("shows the machine: its name, the pressure in words, the four tiles and who uses it", async () => {
    const c = computer(() => fresh());
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:resources-headline");
    for (const tile of ["tile-cpu", "tile-memory", "tile-disk", "tile-swap", "consumers"]) expect(app.shows(`id:${tile}`)).toBe(true);
    const texts = app.texts();
    expect(texts).toContain("Desk");
    expect(texts).toContain("Elevated pressure");
    expect(texts).toContain("Memory under pressure");
    expect(texts.some((t) => t.startsWith("· updated "))).toBe(true);
    // The daemon's memory signal is over its threshold: its tile says High beside the number; the others say nothing.
    expect(app.shows("id:resources-line-dot")).toBe(true);
    expect(app.shows("id:tile-memory-status")).toBe(true);
    expect(app.shows("id:tile-cpu-status")).toBe(false);
    expect(app.shows("id:tile-disk-status")).toBe(false);
    // CPU 72% of 8 cores; 27 GB of 32 GB; the root disk 367 GB of 512 GB; swap 2 GB of 8 GB.
    for (const text of ["72%", "8 cores", "27 GB", "High", "· of 32 GB", "367 GB", "of 512 GB", "2 GB", "of 8 GB"]) expect(texts).toContain(text);
    expect(texts.join(" ")).toContain("Using this machine");
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

  test("with no computer it sends the person to Settings to pair one", async () => {
    const app = await open("./NoServerHarness.tsx", { host: { handlers: { "app.daemon": () => ({ error: "no computer" }) } } });
    await app.waitFor("text:No current server");
    expect(app.texts()).toContain("Pair a computer in Settings to see its resources.");
    await app.press("text:Open Settings");
    await app.waitFor("id:settings");
  });

  test("the raw metric and the kernel's pressure figures are only behind ⓘ, and no tile draws a chart", async () => {
    const app = await open(HARNESS, computer(() => fresh()).options);
    await app.waitFor("id:resources-headline");
    const raw = "psi.memory.some.avg10 · 3.2 / threshold 2";
    expect(app.texts()).not.toContain(raw);
    expect(app.shows("label:CPU utilization history, scale 0–100%")).toBe(false);
    expect(app.shows("label:Machine details")).toBe(true);
    await app.press("id:resources-details");
    await app.waitFor("id:resources-machine-details");
    expect(app.texts()).toContain(raw);
    expect(app.texts()).toContain("PSI some / full · 10s / 1m / 5m");
    for (const key of ["CPU", "Mem", "I/O"]) expect(app.texts()).toContain(key);
    expect(app.texts()).toContain("Load · 1 / 5 / 15 min");
    await app.press("id:resources-details");
    expect(app.shows("id:resources-machine-details")).toBe(false);
  });

  test("a Mac's memory pressure level is said in words, its level behind ⓘ", async () => {
    const mac = () => ({ ...fresh(), signals: [], psi: {}, memory_pressure: { level: "warn", free_percent: 41.6 }, cpu: { load1: 2.14, per_core_percent: [10, 20, 30, 40], utilization_percent: 25 } });
    const app = await open(HARNESS, computer(mac).options);
    await app.waitFor("id:resources-headline");
    expect(app.texts()).toContain("Memory under pressure");
    expect(app.shows("id:tile-memory-status")).toBe(true);
    expect(app.texts()).toContain("4 cores");
    await app.press("id:resources-details");
    await app.waitFor("id:resources-machine-details");
    expect(app.texts()).toContain("Memory pressure");
    expect(app.texts()).toContain("Warning");
    expect(app.texts()).toContain("42% free · Load 2.1 · 4 cores");
  });

  test("a comfortable machine says its pressure is low", async () => {
    const calm = () => ({ ...fresh(), state: "normal", signals: [] });
    const app = await open(HARNESS, computer(calm).options);
    await app.waitFor("id:resources-headline");
    expect(app.texts()).toContain("Comfortable");
    expect(app.texts()).toContain("Pressure low");
    expect(app.shows("id:resources-line-dot")).toBe(true);
    expect(app.shows("id:tile-memory-status")).toBe(false);
  });

  test("Refresh reads at once, and is off while the computer is offline", async () => {
    const c = computer(() => fresh());
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:resources-headline");
    expect(c.calls()).toBe(1);
    await app.press("label:Refresh resources");
    await app.wait(50);
    expect(c.calls()).toBe(2);
    await app.host.send("app.daemonState", "waiting");
    await app.wait(50);
    expect(app.enabled("id:resources-refresh")).toBe(false);
  });

  test("consumer rows show owner, status and residue, and read aloud whether they are open", async () => {
    const app = await open(HARNESS, computer(() => fresh()).options);
    await app.waitFor("id:consumers");
    const texts = app.texts();
    expect(texts).toContain("running · codex · /workspace/mobile");
    expect(texts.some((t) => t.startsWith("Residual · done"))).toBe(true);
    expect(texts).toContain("CPU");
    expect(texts).toContain("Memory");
    expect(app.shows("label:Filter Workers")).toBe(true);
    expect(app.shows("label:Expand Android release build, Worker, running, CPU 245%, RSS 7 GB, 18 processes")).toBe(true);
    await app.press("id:consumer-worker:worker:android-build");
    expect(app.shows("label:Collapse Android release build, Worker, running, CPU 245%, RSS 7 GB, 18 processes")).toBe(true);
    expect(app.texts()).toContain("Command / start token");
    expect(app.texts()).toContain("start 918204");
  });

  test("a consumer's title, IDs and commands, and a process's PID, command and start token can be copied", async () => {
    const app = await open(HARNESS, computer(() => fresh()).options);
    await app.waitFor("id:consumers");
    await app.press("id:consumer-worker:worker:android-build");
    let choice = "Copy Worker ID";
    app.onDialog((dialog) => (dialog.sheet ? choice : null));
    await answering(app, () => app.longPress("id:consumer-details"));
    expect(app.dialogs().at(-1)?.buttons).toEqual(["Copy title", "Copy ID", "Copy Worker ID", "Copy Work ID", "Copy commands", "Cancel"]);
    await app.waitUntil(() => app.clipboard() === "worker-build");
    choice = "Copy PID";
    await answering(app, () => app.longPress("id:process-18420"));
    await app.waitUntil(() => app.clipboard() === "18420");
    choice = "Copy start token";
    await answering(app, () => app.longPress("id:process-18420"));
    await app.waitUntil(() => app.clipboard() === "918204");
  });

  test("rows with the same identity open on their own", async () => {
    const twin = { owner: "worker", id: "worker:twin", title: "Twin", status: "running", rss_bytes: 1024, process_count: 1 };
    const app = await open(HARNESS, computer(() => ({ ...fresh(), consumers: [twin, twin] })).options);
    await app.waitFor("id:consumers");
    expect(app.count("label:Expand Twin, Worker, running, CPU —, RSS 1 KB, 1 processes")).toBe(2);
    await app.press("label:Expand Twin, Worker, running, CPU —, RSS 1 KB, 1 processes >> nth=0");
    expect(app.count("label:Collapse Twin, Worker, running, CPU —, RSS 1 KB, 1 processes")).toBe(1);
    expect(app.count("label:Expand Twin, Worker, running, CPU —, RSS 1 KB, 1 processes")).toBe(1);
  });

  test("a wide screen puts four tiles across and a Procs column in the table", async () => {
    const app = await open(HARNESS, computer(() => fresh()).options);
    await app.waitFor("id:consumers");
    expect(app.texts()).not.toContain("Procs");
    await app.setViewport(1280, 900);
    await app.wait(50);
    expect(app.texts()).toContain("Procs");
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
