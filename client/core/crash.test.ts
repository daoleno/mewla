import { describe, expect, test } from "bun:test";
import { crashDiagnostic, crashSummary, crashTitle, readCrashReports, type CrashReport } from "./crash";

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
});
