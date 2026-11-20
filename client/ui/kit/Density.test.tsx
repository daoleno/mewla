import { describe, expect, open, test } from "nsh/test";
import { paired, phoneHost } from "../testing/fixtures";

// The Mac density pass (RESULT-DESIGN-PAIRS 11-21) sizes Mewla for Mac only: the phones keep the phone's type.
const APP = "../FakeDaemonApp.tsx";
const host_worker = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: 1_762_840_800_000, process_id: 4242 };
const brain = { chat_thread_id: "t-now", host_worker };
const events = [
  { id: "u1", seq: 1, timestamp: "2026-11-12T07:00:01.000Z", kind: "user_message", role: "user", body: "Is the build green?" },
  { id: "a1", seq: 2, timestamp: "2026-11-12T07:00:02.000Z", kind: "assistant_message", role: "assistant", body: "The build is green on main." },
];
const pairing = { expires_at: "2099-01-01T00:00:00Z", daemon_id: "a".repeat(64), daemon_public_key: "b".repeat(64), local_link: "mewla://settings?p=local" };

async function thread(platform: "ios" | "android" | "macos") {
  const host = phoneHost({ brain, conversations: { "brain-host": events } }, platform === "macos" ? { "app.pairing": () => pairing } : {});
  const app = await open(APP, platform === "macos" ? { platform, width: 1280, height: 800, storage: paired, host } : { platform, storage: paired, host });
  await app.waitFor("text:The build is green on main.", { timeout: 10_000 });
  return app;
}

describe("density", () => {
  for (const platform of ["ios", "android"] as const) {
    test(`${platform} keeps the phone's thread type`, async () => {
      const app = await thread(platform);
      expect(app.spanStyle("text:The build is green on main.").fontSize).toBe(17);
      expect(app.spanStyle("text:Is the build green?").fontSize).toBe(17);
    }, 20_000);
  }

  test("Mewla for Mac's window reads 14.5", async () => {
    const app = await thread("macos");
    // The left pane's Brain row shows the same line first; the thread's is the second.
    expect(app.spanStyle("text:The build is green on main. >> nth=1").fontSize).toBe(14.5);
    expect(app.spanStyle("text:Is the build green?").fontSize).toBe(14.5);
    // The left pane's rows: 13, not the phone's tray rows.
    expect(app.spanStyle("text:All Sessions").fontSize).toBe(13);
  }, 20_000);
});
