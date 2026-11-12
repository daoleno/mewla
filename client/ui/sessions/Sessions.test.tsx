import { describe, expect, open, test } from "nsh/test";
import { answering, framesOf, paired, phoneHost, pressUntil, pressWhenEnabled, typeUntil } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";
const WEB = {
  "app.origin": () => "http://127.0.0.1:1",
  "links.initial": () => null,
  "web.initialPath": () => ({ path: "/", state: null }),
  "web.setLocation": () => null,
};
const session = (id: string, patch: Record<string, unknown> = {}) => ({ id, name: "", status: "done", cwd: "/home/me/mewla", command: "claude --permission-mode bypassPermissions", provider: "claude", updated_at: Date.now(), ...patch });

/**
 * `drawnHeader`: the phone-width web app, whose page header is the app's own laid-out row. NSH 0.5.0's
 * headless Android model lays a native header's items at the screen's origin, so a Session's title covers
 * ⋯ and the Git chip there (nsh-upstream-1111/DRAFT-headless-header-item-order.md); a test that presses a
 * header action runs here until NSH places them. Back to Android when it does.
 */
async function sessionsApp(daemon: Record<string, unknown>, options: { drawnHeader?: boolean } = {}) {
  const host = phoneHost({ brain: {}, ...daemon }, options.drawnHeader ? WEB : {});
  const app = await open(APP, options.drawnHeader
    ? { host, storage: paired, platform: "web", width: 390, height: 844 }
    : { host, storage: paired, platform: "android" });
  // The inbox link opens the tray; its last row is All Sessions, by folder.
  await app.host.send("links.open", { url: "mewla://sessions" });
  await app.waitFor("id:tray-all-sessions");
  await pressUntil(app, "id:tray-all-sessions", "id:sessions");
  return app;
}

describe("Sessions", () => {
  test("lists the computer's Sessions by folder, each folder's caption with its count; with two, a caption folds", async () => {
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

  test("one folder has its caption and count, and does not fold; with none, it offers New session", async () => {
    const one = await sessionsApp({ worker_sessions: [session("a", { name: "only" }), session("b", { name: "other" })] });
    await one.waitFor("text:only");
    expect(one.count("id:section")).toBe(1);
    expect(one.texts()).toContain("mewla");
    expect(one.texts()).toContain("2");
    expect(one.count("id:session-row")).toBe(2);
    const none = await sessionsApp({ worker_sessions: [] });
    await none.waitFor("text:No sessions yet");
    // The chosen pet, idle, above it.
    expect(none.shows("id:sessions-empty-cat")).toBe(true);
    expect(none.texts()).toContain("New session");
  }, 20_000);

  test("New session (the tray's +): a tile chooses the agent, the last folder is chosen, Start starts it and opens it", async () => {
    const app = await open(APP, { host: phoneHost({ brain: {}, worker_sessions: [session("a", { name: "first" })] }), storage: paired, platform: "android" });
    await app.host.send("links.open", { url: "mewla://sessions" });
    await app.waitFor("text:first");
    await app.press("id:tray-new");
    await app.waitFor("id:preset-codex");
    // SPEC §11 New session sheet: Agent, Folder (recent projects by name, the path under it), Start; no raw path field.
    for (const text of ["Agent", "Folder", "mewla", "/home/me/mewla", "Other folder…", "Start"]) expect(app.texts()).toContain(text);
    await app.press("id:preset-codex");
    await app.wait(100);
    expect(framesOf(app.host.posts(), "create_session")).toHaveLength(0);
    await app.press("id:new-launch");
    await app.waitFor("id:session-menu");
    const create = framesOf(app.host.posts(), "create_session").at(-1)!;
    expect(create).toMatchObject({ cwd: "/home/me/mewla", command: "codex --dangerously-bypass-approvals-and-sandbox", name: "" });
    const active = framesOf(app.host.posts(), "set_active_worker").map((f) => f.worker_id);
    expect(active.at(-1)).toBe("w101");
  }, 20_000);

  test("New session's custom command: a Soft group, no uppercase labels; Start launches the typed command", async () => {
    const app = await open(APP, { host: phoneHost({ brain: {}, worker_sessions: [session("a", { name: "first" })] }), storage: paired, platform: "android" });
    await app.host.send("links.open", { url: "mewla://sessions" });
    await app.waitFor("text:first");
    await app.press("id:tray-new");
    await app.waitFor("id:preset-codex");
    expect(app.texts()).toContain("Folder");
    expect(app.texts().some((text) => text === "WORKING DIRECTORY" || text === "Advanced")).toBe(false);
    await app.press("id:new-advanced");
    await app.waitFor("id:new-command");
    await app.type("id:new-command", "htop");
    await app.press("id:new-launch");
    await app.waitFor("id:session-menu");
    expect(framesOf(app.host.posts(), "create_session").at(-1)).toMatchObject({ command: "htop" });
  }, 20_000);

  test("the tray menu's Services lists ports by project; a port's page opens its terminal and shares it after asking", async () => {
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
    await app.host.send("links.open", { url: "mewla://sessions" });
    await app.waitFor("id:tray-menu");
    await answering(app, () => app.press("id:tray-menu"));
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
      await app.press("id:tray-menu");
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
    expect(app.dialogs()[0]).toMatchObject({ title: "Terminate 2 sessions?" });
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
    expect(app.count("label:release notes, Running, Claude Code · mewla")).toBe(1);
  }, 20_000);

  test("a row says what its Session does in words, never its terminal; running, a live dot and how long", async () => {
    const pasted = '"$MEWLA_WORKER_PROGRESS_CMD" worker progress --turn-id t1 --status running';
    const footer = "⏸ manual mode on · ? for shortcuts";
    const app = await sessionsApp({ worker_sessions: [
      session("a", { name: "pasted", status: "running", delegated: true, started_at: Date.now() - 12 * 60_000, summary: pasted, last_output_lines: [pasted] }),
      session("b", { name: "quiet one", status: "unknown", summary: footer, last_output_lines: [footer] }),
      session("c", { name: "reporter", status: "running", phase: "working", summary: "Rerunning the suite", started_at: Date.now() - 4 * 60_000 }),
    ] });
    await app.waitFor("text:reporter");
    // Nothing reported: the agent and project, never the bare "Working" or "Idle".
    expect(app.texts().filter((t) => t === "Claude Code · mewla").length).toBe(2);
    expect(app.texts()).not.toContain("Working");
    expect(app.texts()).toContain("Rerunning the suite");
    expect(app.texts()).toContain("12m");
    expect(app.texts()).toContain("4m");
    expect(app.count("id:row-live")).toBe(2);
    expect(app.texts()).not.toContain("live");
    expect(app.texts().some((t) => t.includes("MEWLA_WORKER_PROGRESS") || t.includes("manual mode"))).toBe(false);
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

  test("shows its chat, and the prompt open in its terminal as a request card in its thread the person can answer", async () => {
    const app = await sessionsApp({ worker_sessions: [blocked], conversations: { w9: transcript } });
    await app.waitFor("text:cleanup");
    await app.waitFor("text:cleanup", { timeout: 10_000 });
    await app.press("text:cleanup");
    await app.waitFor("id:request-card");
    // The terminal's permission menu: what it runs, then No and Yes (the default).
    expect(app.texts()).toContain("Approve");
    expect(app.texts()).toContain("rm -rf build");
    expect(app.texts()).toContain("Delete the build folder");
    await app.wait(500);
    await app.press("id:prompt-option-2");
    await app.waitFor("text:mewla · Running");
    expect(framesOf(app.host.posts(), "answer_prompt").at(-1)).toMatchObject({ worker_id: "w9", prompt_answer: { prompt_id: "p-1", number: 2 } });
    await app.waitFor("id:request-sealed");
    expect(app.shows("id:request-card")).toBe(false);
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
    const app = await sessionsApp({ worker_sessions: [session("w1", { name: "notes", status: "done" })], conversations: { w1: transcript } }, { drawnHeader: true });
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

  test("a Codex Session's /status opens its status, and its model chip opens Model & Effort", async () => {
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

  test("a Calendar run's Session is titled by its run, says where it came from, and opens its Work", async () => {
    const startedAt = Date.parse("2026-07-17T03:00:30Z");
    const work = {
      id: "wf-1", project: "calendar", path: "calendar/wf-1.md", title: "Daily digest", mtime: "2026-07-17T03:01:00Z", body: "# Daily digest\n\nRead the three papers.\n",
      frontmatter: { id: "wf-1", kind: "calendar_action", created: "2026-07-17T03:00:00Z", worker_session: "w4", extra: { calendar_item_id: "c1", calendar_run_id: "r1" } },
    };
    const calendarItem = { id: "c1", title: "Paper digest", kind: "scheduled_action", status: "scheduled", timezone: "UTC", due_at: "2026-07-18T03:00:00Z", runs: [{ id: "r1", status: "running", scheduled_for: "2026-07-17T03:00:00Z", work_id: "wf-1" }] };
    const app = await sessionsApp({ worker_sessions: [session("w4", { started_at: startedAt })], conversations: { w4: [] }, work_items: [work], calendar_items: [calendarItem] }, { drawnHeader: true });
    // The run's label comes before the Work's title, as in v0.3.1.
    await app.waitFor("text:Paper digest · Jul 17");
    // Its tile is its agent's logo (no badge): the run's title and the screen reader say it came from Calendar.
    expect(app.count("id:session-row")).toBe(1);
    await app.press("id:session-row");
    await app.waitFor("id:session-calendar");
    app.onDialog((dialog) => (dialog.buttons.includes("Open its Work") ? "Open its Work" : null));
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

  test("a Session's Changes (its menu) opens Git review of its folder", async () => {
    const files = [{ path: "app.ts", staged: false, unstaged: true, untracked: false, additions: 3, deletions: 1 }, { path: "notes.md", staged: false, unstaged: false, untracked: true, additions: 0, deletions: 0 }];
    const status = { available: true, repo_root: "/home/me/mewla", repo_name: "mewla", branch: "main", has_commits: true, clean: false, file_count: 2, staged_file_count: 0, unstaged_file_count: 1, untracked_file_count: 1, additions: 3, deletions: 1, files };
    const app = await sessionsApp({ worker_sessions: [session("w5", { name: "repo work" })], conversations: { w5: [] }, replies: { git_diff_status: { type: "git_diff_status", status } } }, { drawnHeader: true });
    await app.waitFor("text:repo work", { timeout: 10_000 });
    await app.press("text:repo work");
    await app.waitFor("id:session-menu");
    app.onDialog((dialog) => (dialog.buttons.includes("Changes") ? "Changes" : null));
    await answering(app, () => app.press("id:session-menu"));
    await app.waitFor("id:git-page");
    expect(framesOf(app.host.posts(), "git_diff_status").at(-1)).toMatchObject({ target_id: "w5", cwd: "/home/me/mewla" });
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
    await app.host.send("links.open", { url: "mewla://sessions/w6" });
    await app.waitFor("id:composer");
    // Under a full suite's load a press or the first keys after a sheet closes can be lost: each step waits for what it does.
    await pressUntil(app, "id:composer-plus", "id:attach-file");
    await pressUntil(app, "id:attach-file", "text:Ready");
    await typeUntil(app, "id:composer", "Read this", "id:send");
    await pressWhenEnabled(app, "id:send");
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
  test("in the terminal, Terminate offline says the computer is offline instead of doing nothing (parity #37)", async () => {
    const app = await sessionsApp({ worker_sessions: [session("w1", { name: "notes", status: "done" })], conversations: { w1: transcript } }, { drawnHeader: true });
    await app.waitFor("text:notes", { timeout: 10_000 });
    await app.press("text:notes");
    await app.waitFor("id:session-chat");
    app.onDialog((dialog) => (dialog.buttons.includes("Terminate") ? "Terminate" : null));
    await app.press("id:session-mode-terminal");
    await app.waitUntil(() => !app.shows("id:session-chat"));
    await app.host.send("app.daemonDown");
    await app.waitFor("id:terminal-offline", { timeout: 10_000 });
    await answering(app, () => app.press("id:session-menu"));
    await app.waitFor("id:terminal-notice");
    expect(app.texts()).toContain("Your computer is offline.");
    expect(app.dialogs().some((dialog) => dialog.title === "Terminate this session?")).toBe(false);
    expect(framesOf(app.host.posts(), "kill_worker")).toEqual([]);
  }, 20_000);

  test("the terminal sits in its surface group over a key bar of tiles in the mock's order, words in mono as the mock writes them", async () => {
    // A phone (touch: every key shows), the Session last left in its terminal.
    const host = phoneHost({ brain: {}, worker_sessions: [session("w1", { name: "notes", status: "done" })], conversations: { w1: transcript } });
    const app = await open(APP, { host, storage: { ...paired, "mewla:codex_render_modes": JSON.stringify({ w1: "terminal", "s1:w1": "terminal" }) }, platform: "android" });
    await app.host.send("links.open", { url: "mewla://sessions/w1" });
    await app.waitFor("id:terminal-group", { timeout: 10_000 });
    const keys = ["terminal-attach", "terminal-keyboard", "key-esc", "key-tab", "key-ctrl", "key-ctrl-c", "key-ctrl-d", "key-ctrl-b", "key-up", "key-down", "key-left", "key-right", "key-enter", "key-paste"];
    for (const key of keys) expect(app.count(`id:${key}`)).toBe(1);
    for (const word of ["esc", "tab", "ctrl", "^C", "^D", "^B"]) expect(app.texts()).toContain(word);
    for (const label of ["Attach", "Keyboard", "Up", "Down", "Left", "Right", "Enter", "Paste"]) expect(app.count(`label:${label}`)).toBe(1);
  }, 20_000);

  test("a file tapped in a Session without a live generation says why (parity #38)", async () => {
    const reply = [{ id: "a1", seq: 1, kind: "assistant_message", timestamp: new Date().toISOString(), body: "Wrote [the notes](/home/me/mewla/notes.md)." }];
    const app = await sessionsApp({ worker_sessions: [session("w2", { name: "writer" })], conversations: { w2: reply } });
    await app.waitFor("text:writer", { timeout: 10_000 });
    await app.press("text:writer");
    await app.waitFor("text:the notes");
    await app.press("text:the notes");
    await app.waitFor("text:This Session has no live generation identity. Refresh the Session list and try again.");
    expect(app.shows("id:file-preview")).toBe(false);
  }, 20_000);

  test("a file preview opened from a Session closes when the Session restarts (parity #44)", async () => {
    const reply = [{ id: "a1", seq: 1, kind: "assistant_message", timestamp: new Date().toISOString(), body: "Wrote [the notes](/home/me/mewla/notes.md)." }];
    const live = session("w3", { name: "writer", status: "running", process_id: 42, started_at: 1_700_000_000_000 });
    const app = await sessionsApp({ worker_sessions: [live], conversations: { w3: reply } });
    await app.waitFor("text:writer", { timeout: 10_000 });
    await app.press("text:writer");
    await app.waitFor("text:the notes");
    await app.press("text:the notes");
    await app.waitFor("id:file-preview");
    // The same generation again (a status update) keeps it open.
    await app.host.send("app.daemonPush", { type: "worker_session_updated", worker_session: { ...live, status: "done" } });
    await app.wait(300);
    expect(app.shows("id:file-preview")).toBe(true);
    await app.host.send("app.daemonPush", { type: "worker_session_updated", worker_session: { ...live, process_id: 43, started_at: 1_700_000_100_000 } });
    await app.waitUntil(() => !app.shows("id:file-preview"), { timeout: 5_000 });
    expect(app.shows("id:session-menu")).toBe(true);
  }, 20_000);
  test("New Terminal from a Session starts in its folder with its agent's Provider connection (parity #39, #40)", async () => {
    const providers = {
      type: "providers", revision: 1,
      connections: [{ id: "gw", name: "Gateway", preset_id: "custom", clients: ["claude"], credential_ready: true, advanced: false }],
      defaults: { claude: { connection_id: "gw" } },
      presets: [{ id: "custom", label: "Custom Gateway", clients: ["claude"], advanced: true }],
      models: {},
    };
    const app = await sessionsApp({ worker_sessions: [session("w4", { name: "site work", cwd: "/home/me/site" }), session("w0", { name: "other", cwd: "/home/me/mewla" })], conversations: { w4: transcript }, replies: { list_providers: providers } }, { drawnHeader: true });
    await app.waitFor("text:site work", { timeout: 10_000 });
    await app.press("text:site work");
    await app.waitFor("id:session-chat");
    app.onDialog((dialog) => (dialog.buttons.includes("New Terminal") ? "New Terminal" : null));
    await answering(app, () => app.press("id:session-menu"));
    await app.waitFor("id:new-cwd");
    // Its folder is listed first and chosen; another Session's comes after it.
    expect(app.texts().indexOf("/home/me/site")).toBeLessThan(app.texts().indexOf("/home/me/mewla"));
    await app.press("id:preset-claude");
    await app.press("id:new-launch");
    await app.waitUntil(() => framesOf(app.host.posts(), "create_session").length > 0, { timeout: 5_000 });
    expect(framesOf(app.host.posts(), "create_session").at(-1)).toMatchObject({ target_id: "w4", cwd: "/home/me/site", connection_id: "gw" });
  }, 20_000);
  test("an open Git review closes when its Session changes folder (parity #43)", async () => {
    const files = [{ path: "app.ts", staged: false, unstaged: true, untracked: false, additions: 3, deletions: 1 }];
    const status = { available: true, repo_root: "/home/me/mewla", repo_name: "mewla", branch: "main", has_commits: true, clean: false, file_count: 1, staged_file_count: 0, unstaged_file_count: 1, untracked_file_count: 0, additions: 3, deletions: 1, files };
    const repo = session("w8", { name: "repo work" });
    const app = await sessionsApp({ worker_sessions: [repo], conversations: { w8: [] }, replies: { git_diff_status: { type: "git_diff_status", status } } }, { drawnHeader: true });
    await app.waitFor("text:repo work", { timeout: 10_000 });
    await app.press("text:repo work");
    await app.waitFor("id:session-menu");
    app.onDialog((dialog) => (dialog.buttons.includes("Changes") ? "Changes" : null));
    await answering(app, () => app.press("id:session-menu"));
    await app.waitFor("id:git-page");
    await app.host.send("app.daemonPush", { type: "worker_session_updated", worker_session: { ...repo, cwd: "/home/me/site" } });
    await app.waitUntil(() => !app.shows("id:git-page"), { timeout: 5_000 });
    expect(app.shows("id:session-menu")).toBe(true);
  }, 20_000);
});
