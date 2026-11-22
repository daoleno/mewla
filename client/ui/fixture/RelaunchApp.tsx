// The app on a computer whose agents ended while their Works were open, for the relaunch shots
// (client/scripts/shots/relaunch.js, with --entry): one is being started again, one was started again in the same
// conversation, one was not (the restart bound). Never bundled: App.tsx is the app's entry and does not import this.
import "./seed";
import { host } from "nsh";
import "../App";
import { installPlatform, platform } from "../../core/platform";
import { FakeDaemon } from "../testing/fakeDaemon";
import { scenario } from "./scenario";

const MIN = 60_000;
const ago = (minutes: number) => new Date(Date.now() - minutes * MIN).toISOString();
const base = scenario();
const ended = [
  { id: "r-died", work: "w-r-died", title: "Changelog for 0.5", agent: "claude", status: "running", relaunch: { state: "restarting", line: "Restarting after SIGKILL" } },
  { id: "r-back", work: "w-r-back", title: "Invoice export", agent: "codex", status: "done", relaunch: { state: "restarted", line: "Restarted after SIGKILL" } },
  { id: "r-gave", work: "w-r-gave", title: "Search index rebuild", agent: "claude", status: "failed", relaunch: { state: "not_restarted", line: "Gave up after 3 restarts" } },
] as const;
const sessions = ended.map((s) => ({
  id: s.id, name: s.title, status: s.status, project: "atlas-api", cwd: "/home/me/work/atlas-api", command: s.agent, provider: s.agent,
  delegated: true, summary: s.relaunch.line, relaunch: s.relaunch, last_output_lines: [], started_at: ago(40), updated_at: ago(1),
  ...(s.status === "running" ? { running_since: ago(1) } : {}),
}));
const works = ended.map((s, i) => ({
  work_id: s.work, revision: 3, title: s.title, status: "running", progress_mode: "owned", unread_result: false, attempt_session_id: s.id, attempt_delegated: true,
  summary: "", created_at: ago(40 - i), updated_at: ago(1),
  worker_sessions: [{ session_id: s.id, name: s.title, provider: s.agent, state: s.status, summary: "", live: true, joined_at: ago(39 - i), updated_at: ago(1) }],
}));
let seq = 900;
const ev = (e: Record<string, unknown>, minutes: number) => ({ id: `r${++seq}`, seq, timestamp: ago(minutes), ...e });
const thread = [
  ev({ kind: "user_message", role: "user", body: "Write the 0.5 changelog, add the invoice export, and rebuild the search index." }, 42),
  ev({ kind: "assistant_message", role: "assistant", body: "Three Workers are on it." }, 41),
  ...ended.map((s, i) => ev({ kind: "status", source: "work_result", status: "session.done", title: s.title, body: "", work_id: s.work, work_session_id: s.id, session_name: s.title,
    unread: false, work_review_state: "resolved", work_session_state: "finalized", work_result_current: false }, 40 - i)),
];
const conversations: Record<string, unknown[]> = { ...base.conversations, "brain-host": thread };
for (const s of ended) {
  conversations[s.id] = [
    ev({ kind: "user_message", role: "user", origin: "delegated", body: `Execute:\n\n${s.title} in atlas-api.` }, 39),
    ev({ kind: "assistant_message", role: "assistant", body: "On it." }, 38),
  ];
}
const daemon = new FakeDaemon({
  ...base,
  brain: { ...base.brain, current_work: works },
  worker_sessions: sessions,
  conversations: conversations as typeof base.conversations,
});
installPlatform({ ...platform(), openSocket: () => daemon.open() });
host.on<Record<string, unknown>>("app.daemonPush", (frame) => daemon.push(frame as { type: string }));
