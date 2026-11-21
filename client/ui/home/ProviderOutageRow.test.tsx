import { describe, expect, open, test } from "nsh/test";
import { expoTokenService, paired, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";
const HOST = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-21T07:00:00.000Z" };

function setup(brain: Record<string, unknown>) {
  return { platform: "android" as const, storage: { ...paired }, network: { ...expoTokenService }, host: phoneHost({ brain }) };
}

describe("a model provider refusing requests says so in Brain's Chat, in one line", () => {
  test("the line shows while the computer lists an outage and clears by itself when it ends", async () => {
    const since = new Date(Date.now() - 40 * 60_000);
    const clock = `${String(since.getHours()).padStart(2, "0")}:${String(since.getMinutes()).padStart(2, "0")}`;
    const outage = { provider: "claude", since: since.toISOString(), kind: "rate limit", cause: "rate_limit, HTTP 429: Request rejected (429)" };
    const app = await open(APP, setup({ chat_thread_id: "t-now", host_worker: HOST, provider_outages: [outage] }));
    await app.waitFor("id:chat-provider-outage", { timeout: 10_000 });
    expect(app.shows(`text:Claude hasn't answered since ${clock} (rate limit). Work is held, not lost.`)).toBe(true);
    app.host.send("app.daemonPush", { type: "brain_snapshot", brain: { chat_thread_id: "t-now", host_worker: HOST } });
    await app.waitUntil(() => !app.shows("id:chat-provider-outage"));
  }, 20_000);
});
