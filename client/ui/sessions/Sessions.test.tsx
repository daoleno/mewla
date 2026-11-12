import { describe, expect, open, test } from "nsh/test";
import { answering, framesOf, paired, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";
const session = (id: string, patch: Record<string, unknown> = {}) => ({ id, name: "", status: "done", cwd: "/home/me/mewla", command: "claude --permission-mode bypassPermissions", provider: "claude", updated_at: Date.now(), ...patch });

async function sessionsApp(daemon: Record<string, unknown>) {
  const app = await open(APP, { host: phoneHost({ brain: {}, ...daemon }), storage: paired, platform: "android" });
  await app.host.send("links.open", { url: "mewla://sessions" });
  return app;
}

describe("Sessions", () => {
  test("lists the computer's Sessions by folder, a caption per folder once there are two", async () => {
    const app = await sessionsApp({ worker_sessions: [session("a", { name: "release notes" }), session("b", { cwd: "/home/me/site", status: "running", provider: "codex", command: "codex" }), session("c", { needs_attention: true, status: "blocked" })] });
    await app.waitFor("text:release notes");
    expect(app.count("id:session-row")).toBe(3);
    expect(app.texts()).toContain("mewla");
    expect(app.texts()).toContain("site");
    // The first caption is the mewla folder's: folded, it keeps its count and its Session waiting on you.
    await app.press("id:section >> nth=0");
    await app.waitFor("text:2");
    expect(app.count("id:session-row")).toBe(1);
  }, 20_000);

  test("a tap that spans a Session's update still opens it: the update does not rebuild the row under the finger", async () => {
    const app = await sessionsApp({ worker_sessions: [session("a", { name: "release notes", status: "running" })] });
    await app.waitFor("text:release notes");
    await app.dragAndHold("id:session-row", { x: 0, y: 0 });
    await app.host.send("app.daemonPush", { type: "worker_session_updated", worker_session: session("a", { name: "release notes v2", status: "done" }) });
    // The update has reached the row while the finger is still down.
    await app.waitFor("text:release notes v2");
    await app.drop();
    await app.waitFor("id:session-menu");
  }, 20_000);

  test("one folder has no caption; with none, it offers New session", async () => {
    const one = await sessionsApp({ worker_sessions: [session("a", { name: "only" })] });
    await one.waitFor("text:only");
    expect(one.count("id:section")).toBe(0);
    const none = await sessionsApp({ worker_sessions: [] });
    await none.waitFor("text:No sessions yet");
    // The chosen pet, idle, above it.
    expect(none.shows("id:sessions-empty-cat")).toBe(true);
    expect(none.texts()).toContain("New session");
  }, 20_000);

  test("New session starts the agent in the folder shown and opens it", async () => {
    const app = await sessionsApp({ worker_sessions: [session("a", { name: "first" })] });
    await app.waitFor("text:first");
    await app.press("id:new-session");
    await app.waitFor("id:preset-codex");
    await app.press("id:preset-codex");
    await app.waitFor("id:session-menu");
    const create = framesOf(app.host.posts(), "create_session").at(-1)!;
    expect(create).toMatchObject({ cwd: "/home/me/mewla", command: "codex --dangerously-bypass-approvals-and-sandbox", name: "" });
    const active = framesOf(app.host.posts(), "set_active_worker").map((f) => f.worker_id);
    expect(active.at(-1)).toBe("w101");
  }, 20_000);

  test("the menu's Services lists ports by project; a port's page opens its terminal and shares it after asking", async () => {
    const web = { id: "svc-1", generation: "g1", worker_id: "a", worker_name: "dev server", project: "mewla", cwd: "/home/me/mewla", process: "vite", pid: 9, port: 5173, protocol: "tcp", binds: ["0.0.0.0"], urls: [{ label: "LAN", url: "http://192.0.2.10:5173", address: "192.0.2.10:5173", kind: "lan" }], local_only: false, source: "session" };
    const app = await sessionsApp({
      worker_sessions: [session("a", { name: "dev server" })],
      replies: {
        list_session_services: { type: "session_service_list", services: [web], interfaces: [] },
        service_tunnel: { type: "service_tunnel", service_id: "svc-1", tunnel: { status: "running", url: "https://quiet-cat.trycloudflare.com", generation: "g1" } },
      },
    });
    await app.waitFor("text:dev server");
    app.onDialog((dialog) => (dialog.buttons.includes("Services") ? "Services" : dialog.buttons.includes("Start tunnel") ? "Start tunnel" : null));
    await answering(app, () => app.press("id:sessions-menu"));
    await app.waitFor("id:service-row");
    expect(app.texts()).toContain("5173");
    expect(app.texts()).toContain("1 port");
    await app.press("id:service-row");
    await app.waitFor("id:service-terminal");
    expect(app.texts()).toContain("192.0.2.10:5173");
    await answering(app, () => app.press("id:tunnel-toggle"));
    await app.waitFor("id:tunnel-copy");
    expect(framesOf(app.host.posts(), "service_tunnel").at(-1)).toMatchObject({ service_id: "svc-1", service_generation: "g1", tunnel_action: "start" });
    expect(app.texts()).toContain("Stop public tunnel");
    await app.press("id:service-terminal");
    await app.waitFor("id:session-menu");
    expect(framesOf(app.host.posts(), "set_active_worker").at(-1)?.worker_id).toBe("a");
  }, 20_000);

  test("a switch of computer closes an open sheet first, then switches", async () => {
    const servers = JSON.parse(paired["mewla:v3:servers"]);
    const storage = { ...paired, "mewla:v3:servers": JSON.stringify([...servers, { ...servers[0], id: "s2", name: "Lab", daemonId: "c".repeat(64) }]) };
    const app = await open(APP, { host: phoneHost({ brain: {}, worker_sessions: [session("a", { name: "dev server" })] }), storage, platform: "android" });
    await app.host.send("links.open", { url: "mewla://sessions" });
    await app.waitFor("text:dev server");
    app.onDialog((dialog) => (dialog.buttons.includes("Services") ? "Services" : dialog.buttons.includes("Cancel") ? "Cancel" : null));
    // The menu offers Services once the computer is connected; a slow runner may get there after the list shows.
    for (let tries = 0; !app.shows("id:services-sheet") && tries < 5; tries += 1) {
      await app.press("id:sessions-menu");
      await app.waitFor("id:services-sheet", { timeout: 2_000 }).catch(() => {});
    }
    expect(app.shows("id:services-sheet")).toBe(true);
    await app.host.send("links.open", { url: "mewla://sessions?serverId=s2" });
    await app.waitUntil(() => !app.shows("id:services-sheet"));
    expect(app.storage()["mewla:v1:current_server_id"]).toBe("s1");
    await app.waitUntil(() => app.storage()["mewla:v1:current_server_id"] === "s2");
    expect(app.shows("id:services-sheet")).toBe(false);
  }, 20_000);

  test("a long press selects; Terminate asks, then ends them and they leave the list", async () => {
    const app = await sessionsApp({ worker_sessions: [session("a", { name: "one" }), session("b", { name: "two" })] });
    await app.waitFor("text:one");
    await app.longPress("text:one");
    await app.waitFor("text:1 selected");
    await app.waitFor("text:two", { timeout: 10_000 });
    await app.press("text:two");
    await app.waitFor("text:2 selected");
    app.onDialog(() => "Terminate");
    await answering(app, () => app.press("id:selection-terminate"));
    await app.waitFor("text:No sessions yet");
    expect(framesOf(app.host.posts(), "kill_worker").map((f) => f.worker_id).sort()).toEqual(["a", "b"]);
    expect(app.dialogs()[0]).toMatchObject({ title: "Terminate sessions?" });
  }, 20_000);
  test("Back while selecting ends selecting and stays on Sessions (parity #26)", async () => {
    const app = await sessionsApp({ worker_sessions: [session("a", { name: "one" }), session("b", { name: "two" })] });
    await app.waitFor("text:one");
    await app.longPress("text:one");
    await app.waitFor("text:1 selected");
    await app.back();
    await app.waitUntil(() => !app.shows("text:1 selected"));
    expect(app.count("id:session-row")).toBe(2);
    expect(app.shows("id:selection-terminate")).toBe(false);
  }, 20_000);

  test("a row names itself to a screen reader: title and state (parity #10)", async () => {
    const app = await sessionsApp({ worker_sessions: [session("a", { name: "release notes", status: "running" })] });
    await app.waitFor("text:release notes");
    expect(app.count("label:release notes, Running, No recent output")).toBe(1);
  }, 20_000);
});

describe("a Session", () => {
  const transcript = [
    { id: "u1", seq: 1, kind: "user_message", timestamp: "2026-11-11T07:00:00.000Z", body: "Delete the build folder" },
    { id: "a1", seq: 2, kind: "assistant_message", timestamp: "2026-11-11T07:00:05.000Z", body: "I need your **permission** to run `rm -rf build`." },
  ];
  const blocked = session("w9", {
    name: "cleanup", status: "blocked", process_id: 42, started_at: 1_700_000_000_000,
    prompt: { id: "p-1", kind: "permission", provider: "claude", title: "Run this command?", request: "rm -rf build", options: [{ number: 1, label: "Yes", selected: true }, { number: 2, label: "No" }] },
  });

  test("shows its chat, and the prompt open in its terminal as a card the person can answer", async () => {
    const app = await sessionsApp({ worker_sessions: [blocked], conversations: { w9: transcript } });
    await app.waitFor("text:cleanup");
    await app.waitFor("text:cleanup", { timeout: 10_000 });
    await app.press("text:cleanup");
    await app.waitFor("id:action-prompt");
    expect(app.texts()).toContain("Run this command?");
    expect(app.texts()).toContain("Delete the build folder");
    await app.press("id:prompt-option-2");
    await app.waitFor("text:Running");
    expect(framesOf(app.host.posts(), "answer_prompt").at(-1)).toMatchObject({ worker_id: "w9", prompt_answer: { prompt_id: "p-1", number: 2 } });
    expect(app.shows("id:action-prompt")).toBe(false);
  }, 20_000);

  test("a long chat read back up, left and reopened shows its messages at the row it was read to (FAILURES R4#9)", async () => {
    const long = Array.from({ length: 60 }, (_, i) => ({
      id: `m${i}`, seq: i + 1, kind: i % 2 ? "assistant_message" : "user_message",
      timestamp: new Date(Date.UTC(2026, 10, 11, 7, 0, i)).toISOString(),
      body: `Message ${i}. ` + "A long line of words that wraps over several rows in a phone-sized chat. ".repeat(4),
    }));
    const app = await sessionsApp({ worker_sessions: [session("w1", { name: "longchat", status: "done" })], conversations: { w1: long } });
    await app.waitFor("text:longchat", { timeout: 10_000 });
    await app.press("text:longchat");
    await app.waitFor("id:session-chat");
    await app.waitUntil(() => app.texts().some((t) => t.startsWith("Message 59.")), { timeout: 10_000 });
    if (app.shows("id:jump-latest")) await app.press("id:jump-latest");
    await app.wait(300);
    for (let i = 0; i < 3; i += 1) await app.scroll("id:session-chat", -1500);
    await app.wait(300);
    expect(app.shows("id:jump-latest")).toBe(true);
    await app.back();
    await app.waitFor("text:longchat");
    await app.press("text:longchat");
    await app.waitFor("id:session-chat");
    await app.wait(1000);
    expect(app.texts().filter((t) => t.startsWith("Message")).length).toBeGreaterThan(0);
    expect(app.shows("id:jump-latest")).toBe(true);
  }, 30_000);

  test("the menu switches to the terminal and renames it on this phone", async () => {
    const app = await sessionsApp({ worker_sessions: [session("w1", { name: "notes", status: "done" })], conversations: { w1: transcript } });
    await app.waitFor("text:notes", { timeout: 10_000 });
    await app.press("text:notes");
    await app.waitFor("id:session-chat");
    app.onDialog((dialog) => (dialog.buttons.includes("Rename") ? "Rename" : null));
    await answering(app, () => app.press("id:session-menu"));
    await app.waitFor("id:rename-input");
    await app.type("id:rename-input", "Scratchpad");
    await app.press("id:rename-save");
    await app.waitFor("text:Scratchpad");
    await app.back();
    await app.waitFor("text:Scratchpad");
  }, 20_000);

  test("a DSH Session's permission request is reviewed and allowed once", async () => {
    const dshSession = session("w5", { name: "dsh run", status: "running", provider: "dsh", command: "dsh", process_id: 77, started_at: 1_700_000_000_000, capabilities: { structured_events: true } });
    const app = await sessionsApp({
      worker_sessions: [dshSession],
      conversations: { w5: { source: "dsh_session_jsonl", events: [] } },
      dsh: { epoch: "e1", items: [{ rpcId: "r1", payload: { type: "approval/requested", sessionId: "s", toolName: "Bash", reason: "Run `make deploy`" } }] },
    });
    await app.waitFor("text:dsh run", { timeout: 10_000 });
    await app.press("text:dsh run");
    await app.waitFor("id:dsh-banner");
    expect(app.texts()).toContain("Permission requested");
    await app.press("id:dsh-review");
    await app.waitFor("id:dsh-allow");
    await app.press("id:dsh-allow");
    await app.waitFor("id:dsh-banner", { state: "gone" });
    const answers = framesOf(app.host.posts(), "dsh_interaction").filter((f) => f.dsh_answer);
    expect(answers.at(-1)).toMatchObject({ worker_id: "w5", process_id: 77, dsh_answer: { epoch: "e1", rpcId: "r1", outcome: "allowed-once" } });
  }, 20_000);

  test("a Codex Session's /status opens its status, and its model chip opens Model & Effect", async () => {
    const codex = session("w3", { name: "codex run", status: "done", provider: "codex", command: "codex", capabilities: { structured_events: true, model_profile_managed: true, model_profile_active_switch: true } });
    const runtime = { session_id: "w3", client: "codex", connection_id: "c1", connection_name: "OpenAI", model_id: "gpt-6", reasoning_efforts: ["low", "high"], credential_ready: true, hot_switchable: true };
    const app = await sessionsApp({
      worker_sessions: [codex],
      conversations: { w3: [] },
      replies: {
        codex_slash_commands: { type: "codex_slash_commands", commands: [{ value: "/status", name: "status", description: "Show status", category: "session", chat_supported: true }] },
        codex_status: { type: "codex_status", status: { thread: { id: "th-1", model: "gpt-6", effort: "high", cwd: "/repo" }, limits: [{ id: "codex", windows: [{ used_percent: 25, window_minutes: 300 }] }] } },
        get_thread_runtime: { type: "thread_runtime", runtime },
        list_providers: { type: "providers", revision: 1, connections: [{ id: "c1", name: "OpenAI", clients: ["codex"], credential_ready: true, advanced: false }], defaults: { codex: { connection_id: "c1" } }, presets: [], models: { c1: [{ id: "gpt-6", available: true, source: "catalog" }] } },
      },
    });
    await app.waitFor("text:codex run", { timeout: 10_000 });
    await app.press("text:codex run");
    await app.waitFor("id:model-chip");
    expect(app.texts()).toContain("gpt-6");
    await app.type("id:composer", "/sta");
    await app.waitFor("id:slash-status");
    await app.type("id:composer", "tus");
    await app.press("id:send");
    await app.waitFor("id:status-meter");
    expect(framesOf(app.host.posts(), "send_input")).toHaveLength(0);
    expect(framesOf(app.host.posts(), "codex_status").at(-1)).toMatchObject({ target_id: "w3" });
    await app.back();
    await app.waitFor("id:status-meter", { state: "gone" });
    await app.press("id:model-chip");
    await app.waitFor("id:model-row-gpt-6");
  }, 20_000);

  test("a Calendar run's Session is titled by its run, badged, says where it came from, and opens its Work", async () => {
    const startedAt = Date.parse("2026-07-17T03:00:30Z");
    const work = {
      id: "wf-1", project: "calendar", path: "calendar/wf-1.md", title: "Daily digest", mtime: "2026-07-17T03:01:00Z", body: "# Daily digest\n\nRead the three papers.\n",
      frontmatter: { id: "wf-1", kind: "calendar_action", created: "2026-07-17T03:00:00Z", worker_session: "w4", extra: { calendar_item_id: "c1", calendar_run_id: "r1" } },
    };
    const calendarItem = { id: "c1", title: "Paper digest", kind: "scheduled_action", status: "scheduled", timezone: "UTC", due_at: "2026-07-18T03:00:00Z", runs: [{ id: "r1", status: "running", scheduled_for: "2026-07-17T03:00:00Z", work_id: "wf-1" }] };
    const app = await sessionsApp({ worker_sessions: [session("w4", { started_at: startedAt })], conversations: { w4: [] }, work_items: [work], calendar_items: [calendarItem] });
    // The run's label comes before the Work's title, as in v0.3.1.
    await app.waitFor("text:Paper digest · Jul 17");
    expect(app.count("id:row-calendar")).toBe(1);
    await app.press("id:session-row");
    await app.waitFor("id:session-calendar");
    app.onDialog((dialog) => (dialog.buttons.includes("Open Brain") ? "Open Brain" : null));
    await answering(app, () => app.press("id:session-menu"));
    await app.waitFor("id:work-file-page");
    expect(app.texts()).toContain("Read the three papers.");
  }, 20_000);

  test("a chat it cannot read says why and opens the terminal; a ready one says where it runs", async () => {
    const app = await sessionsApp({
      worker_sessions: [session("w6", { name: "broken" }), session("w8", { name: "fresh", cwd: "/home/me/code/mewla" })],
      conversations: { w6: { available: false, reason: "transcript_malformed", events: [] }, w8: [] },
    });
    await app.waitFor("text:broken", { timeout: 10_000 });
    await app.press("text:broken");
    await app.waitFor("id:chat-open-terminal");
    expect(app.texts()).toContain("Chat could not read this session transcript. Open the terminal view.");
    await app.press("id:chat-open-terminal");
    await app.waitFor("id:chat-open-terminal", { state: "gone" });
    await app.back();
    await app.waitFor("text:fresh", { timeout: 10_000 });
    await app.press("text:fresh");
    await app.waitFor("text:Ready");
    expect(app.texts()).toContain("…/code/mewla");
  }, 20_000);

  test("a mewla://sessions/<id> link (a notification, Work, Calendar) opens that Session", async () => {
    const app = await sessionsApp({ worker_sessions: [session("w7", { name: "linked" })], conversations: { w7: [] } });
    await app.waitFor("text:linked");
    await app.host.send("links.open", { url: "mewla://brain" });
    await app.host.send("links.open", { url: "mewla://sessions/w7" });
    await app.waitFor("id:session-menu");
    expect(framesOf(app.host.posts(), "set_active_worker").at(-1)?.worker_id).toBe("w7");
  }, 20_000);

  test("a Session in a repository shows its changes on a chip that opens Git review", async () => {
    const files = [{ path: "app.ts", staged: false, unstaged: true, untracked: false, additions: 3, deletions: 1 }, { path: "notes.md", staged: false, unstaged: false, untracked: true, additions: 0, deletions: 0 }];
    const status = { available: true, repo_root: "/home/me/mewla", repo_name: "mewla", branch: "main", has_commits: true, clean: false, file_count: 2, staged_file_count: 0, unstaged_file_count: 1, untracked_file_count: 1, additions: 3, deletions: 1, files };
    const app = await sessionsApp({ worker_sessions: [session("w5", { name: "repo work" })], conversations: { w5: [] }, replies: { git_diff_status: { type: "git_diff_status", status } } });
    await app.waitFor("text:repo work", { timeout: 10_000 });
    await app.press("text:repo work");
    await app.waitFor("id:session-git");
    expect(app.texts()).toContain("2 files  +3  -1");
    expect(framesOf(app.host.posts(), "git_diff_status").at(-1)).toMatchObject({ target_id: "w5", cwd: "/home/me/mewla" });
    await app.press("id:session-git");
    await app.waitFor("id:git-page");
  }, 20_000);

  test("Attach File uploads a picked file and the message carries where the computer keeps it", async () => {
    const app = await open(APP, {
      host: phoneHost({ brain: {}, worker_sessions: [session("w6", { name: "attach" })], conversations: { w6: [] } }, {
        "files.pick": () => [{ uri: "content://picker/plan.pdf", name: "plan.pdf", mimeType: "application/pdf", size: 2048 }],
        "files.upload": () => Promise.resolve().then(() => ({ status: 200, body: JSON.stringify({ path: "/state/uploads/plan.pdf", name: "plan.pdf" }) })),
        "files.cancel": () => null,
      }),
      storage: paired,
      platform: "android",
    });
    await app.host.send("links.open", { url: "mewla://sessions" });
    await app.waitFor("text:attach", { timeout: 10_000 });
    await app.press("text:attach");
    await app.waitFor("id:composer");
    await app.press("id:composer-plus");
    await app.press("id:attach-file");
    await app.waitFor("text:Ready");
    await app.type("id:composer", "Read this");
    // A slow runner draws the typed text a frame later: Send is enabled once it has.
    await app.waitUntil(() => app.enabled("id:send"));
    await app.press("id:send");
    await app.waitUntil(() => framesOf(app.host.posts(), "send_input").length === 1);
    expect(framesOf(app.host.posts(), "send_input")[0]).toMatchObject({ worker_id: "w6", text: "Read this", attachments: [{ name: "plan.pdf", path: "/state/uploads/plan.pdf", content_type: "application/pdf" }] });
    await app.waitFor("text:Ready", { state: "gone" });
  }, 20_000);

  test("a reply's image on the computer is read through the Session and shown", async () => {
    const reply = [{ id: "a1", seq: 1, kind: "assistant_message", timestamp: new Date().toISOString(), body: "Here is the chart:\n\n![chart](out/chart.png)" }];
    const png = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";
    const app = await sessionsApp({ worker_sessions: [session("w8", { name: "charts" })], conversations: { w8: reply }, replies: { codex_asset: { type: "codex_asset", path: "/home/me/mewla/out/chart.png", content_type: "image/png", data_url: png } } });
    await app.waitFor("text:charts", { timeout: 10_000 });
    await app.press("text:charts");
    await app.waitUntil(() => framesOf(app.host.posts(), "codex_asset").length === 1);
    expect(framesOf(app.host.posts(), "codex_asset")[0]).toMatchObject({ path: "out/chart.png", cwd: "/home/me/mewla" });
    expect(app.texts()).toContain("Today");
  }, 20_000);
});
