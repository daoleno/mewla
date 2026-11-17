import { describe, expect, open, test } from "nsh/test";
import { expoTokenService, paired, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";
const json = (body: unknown) => ({ status: 200, headers: { "content-type": "application/json" }, body: JSON.stringify(body) });
const DAEMON_UPDATE = "http://127.0.0.1:1/daemon-update";

/** The app on Desk, whose Mewla reports `computer`; `post` answers the row's tap. */
function setup(computer: unknown, post: Record<string, unknown> = {}) {
  const h = phoneHost({ brain: { chat_thread_id: "t-now", host_worker: { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-11T07:00:00.000Z" } } });
  return {
    platform: "android" as const,
    storage: { ...paired },
    network: { ...expoTokenService, [`GET ${DAEMON_UPDATE}`]: json(computer), ...Object.fromEntries(Object.entries(post).map(([key, body]) => [key, json(body)])) },
    host: h,
  };
}

describe("an update that waits says so in Brain's Chat", () => {
  test("a busy computer from before bounded updates: the row names the version and Update now installs it", async () => {
    const app = await open(APP, setup({ version: "0.4.12", supported: true, latest: "0.4.14" }, { [`POST ${DAEMON_UPDATE}`]: { version: "0.4.12", supported: true, latest: "0.4.14", updating: true } }));
    await app.waitFor("id:chat-update-row", { timeout: 10_000 });
    expect(app.shows("text:Update to 0.4.14 waiting for Workers to finish")).toBe(true);
    await app.press("id:chat-update-action");
    await app.waitUntil(() => !app.shows("id:chat-update-row"));
    expect(app.requests().some((r) => r.method === "POST" && r.url === DAEMON_UPDATE)).toBe(true);
  }, 20_000);

  test("a staged update names what it waits for", async () => {
    const app = await open(APP, setup({ version: "0.4.15", supported: true, latest: "0.4.16", waiting_for: "a model reply", install_by: "2026-11-18T10:00:00Z" }));
    await app.waitFor("text:Update to 0.4.16 waiting for a model reply", { timeout: 10_000 });
  }, 20_000);

  test("old terminals: Restart terminals to finish updating, with what it ends; Restart restarts them", async () => {
    const app = await open(APP, setup(
      { version: "0.4.15", supported: true, host_version: "0.4.6", terminals_restart: true, live_terminals: 2 },
      { [`POST ${DAEMON_UPDATE}/terminals`]: { version: "0.4.15", supported: true, host_version: "0.4.6", terminals_restart: true } },
    ));
    await app.waitFor("text:Restart terminals to finish updating", { timeout: 10_000 });
    expect(app.shows("text:Ends 2 open terminals")).toBe(true);
    await app.press("id:chat-update-action");
    await app.waitUntil(() => !app.shows("id:chat-update-row"));
    expect(app.requests().some((r) => r.method === "POST" && r.url === `${DAEMON_UPDATE}/terminals`)).toBe(true);
  }, 20_000);

  test("an update that installs by itself shows nothing", async () => {
    const app = await open(APP, setup({ version: "0.4.15", supported: true, latest: "0.4.16" }));
    await app.waitFor("id:home-cat", { timeout: 10_000 });
    await app.wait(800);
    expect(app.shows("id:chat-update-row")).toBe(false);
  }, 20_000);
});
