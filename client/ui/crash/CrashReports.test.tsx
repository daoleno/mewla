import { describe, expect, open, test } from "nsh/test";

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
      "app.legacyStorage": () => null,
      "app.legacyStorageCopied": () => undefined,
      "app.crashReports": () => reports,
      "app.clearCrashReports": () => null,
      "scan.permission": () => "denied",
    },
  };
}

describe("a crash last time", () => {
  test("shows on the next start with the trace, Copy and Dismiss; Dismiss clears it in the host", async () => {
    const app = await open("../App.tsx", { host: host([report]), platform: "android" });
    await app.waitFor("id:crash-sheet");
    expect(app.texts()).toContain("Mewla closed unexpectedly");
    expect(app.texts().join(" ")).toContain("java.lang.NullPointerException: boom");
    await app.press("id:crash-copy");
    expect(app.clipboard()).toContain("QrScanner.kt:45");
    await app.press("id:crash-dismiss");
    const clear = app.host.calls().find((call) => call.name === "app.clearCrashReports");
    expect(clear?.data).toEqual({ ids: [report.id] });
    await app.waitUntil(() => !app.shows("id:crash-sheet"));
  });

  test("nothing kept, nothing shown", async () => {
    const app = await open("../App.tsx", { host: host([]), platform: "android" });
    await app.waitFor("id:set-up");
    expect(app.shows("id:crash-sheet")).toBe(false);
  });
});
