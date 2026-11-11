import { describe, expect, open, test } from "nsh/test";
import { paired, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";
const brain = {
  chat_thread_id: "t-now",
  host_worker: { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-11T07:00:00.000Z" },
};

describe("a Brain notification's thread", () => {
  test("a past thread opens read-only, and Current chat goes back", async () => {
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain }) });
    await app.waitFor("id:composer", { timeout: 10_000 });
    await app.host.send("links.open", { url: "mewla://brain?thread=t-old&serverId=s1" });
    await app.waitFor("id:brain-past-thread");
    expect(app.shows("id:composer")).toBe(false);
    await app.press("id:brain-current-thread");
    await app.waitFor("id:composer");
    expect(app.shows("id:brain-past-thread")).toBe(false);
  }, 20_000);

  test("the current thread opens as Brain's live chat", async () => {
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain }) });
    await app.waitFor("id:composer", { timeout: 10_000 });
    await app.host.send("links.open", { url: "mewla://brain?thread=t-now" });
    await app.waitFor("id:composer");
    expect(app.shows("id:brain-past-thread")).toBe(false);
  }, 20_000);
});
