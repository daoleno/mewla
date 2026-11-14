/**
 * The soft-final mock's own scenario (site/v/soft-final/index.html: S, SESS, WORK, PCS, EVENTS, the Brain thread
 * and the Rate-limit Session's thread) as a scripted computer's state, so the fidelity sheet's pairs show the same
 * content on both sides. For `fixture/FixtureApp.tsx` and its test only; the app never imports it.
 */
import dashboard from "../../core/resources/fixtures/dashboard-v2.json";
import type { FakeDaemonState } from "../testing/fakeDaemon";
import { SKETCH, TALL, WIDE } from "./pictures";

/**
 * The fixture's "now" in its test: the mock's day (the 12th) at 14:00 local, after the Work began (13:20) and before
 * the day's first reminder (15:00), as the mock shows it. The scenario's clock times are today's, so on the wall
 * clock what needs you changed through the day (from 15:00 the reminder's alert made it "4 need you"); the test
 * pins the app's clock here instead. On a device (`nsh dev`) the scenario keeps the real clock.
 */
export const FIXTURE_NOW = new Date(2026, 10, 12, 14, 0).getTime();

const MIN = 60_000;
const HOUR = 60 * MIN;
const ago = (ms: number) => new Date(Date.now() - ms).toISOString();
/** Today at hh:mm, local time. */
function today(hhmm: string, days = 0): string {
  const [h, m] = hhmm.split(":").map(Number);
  const d = new Date();
  d.setDate(d.getDate() + days);
  d.setHours(h, m, 0, 0);
  return d.toISOString();
}

/** The mock's three computers paired (PCS), workbench current; studio and laptop are the `computers` message's. */
export const storage = {
  "mewla:v3:servers": JSON.stringify([
    { id: "s1", name: "workbench", url: "ws://127.0.0.1:1/ws", daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64) },
    { id: "s2", name: "studio", url: "ws://127.0.0.1:2/ws", daemonId: "c".repeat(64), daemonPublicKey: "e".repeat(64) },
    { id: "s3", name: "laptop", url: "ws://127.0.0.1:3/ws", daemonId: "d".repeat(64), daemonPublicKey: "f".repeat(64) },
  ]),
  "mewla:v1:current_server_id": "s1",
  "mewla:onboarded": "true",
};

const AGENT = { claude: "claude", codex: "codex", cursor: "cursor-agent", opencode: "opencode" } as const;
type Agent = keyof typeof AGENT;

/** Ids the sheet and its test open. */
export const IDS = { ratelimit: "ratelimit", pricing: "pricing", search: "search", docsWork: "w-docsref", pricingWork: "w-pricing" } as const;

interface Sess {
  id: string; agent: Agent; name: string; proj: string;
  st: "wait" | "run" | "idle" | "done"; act: string; t?: number | string; work?: string; brief?: string; reply?: string;
}

/** SESS, as the mock lists them (`t` in minutes for a running one). */
const SESS: Sess[] = [
  { id: "ratelimit", agent: "claude", name: "Rate-limit window fix", proj: "atlas-api", st: "wait", act: "Wants to push a branch",
    brief: "Fix the off-by-one in atlas-api's sliding window and get the rate-limit tests green." },
  { id: "pricing", agent: "claude", name: "Pricing page rework", proj: "landing-site", st: "wait", act: "Building the comparison table", t: 12,
    brief: "Rework the pricing page: three plans, a comparison table, annual toggle. Match the new tokens.",
    reply: "Plans and the annual toggle are in. Building the comparison table now, then a pass on mobile widths." },
  { id: "flaky", agent: "codex", name: "Flaky checkout test", proj: "shop-web", st: "run", act: "Rerunning the suite", t: 4,
    brief: "checkout.spec fails about one run in six on CI. Find the cause, fix it, prove it with five clean runs.",
    reply: "It was a race between the cart badge and the payment iframe. Waiting on the iframe now; 3 of 5 runs clean so far." },
  { id: "search", agent: "codex", name: "Search service setup", proj: "search-svc", st: "wait", act: "Trust this folder?",
    brief: "Set up search-svc locally and get its test suite running.", reply: "Before I start: this folder is new to me." },
  { id: "quickstart", agent: "claude", name: "Quickstart rewrite", proj: "atlas-docs", st: "run", act: "Rewriting the quickstart", t: 9, work: "docsref" },
  { id: "shots", agent: "codex", name: "Screenshot refresh", proj: "atlas-docs", st: "run", act: "Retaking 14 screenshots", t: 6, work: "docsref" },
  { id: "apiref", agent: "opencode", name: "API reference", proj: "atlas-docs", st: "run", act: "Regenerating the reference", t: 3, work: "docsref" },
  { id: "cache", agent: "claude", name: "Cache layer for the catalogue API", proj: "atlas-api", st: "run", act: "Writing the eviction tests", t: 21 },
  { id: "i18n", agent: "opencode", name: "German translation", proj: "landing-site", st: "run", act: "Translating 38 strings", t: 8 },
  { id: "deps", agent: "codex", name: "Dependency audit", proj: "atlas-api", st: "run", act: "Checking 212 packages", t: 5 },
  { id: "a11y", agent: "claude", name: "Accessibility pass", proj: "landing-site", st: "run", act: "Fixing button contrast", t: 15 },
  { id: "logs", agent: "cursor", name: "Structured logging", proj: "atlas-api", st: "run", act: "Replacing 41 log calls", t: 11 },
  { id: "perf", agent: "claude", name: "Cart page speed", proj: "shop-web", st: "run", act: "Profiling the first render", t: 7 },
  { id: "mail", agent: "codex", name: "Order email templates", proj: "shop-web", st: "run", act: "Rendering previews", t: 2 },
  { id: "backup", agent: "opencode", name: "Backup drill script", proj: "atlas-api", st: "run", act: "Restoring to a scratch database", t: 18 },
  { id: "images", agent: "codex", name: "Hero image formats", proj: "landing-site", st: "run", act: "Converting 9 images", t: 1 },
  { id: "docs", agent: "opencode", name: "Docs link sweep", proj: "atlas-docs", st: "idle", act: "42 links checked", t: 120,
    brief: "Check every outbound link in atlas-docs and list the dead ones.", reply: "42 links checked, 3 dead. The list is in links-report.md. Say the word and I will fix them." },
  { id: "node", agent: "codex", name: "Node 22 upgrade", proj: "atlas-api", st: "done", act: "Merged, CI green", t: "11:20",
    brief: "Move atlas-api to Node 22 and keep CI green.", reply: "Upgraded, two deprecated calls replaced, CI green. Merged at 11:20." },
  { id: "tokens", agent: "cursor", name: "Dark-mode tokens", proj: "landing-site", st: "done", act: "11 files, review passed", t: "Yesterday",
    brief: "Replace hard-coded colours on landing-site with the dark-mode tokens.", reply: "11 files moved to tokens. Review passed yesterday." },
];

const RATELIMIT_CMD = "git push origin fix/rate-limit-window";

function workerSession(s: Sess): Record<string, unknown> {
  const status = s.st === "run" ? "running" : s.st === "wait" ? "blocked" : s.st === "done" ? "done" : "unknown";
  const base: Record<string, unknown> = {
    id: s.id, name: s.name, status, project: s.proj, cwd: `/home/me/work/${s.proj}`, command: AGENT[s.agent], provider: s.agent,
    delegated: true, summary: "", last_output_lines: [], started_at: ago(3 * HOUR),
    updated_at: s.st === "done" ? (s.t === "Yesterday" ? today("16:40", -1) : today(String(s.t))) : s.st === "idle" ? ago(2 * HOUR) : ago(MIN),
  };
  if (status === "running") Object.assign(base, { activity: s.act, running_since: ago(Number(s.t) * MIN) });
  if (s.id === "ratelimit") base.approvals = [{ id: "ap-push", kind: "permission", tool: "Bash", summary: RATELIMIT_CMD,
    options: [{ id: "allow", label: "Approve", scope: "once" }, { id: "deny", label: "Deny", scope: "deny" }] }];
  if (s.id === "pricing") base.prompt = { id: "q-trial", kind: "choice", provider: "claude", title: "14-day or 7-day trial?",
    questions: [{ question: "The pricing copy promises a 14-day trial. Billing is set to 7. Which one ships?",
      options: [{ number: 1, label: "14 days, fix billing (Recommended)" }, { number: 2, label: "7 days, fix the copy" }, { number: 3, label: "Hold the page for now" }] }] };
  if (s.id === "search") base.prompt = { id: "tr-folder", kind: "startup", provider: "codex", title: "Trust this folder?", request: "~/work/search-svc",
    options: [{ number: 1, label: "Yes, trust this folder" }, { number: 2, label: "No, exit", selected: true }] };
  return base;
}

const at = (minutesAgo: number) => ago(minutesAgo * MIN);
let seq = 0;
const ev = (e: Record<string, unknown>, minutesAgo: number) => ({ id: `e${++seq}`, seq, timestamp: at(minutesAgo), ...e });
const user = (body: string, m: number, extra: Record<string, unknown> = {}) => ev({ kind: "user_message", role: "user", body, ...extra }, m);
const reply = (body: string, m: number) => ev({ kind: "assistant_message", role: "assistant", body }, m);
const read = (path: string, m: number) => ev({ kind: "tool_call", role: "assistant", call_id: `c${seq + 1}`, tool_name: "Read", title: `Read ${path.split("/").pop()}`,
  input: JSON.stringify({ file_path: path, path }), status: "completed" }, m);
const run = (command: string, m: number) => ev({ kind: "tool_call", role: "assistant", call_id: `c${seq + 1}`, tool_name: "Bash", title: command,
  input: JSON.stringify({ command }), status: "completed" }, m);
const PROTOCOL = "Mewla lifecycle protocol:\nProfile: long_running.\nYou are the delegated Worker: execute the assigned work directly.";
const brief = (task: string, m: number) => user(`Execute:\n\n${task}\n\n${PROTOCOL}`, m, { origin: "delegated" });

function workResult(work: string, title: string, body: string, session: Sess, m: number, patch: Record<string, unknown> = {}) {
  return ev({ kind: "status", source: "work_result", status: "session.done", title, body, work_id: work, work_session_id: session.id, session_name: session.name,
    unread: false, work_review_state: "resolved", work_session_state: "finalized", work_result_current: true, ...patch }, m);
}

const byId = (id: string) => SESS.find((s) => s.id === id)!;
const ARRIVING = "Tonight at 23:30, and it is on the Calendar. I will hold it if the rate-limit push is not approved by then.";

/** Brain's thread (the mock's buildThread, not fresh): a sent picture, a folded read, the Work cards, a sealed line, the open requests' stack, the latest question. */
const brainThread = [
  user("Ship the pricing page rework on landing-site, refresh the atlas-docs guides, and get atlas-api's rate-limit tests green before tonight's deploy.", 50,
    { attachments: [{ name: "sketch.png", path: "/state/uploads/sketch.png", content_type: "image/png" }] }),
  read("/home/me/workspace/routing.md", 49),
  read("/home/me/workspace/current.md", 49),
  read("/home/me/work/landing-site/src/pricing.tsx", 49),
  ev({ kind: "tool_call", role: "assistant", call_id: "c-grep", tool_name: "Grep", title: "\"rate limit\" in atlas-api", input: JSON.stringify({ pattern: "rate limit", path: "/home/me/work/atlas-api" }), status: "completed" }, 49),
  reply("Three jobs. Claude takes the pricing page, three Workers split the docs, and the rate-limit fix is already in review.", 48),
  workResult("w-node", "Node 22 upgrade", "Upgraded, two deprecated calls replaced, CI green. Merged at 11:20.", byId("node"), 40, { unread: true }),
  workResult(IDS.pricingWork, "Pricing page rework", "", byId("pricing"), 39, { work_result_current: false }),
  workResult(IDS.docsWork, "Docs refresh", "", byId("quickstart"), 38, { work_result_current: false }),
  workResult("w-tokens", "Dark-mode tokens", "11 files moved to tokens. Review passed yesterday.", byId("tokens"), 37),
  ev({ kind: "status", source: "request_answered", role: "system", body: "Approved", status: "approved", request: {
    id: "approval:node:ap-node", kind: "approval", session_id: "node", thread_id: "t-now", title: "Bash", tool: "Bash", summary: "git push origin chore/node-22",
    asked: "git push origin chore/node-22", answer: "Allow", outcome: "approved", by: { kind: "device", device_name: "Pixel" }, answered_at: at(36), anchor: { approval_id: "ap-node" } } }, 36),
  user("When does tonight's deploy go out?", 2),
  reply(ARRIVING, 1),
];

/** The Rate-limit Session (the mock's #s-session): Brain's brief, a folded read and test run, a code block, the edited files, the push to approve. */
const ratelimitThread = [
  brief(byId("ratelimit").brief!, 30),
  read("/home/me/work/atlas-api/ratelimit/window.go", 29),
  read("/home/me/work/atlas-api/ratelimit/window_test.go", 29),
  run("go test ./ratelimit/... -race", 28),
  reply("The window counted the request on the boundary twice. Fixed in `window.go`, with a test that pins it:\n\n```go\nfunc TestWindowBoundary(t *testing.T) {\n    w := NewWindow(3, time.Second)\n    for i := 0; i < 3; i++ {\n        require.True(t, w.Allow(t0))\n    }\n    // the 4th hit at the edge must fail\n    require.False(t, w.Allow(t0.Add(time.Second)))\n}\n```", 20),
  ev({ kind: "patch", role: "assistant", status: "completed", file_changes: [
    { path: "ratelimit/window_test.go", operation: "add", additions: 42, deletions: 0 },
    { path: "ratelimit/window.go", operation: "update", additions: 9, deletions: 5 },
    { path: "ratelimit/limiter.go", operation: "update", additions: 7, deletions: 7 },
  ] }, 19),
  reply("14 tests pass with `-race`. Ready to push.", 18),
];

/** Every other Session the mock opens: Brain's brief, "Ran 6 commands", its reply. Pricing adds the screenshots and an Edit. */
function sessionThread(s: Sess) {
  if (s.id === "ratelimit") return ratelimitThread;
  const steps = [`git checkout -b work/${s.id}`, "npm test", "git diff --stat"].map((c) => run(c, 20));
  // Pricing: the screenshots fold into the run (pictures under it), and the Edit after the reply stands alone (its chip).
  const shots = s.id === "pricing" ? [read("/home/me/work/landing-site/shots/pricing-mobile.png", 14), read("/home/me/work/landing-site/shots/pricing-desktop.png", 14)] : [];
  const edit = s.id === "pricing" ? [ev({ kind: "tool", tool_name: "Edit", title: "Tool", status: "done", source: "claude_transcript",
    input: JSON.stringify({ file_path: "/home/me/work/landing-site/src/pricing.tsx", old_string: "a", new_string: "b" }),
    files: ["/home/me/work/landing-site/src/pricing.tsx"],
    file_changes: [{ path: "/home/me/work/landing-site/src/pricing.tsx", operation: "update", additions: 38, deletions: 12 }] }, 5)] : [];
  return [s.brief ? brief(s.brief, 25) : brief(`${s.name} in ${s.proj}.`, 25), ...steps, ...shots, reply(s.reply ?? `${s.act}.`, 10), ...edit];
}

/** The phases the mock's Workers report (its steps 2, 3, 2 of five: planning, working, planning). */
const PHASE: Record<string, string> = { quickstart: "planning", shots: "working", apiref: "planning" };

function joined(s: Sess) {
  return { session_id: s.id, name: s.name, provider: s.agent, state: "running", summary: s.act, live: true, joined_at: at(Number(s.t) + 1), updated_at: at(1), ...(PHASE[s.id] ? { phase: PHASE[s.id] } : {}) };
}
const docsWorkers = SESS.filter((s) => s.work === "docsref");

const brain = {
  chat_thread_id: "t-now",
  host_worker: { id: "brain-host", name: "Brain", cwd: "/home/me/workspace", command: "claude", provider: "claude", started_at: ago(6 * HOUR) },
  current_work: [
    { work_id: IDS.docsWork, revision: 4, title: "Docs refresh", status: "running", progress_mode: "owned", unread_result: false, attempt_session_id: "quickstart", attempt_delegated: true,
      summary: "Refresh the atlas-docs guides for the new API: quickstart, screenshots and the generated reference.", created_at: today("13:20"), updated_at: at(1),
      worker_sessions: docsWorkers.map(joined) },
    { work_id: IDS.pricingWork, revision: 2, title: "Pricing page rework", status: "running", progress_mode: "owned", unread_result: false, attempt_session_id: "pricing", attempt_delegated: true,
      summary: "Three plans, a comparison table and an annual toggle on landing-site, matching the new tokens.", created_at: today("12:58"), updated_at: at(1),
      worker_sessions: [joined({ ...byId("pricing"), t: 12 })] },
  ],
  open_work: [],
};

const DOCS_DETAIL = {
  work_id: IDS.docsWork, revision: 4, title: "Docs refresh", status: "running", created_at: today("13:20"), updated_at: at(1),
  summary: "Refresh the atlas-docs guides for the new API: quickstart, screenshots and the generated reference.",
  worker_sessions: docsWorkers.map(joined),
};

/** The phone's time zone, else UTC. */
function zone(): string {
  try { return new Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC"; } catch { return "UTC"; }
}

/** EVENTS, the mock's month around "today" (the 12th there): day offsets from today. */
const EVENTS: [number, string, "bell" | "sync" | "flash", string][] = [
  [0, "15:00", "bell", "Review the pricing copy"], [0, "18:00", "sync", "Check atlas-api error budget"], [0, "23:30", "flash", "Deploy landing-site"],
  [1, "09:00", "flash", "Rotate staging keys"], [4, "10:00", "bell", "Renew the landing-site domain"], [4, "22:00", "flash", "Weekly dependency review"],
  [7, "14:00", "bell", "Demo run-through"], [8, "23:00", "flash", "Database backup drill"], [14, "08:00", "bell", "Quarterly cost review"],
];
const calendar_items = EVENTS.map(([day, time, kind, title], i) => {
  const when = today(time, day);
  const base = { id: `cal-${i}`, title, status: "scheduled", next_at: when, timezone: zone(),
    created_at: ago(7 * 24 * HOUR), updated_at: ago(24 * HOUR), revision: 1 };
  if (kind === "bell") return { ...base, kind: "reminder", notify_at: when, recurrence: "none" };
  if (kind === "sync") return { ...base, kind: "reminder", notify_at: when, recurrence: "daily" };
  return { ...base, kind: "scheduled_action", due_at: when, recurrence: "none", action_instruction: title, action_cwd: "/home/me/work/landing-site" };
});

export function scenario(): FakeDaemonState & { assets: Record<string, string> } {
  const conversations: FakeDaemonState["conversations"] = { "brain-host": brainThread };
  for (const s of SESS) conversations[s.id] = sessionThread(s);
  return {
    brain,
    worker_sessions: SESS.map(workerSession),
    conversations,
    calendar_items,
    // The Work's file (its Work detail's "Work file" row), as the computer lists it.
    work_items: [{
      id: IDS.docsWork, path: "/home/me/.mewla/work/atlas-docs/docs-refresh.md", project: "atlas-docs", title: "Docs refresh",
      body: "# Docs refresh\n\nRefresh the atlas-docs guides for the new API: quickstart, screenshots and the generated reference.\n",
      frontmatter: { id: IDS.docsWork, kind: "task", created: today("13:20"), started: today("13:21"), title: "Docs refresh" }, mtime: at(2),
    }],
    computers: { computers: [
      { daemon_id: "c".repeat(64), name: "studio", online: true, open_requests: 0 },
      { daemon_id: "d".repeat(64), name: "laptop", online: false, open_requests: 0, last_seen: today("09:14") },
    ], open_requests: 0 },
    assets: {
      "/state/uploads/sketch.png": SKETCH,
      "/home/me/work/landing-site/shots/pricing-mobile.png": TALL,
      "/home/me/work/landing-site/shots/pricing-desktop.png": WIDE,
    },
    replies: {
      brain_work_detail: { type: "brain_work_detail", work: DOCS_DETAIL },
      approval_decision: { type: "approval_decided" },
      // Every Session's folder is a repository (the mock's `#git/ratelimit`): the header shows its Git button.
      git_diff_status: { type: "git_diff_status", status: {
        available: true, repo_root: "/home/me/work/atlas-api", repo_name: "atlas-api", branch: "fix/rate-limit-window", has_commits: true, clean: false,
        file_count: 2, staged_file_count: 0, unstaged_file_count: 1, untracked_file_count: 1, additions: 9, deletions: 5,
        files: [
          { path: "ratelimit/window.go", status: "modified", staged: false, unstaged: true, untracked: false, working_additions: 9, working_deletions: 5 },
          { path: "ratelimit/window_test.go", status: "untracked", staged: false, unstaged: false, untracked: true, working_additions: 42 },
        ],
      } },
      // A getter: every read is sampled now, as the computer's own five-second poll would be.
      // Its history moves with it, so the window stays the five minutes it holds.
      get get_resource_telemetry() {
        const shift = Date.now() - Date.parse(dashboard.sampled_at);
        const at = (iso: string) => new Date(Date.parse(iso) + shift).toISOString();
        return {
          type: "resource_telemetry", ...dashboard, sampled_at: at(dashboard.sampled_at),
          history: dashboard.history.map((sample) => ({ ...sample, sampled_at: at(sample.sampled_at) })),
        };
      },
    } as NonNullable<FakeDaemonState["replies"]>,
  };
}
