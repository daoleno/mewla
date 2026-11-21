import { describe, expect, open, test } from "nsh/test";
import { framesOf, paired, phoneHost } from "../testing/fixtures";

const report = {
  id: "1762818467989-java-12.txt",
  kind: "java",
  at: 1762818467989,
  text: "Mewla 0.3.0 (30000, release) com.daoleno.mewla.preview\nOnePlus PYR110, Android 16 (API 36), arm64-v8a\njava at 2026-11-10T23:47:47Z\n\njava.lang.NullPointerException: boom\n\tat com.daoleno.mewla.QrScanner.start(QrScanner.kt:45)",
};

function host(reports: unknown[]) {
  return {
    handlers: {
      "secure.get": () => null,
      "secure.set": () => undefined,
      "secure.remove": () => undefined,
      "app.crashReports": () => reports,
      "app.clearCrashReports": () => null,
      "scan.permission": () => "denied",
    },
  };
}

// Three runs that ended within ten minutes: a crash loop (core/crash.ts CRASH_LOOP).
const loop = [report, { ...report, id: "1762818527989-java-13.txt", at: report.at + 60_000 }, { ...report, id: "1762818587989-native-14.txt", kind: "native", at: report.at + 120_000 }];

// The owner's of 11-14 (v0.4.2): eight 2 s cut-offs in two minutes, the app running all along.
const cutOffs = Array.from({ length: 8 }, (_, i) => ({
  id: `17946716${i}0000-js-${i}.txt`, kind: "js", at: Date.UTC(2026, 10, 14, 15, 53, 55) + i * 15_000,
  text: `Mewla 0.4.2 (40299, release) com.daoleno.mewla\nOnePlus PYR110, Android 16 (API 36), arm64-v8a\njs at 2026-11-14T15:55:41Z\n\nthe script was interrupted: a call into JS exceeded its budget of 2000 ms\nError: interrupted\n    at filter (native)`,
}));

describe("a crash loop last time", () => {
  test("shows on the next start with the traces, Copy and Dismiss; Dismiss clears them in the host", async () => {
    const app = await open("../App.tsx", { host: host(loop), platform: "android" });
    await app.waitFor("id:crash-sheet");
    expect(app.texts()).toContain("Mewla closed unexpectedly");
    expect(app.texts().join(" ")).toContain("java.lang.NullPointerException: boom");
    await app.press("id:crash-copy");
    expect(app.clipboard()).toContain("QrScanner.kt:45");
    await app.press("id:crash-dismiss");
    const clear = app.host.calls().find((call) => call.name === "app.clearCrashReports");
    expect((clear?.data as { ids: string[] }).ids.sort()).toEqual(loop.map((r) => r.id).sort());
    await app.waitUntil(() => !app.shows("id:crash-sheet"));
  });

  test("nothing kept, nothing shown", async () => {
    const app = await open("../App.tsx", { host: host([]), platform: "android" });
    await app.waitFor("id:set-up");
    expect(app.shows("id:crash-sheet")).toBe(false);
  });
});

describe("what the app survived (owner 11-14: \"Mewla hit an error\" over the app, eight times a 2 s cut-off)", () => {
  test("one crash shows nothing", async () => {
    const app = await open("../App.tsx", { host: host([report]), platform: "android" });
    await app.waitFor("id:set-up");
    await app.wait(300);
    expect(app.shows("id:crash-sheet")).toBe(false);
  });

  test("cut-offs show nothing: each goes to the computer once, then the host forgets them", async () => {
    const app = await open("../FakeDaemonApp.tsx", { platform: "android", storage: paired, host: phoneHost({ brain: {} }, { "app.crashReports": () => cutOffs, "app.clearCrashReports": () => null }) });
    await app.waitUntil(() => framesOf(app.host.posts(), "client_diagnostic").filter((f) => f.name === "app_crash").length === 8, { timeout: 10_000 });
    expect(framesOf(app.host.posts(), "client_diagnostic").map((f) => f.request_id).sort()).toEqual(cutOffs.map((r) => r.id).sort());
    await app.waitUntil(() => app.host.calls().some((call) => call.name === "app.clearCrashReports"), { timeout: 5000 });
    const clear = app.host.calls().find((call) => call.name === "app.clearCrashReports");
    expect((clear?.data as { ids: string[] }).ids.sort()).toEqual(cutOffs.map((r) => r.id).sort());
    expect(app.shows("id:crash-sheet")).toBe(false);
  });

  test("a loop whose runs reconnect in between still shows on the third: a run that ended is not forgotten once sent", async () => {
    // One host across three runs: each crash is kept, the app starts and connects, and the next run crashes again.
    const store: (typeof report)[] = [];
    const runHost = () => phoneHost({ brain: {} }, {
      "app.crashReports": () => store.slice(),
      "app.clearCrashReports": ({ ids }: { ids: string[] }) => { for (const id of ids) store.splice(store.findIndex((r) => r.id === id), 1); return null; },
    });
    const crash = (n: number) => ({ ...report, id: `${Date.now()}-java-${n}.txt`, at: Date.now() - (3 - n) * 60_000 });
    for (const n of [1, 2]) {
      store.push(crash(n), { ...cutOffs[n]!, id: `cut-${n}`, at: Date.now() });
      const kept = store.length;
      const app = await open("../FakeDaemonApp.tsx", { platform: "android", storage: paired, host: runHost() });
      await app.waitUntil(() => framesOf(app.host.posts(), "client_diagnostic").filter((f) => f.name === "app_crash").length === kept, { timeout: 10_000 });
      await app.waitUntil(() => !store.some((r) => r.id === `cut-${n}`), { timeout: 5000 });
      expect(app.shows("id:crash-sheet")).toBe(false);
      expect(store.map((r) => r.id)).toEqual(Array.from({ length: n }, (_, i) => store[i]!.id));
    }
    store.push(crash(3));
    const app = await open("../FakeDaemonApp.tsx", { platform: "android", storage: paired, host: runHost() });
    await app.waitFor("id:crash-sheet", { timeout: 10_000 });
    expect(app.texts().join(" ")).toContain("java.lang.NullPointerException: boom");
  });
});
