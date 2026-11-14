import { describe, expect, test } from "bun:test";
import { crashDiagnostic, crashesToShow, crashSummary, crashTitle, endedTheRun, readCrashReports, type CrashReport } from "./crash";

const javaCrash: CrashReport = {
  id: "1762818467989-java-12.txt",
  kind: "java",
  at: 1762818467989,
  text: [
    "Mewla 0.3.0 (30000, release) com.daoleno.mewla.preview",
    "OnePlus PYR110, Android 16 (API 36), arm64-v8a",
    "java at 2026-11-10T23:47:47Z",
    "",
    "Uncaught exception on thread \"main\"",
    "",
    "java.lang.NullPointerException: Attempt to read from field 'r71 q21.a' on a null object reference",
    "\tat com.daoleno.mewla.QrScanner.start$lambda$9(r8-map-id-87e91da5:88)",
    "\tat android.os.Handler.handleCallback(Handler.java:959)",
  ].join("\n"),
};

describe("crash reports", () => {
  test("keeps only well-formed reports, newest first", () => {
    const reports = readCrashReports([
      { id: "a", kind: "js", at: 1, text: "x" },
      { id: "b", kind: "weird", at: 2, text: "y" },
      { id: 3, kind: "java", at: 3, text: "z" },
      null,
    ]);
    expect(reports.map((r) => [r.id, r.kind])).toEqual([["b", "native"], ["a", "js"]]);
    expect(readCrashReports("nope")).toEqual([]);
  });

  test("a crash closed the app; JS errors alone only broke a screen", () => {
    expect(crashTitle([javaCrash])).toBe("Mewla closed unexpectedly");
    expect(crashTitle([{ ...javaCrash, kind: "js" }])).toBe("Mewla hit an error");
  });

  test("the summary names the version, the error and the first frame, within the daemon's 300 characters", () => {
    const summary = crashSummary(javaCrash);
    expect(summary).toStartWith("java 0.3.0 (30000, release) java.lang.NullPointerException");
    expect(summary).toContain("@ at com.daoleno.mewla.QrScanner.start$lambda$9");
    expect(crashSummary({ ...javaCrash, text: javaCrash.text + "x".repeat(400).replace(/^/, "\n") }).length).toBeLessThanOrEqual(300);
    const long = { ...javaCrash, text: `Mewla 1 (1, release) x\n\n${"E".repeat(500)}` };
    expect(crashSummary(long).length).toBe(300);
  });

  test("goes to the computer as the daemon's client_diagnostic", () => {
    expect(crashDiagnostic(javaCrash)).toEqual({ type: "client_diagnostic", name: "app_crash", request_id: javaCrash.id, body: crashSummary(javaCrash) });
  });

  // The owner's eight reports of 11-14 (v0.4.2), two minutes apart at most: every one a 2 s cut-off.
  const cutOff = (i: number): CrashReport => ({
    id: `17946716${i}-js-${i}.txt`, kind: "js", at: Date.UTC(2026, 10, 14, 15, 53, 55) + i * 15_000,
    text: "Mewla 0.4.2 (40299, release) com.daoleno.mewla\nOnePlus PYR110, Android 16 (API 36), arm64-v8a\njs at 2026-11-14T15:55:41Z\n\nthe script was interrupted: a call into JS exceeded its budget of 2000 ms\nError: interrupted\n    at filter (native)",
  });
  const outOfMemory = (at: number): CrashReport => ({ id: `${at}-js-oom.txt`, kind: "js", at, text: "Mewla 0.4.2 (40299, release) x\n\nthe script ran out of memory (the JS heap is limited to 256 MB) and was stopped." });
  const anr: CrashReport = { id: "1-anr.txt", kind: "anr", at: 1, text: "The app stopped answering for 34 s (its JS thread) and was reopened." };

  test("what the app survived is never shown: cut-offs, other JS errors, an ANR the system reopened", () => {
    const eight = Array.from({ length: 8 }, (_, i) => cutOff(i));
    expect(crashesToShow(eight)).toEqual([]);
    expect(crashesToShow([...eight, anr, { ...javaCrash, kind: "js", text: "TypeError: x is undefined" }])).toEqual([]);
    expect(eight.some(endedTheRun)).toBe(false);
    expect(endedTheRun(anr)).toBe(false);
  });

  test("one crash, or crashes far apart, are not shown either: the computer has them", () => {
    expect(crashesToShow([javaCrash])).toEqual([]);
    const hour = 60 * 60_000;
    expect(crashesToShow([javaCrash, { ...javaCrash, id: "b", at: javaCrash.at + hour }, outOfMemory(javaCrash.at + 2 * hour)])).toEqual([]);
  });

  test("three runs ended within ten minutes are a crash loop: the sheet shows them, newest first", () => {
    const loop = [javaCrash, { ...javaCrash, id: "b", kind: "native" as const, at: javaCrash.at + 60_000 }, outOfMemory(javaCrash.at + 9 * 60_000)];
    expect(crashesToShow([...loop, cutOff(1)]).map((r) => r.id)).toEqual([loop[2]!.id, "b", javaCrash.id]);
  });
});
