import { describe, expect, open, test } from "nsh/test";
import { paired, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";
const host_worker = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: 1_762_840_800_000, process_id: 4242 };
const brain = { chat_thread_id: "t-now", host_worker };
const session = { id: "w1", name: "notes", status: "running", cwd: "/home/me/mewla", command: "claude", provider: "claude", started_at: 1_762_840_800_000, process_id: 77, updated_at: Date.now() };
const transcript = [
  { id: "u1", seq: 1, kind: "user_message", timestamp: "2026-11-11T07:00:00.000Z", body: "Write the plan" },
  { id: "a1", seq: 2, kind: "assistant_message", timestamp: "2026-11-11T07:00:05.000Z", body: "Wrote [the plan](/home/me/mewla/plan.md)." },
];
const daemon = { brain, worker_sessions: [session], conversations: { w1: transcript } };

async function app() {
  const opened = await open(APP, { platform: "android", storage: paired, host: phoneHost(daemon) });
  await opened.waitFor("id:composer", { timeout: 10_000 });
  await opened.waitFor("text:What should Brain do?", { timeout: 10_000 });
  return opened;
}

describe("a draft survives a page pushed over its chat (SPEC §3.8)", () => {
  test("home: typed, a Session opened from the tray, Back: the text is still there", async () => {
    const a = await app();
    await a.type("id:composer", "half a thought");
    await a.waitUntil(() => a.value("id:composer") === "half a thought");
    await a.press("id:state-control");
    await a.waitFor("id:tray-row");
    await a.press("id:tray-row");
    await a.waitFor("text:Write the plan", { timeout: 10_000 });
    // Trial: three pages: Back returns to the running page the Session was opened from, and Back again to Brain.
    await a.back();
    await a.waitFor("id:home-page-running");
    await a.back();
    await a.waitFor("id:home-header");
    await a.waitUntil(() => a.value("id:composer") === "half a thought", { timeout: 3_000 });
  }, 25_000);

  test("home: typed, a page opened by a link, Back: the text is still there", async () => {
    const a = await app();
    await a.type("id:composer", "half a thought");
    await a.waitUntil(() => a.value("id:composer") === "half a thought");
    await a.host.send("links.open", { url: "mewla://sessions/w1" });
    await a.waitFor("text:Write the plan", { timeout: 10_000 });
    await a.back();
    await a.waitFor("id:home-header");
    await a.waitUntil(() => a.value("id:composer") === "half a thought", { timeout: 3_000 });
  }, 25_000);

  test("a Session: typed, a file opened from its chat, Back: the text is still there", async () => {
    const a = await app();
    await a.host.send("links.open", { url: "mewla://sessions/w1" });
    await a.waitFor("text:the plan", { timeout: 10_000 });
    await a.type("id:composer", "and the tests");
    await a.waitUntil(() => a.value("id:composer") === "and the tests");
    await a.press("text:the plan");
    await a.waitFor("id:file-preview", { timeout: 10_000 });
    await a.back();
    await a.waitFor("text:Write the plan");
    await a.waitUntil(() => a.value("id:composer") === "and the tests", { timeout: 3_000 });
  }, 25_000);
});
