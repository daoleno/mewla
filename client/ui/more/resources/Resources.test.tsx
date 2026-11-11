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

  test("with no computer it sends the person to Settings to pair one", async () => {
    const app = await open("./NoServerHarness.tsx", { host: { handlers: { "app.daemon": () => ({ error: "no computer" }) } } });
    await app.waitFor("text:No current server");
    expect(app.texts()).toContain("Pair a computer in Settings to see its resources.");
    await app.press("text:Open Settings");
    await app.waitFor("id:settings");
  });

  test("every chart is named for a screen reader, and CPU has its pressure-state strip", async () => {
    const app = await open(HARNESS, computer(() => fresh()).options);
    await app.waitFor("id:resources-headline");
    for (const label of [
      "CPU utilization history, scale 0–100%",
      "Memory utilization history, scale 0–100%",
      "Disk read throughput history",
      "Disk write throughput history",
      "PSI CPU some history, scale 0–100%",
    ]) expect(app.shows(`label:${label}`)).toBe(true);
    expect(app.shows("id:cpu-pressure-strip")).toBe(true);
    // Disk read and write are keyed, and the PSI micro line stands apart from the caption.
    expect(app.texts()).toContain("CPU · max some 10s");
    expect(app.texts().some((t) => t.startsWith("R ") && !t.includes("·"))).toBe(true);
    expect(app.texts()).toContain("CPU 8.4% · Mem 3.2% · I/O 1.1%");
  });

  test("machine details show a PSI history per resource; the toggle says whether it is open", async () => {
    const app = await open(HARNESS, computer(() => fresh()).options);
    await app.waitFor("id:resources-headline");
    expect(app.shows("label:Machine details")).toBe(true);
    expect(app.shows("label:Mem PSI some history, scale 0–100%")).toBe(false);
    await app.press("id:resources-details");
    for (const key of ["CPU", "Mem", "I/O"]) expect(app.shows(`label:${key} PSI some history, scale 0–100%`)).toBe(true);
    expect(app.texts()).toContain("some / full · 10s / 1m / 5m");
  });

  test("a Mac shows its memory pressure level on a three-step meter", async () => {
    const mac = () => ({ ...fresh(), psi: {}, memory_pressure: { level: "warn", free_percent: 41.6 }, cpu: { load1: 2.14, per_core_percent: [10, 20, 30, 40], utilization_percent: 25 } });
    const app = await open(HARNESS, computer(mac).options);
    await app.waitFor("id:resources-headline");
    expect(app.shows("label:Memory pressure Warning")).toBe(true);
    expect(app.shows("label:Memory pressure history, from normal to critical")).toBe(true);
    expect(app.texts()).toContain("Warning");
    expect(app.texts()).toContain("Memory pressure");
    expect(app.texts()).toContain("42% free · Load 2.1 · 4 cores");
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
    await app.waitFor("text:Last sample · offline", { timeout: 2_000 }).catch(() => {});
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
    await app.longPress("id:consumer-details");
    expect(app.dialogs().at(-1)?.buttons).toEqual(["Copy title", "Copy ID", "Copy Worker ID", "Copy Work ID", "Copy commands", "Cancel"]);
    await app.waitUntil(() => app.clipboard() === "worker-build");
    choice = "Copy PID";
    await app.longPress("id:process-18420");
    await app.waitUntil(() => app.clipboard() === "18420");
    choice = "Copy start token";
    await app.longPress("id:process-18420");
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
