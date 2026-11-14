/**
 * A computer shaped as the owner's was on 2026-11-14, for budget tests: none of its words, its sizes and its mix.
 *
 * - Brain's thread: the 240 events the daemon keeps of it, 401 KB. 152 commands with their output (a command 441
 *   characters on average, a heredoc up to 3 KB; output 1.4 KB), 40 replies of 1.6 KB of Markdown, 30 messages of
 *   0.6 KB, 11 Read and Write calls with 1.7 KB of input, 6 status notes of 1.7 KB.
 * - 60 Sessions (the list was 46-66 KB), with their terminal tails as a v0.4.2 app still gets them.
 * - 57 Work items, 250 KB, and six Works in Brain's snapshot with three Workers each.
 */

const at = (i: number) => new Date(Date.UTC(2026, 10, 14, 6, 0, 0) + i * 20_000).toISOString();
const words = (n: number, seed: number) =>
  Array.from({ length: n }, (_, i) => ["the", "release", "Session", "tests", "green", "phone", "构建", "通过", "daemon", "worktree", "commit", "检查"][(i * 7 + seed) % 12]).join(" ");
const path = (i: number) => `/home/me/.mewla/worktrees/project-${i % 9}/client/${["core", "ui"][i % 2]}/module${i % 23}.ts`;

const COMMANDS = [
  (i: number) => `cd /home/me/.mewla/worktrees/project-${i % 9} && git log --oneline -15 origin/main && git status --short | head -20`,
  (i: number) => `rg -n "sessionRowModel|presentWorker" client/core client/ui --glob '!*.test.*' | head -${20 + (i % 30)}`,
  (i: number) => `sed -n ${i},${i + 80}p ${path(i)}`,
  (i: number) => `cat > /tmp/notes-${i}.md <<'EOF'\n${Array.from({ length: 12 + (i % 40) }, (_, l) => `- ${words(9, i + l)}`).join("\n")}\nEOF`,
  (i: number) => `python3 - <<'EOF'\nimport json,sys\n${Array.from({ length: 6 + (i % 25) }, (_, l) => `print(json.dumps({"line": ${l}, "text": "${words(6, l)}"}))`).join("\n")}\nEOF`,
  (i: number) => `GH_TOKEN=$(gh auth token -u me) gh run list --repo me/project --limit ${5 + (i % 10)} --json databaseId,status,conclusion`,
  (i: number) => `cd /home/me/.mewla/worktrees/project-${i % 9}/client/ui && nsh test chat/Thread${i % 7}.test.tsx 2>&1 | tail -${3 + (i % 5)}`,
  (i: number) => `go test ./server -run 'TestFirstEvents|TestFrame${i % 4}' -count=1 2>&1 | tail -5`,
];
const output = (i: number) => Array.from({ length: 18 + (i % 14) }, (_, l) => `${String(l + 1).padStart(4)}  ${words(8, i * 3 + l)}`).join("\n");

/** The thread's events, oldest first. */
export function ownerShapedThread(): Record<string, unknown>[] {
  const events: Record<string, unknown>[] = [];
  let commands = 0;
  for (let i = 0; i < 240; i += 1) {
    const base = { id: `e${i}`, seq: i + 1, timestamp: at(i), source: "claude_transcript" };
    const slot = i % 48;
    if (slot === 0 || slot === 17 || slot === 33 || slot === 41 || slot === 46 || (slot === 9 && i % 96 === 9)) {
      events.push({ ...base, kind: "user_message", role: "user", body: `${words(60, i)}\n\n${words(30, i + 1)}` });
    } else if (slot % 6 === 5 || slot === 47) {
      events.push({ ...base, kind: "assistant_message", role: "assistant", body: `**${words(5, i)}**\n\n${Array.from({ length: 9 }, (_, l) => `- ${words(14, i + l)} \`${path(l)}\``).join("\n")}\n\n${words(40, i + 2)}` });
    } else if (slot === 22 && events.filter((e) => e.kind === "status").length < 6) {
      events.push({ ...base, kind: "status", title: "Task notification", task_id: `t${i}`, body: words(240, i) });
    } else if ((slot === 14 || slot === 30) && events.filter((e) => e.kind === "tool").length < 11) {
      const write = i % 2 === 0;
      events.push({ ...base, kind: "tool", call_id: `toolu_${i}`, tool_name: write ? "Write" : "Read", input: JSON.stringify({ file_path: path(i), ...(write ? { content: words(260, i) } : {}) }), output: write ? "File created successfully" : output(i).slice(0, 90), files: [path(i)], status: "done" });
    } else {
      events.push({ ...base, kind: "command", call_id: `toolu_${i}`, command: COMMANDS[commands % COMMANDS.length]!(i), output: output(i), body: "Exit code 0", status: "done", exit_code: 0 });
      commands += 1;
    }
  }
  return events;
}

const tail = Array.from({ length: 40 }, (_, i) => `  ${words(10, i)} ${"·".repeat(40)}`);

/** The computer's Sessions, as worker_session_list carries them to a v0.4.2 app (terminal tails included). */
export function ownerShapedSessions(now: number): Record<string, unknown>[] {
  return Array.from({ length: 60 }, (_, i) => ({
    id: `w${i}`, name: `project-${i % 9} ${words(2, i)}`, status: ["running", "done", "idle", "failed", "blocked"][i % 5],
    cwd: `/home/me/.mewla/worktrees/project-${i % 9}-${i}`, command: "claude --model claude-opus-5-5 --permission-mode bypassPermissions",
    provider: i % 4 ? "claude" : "codex", executor_id: "claude", summary: i % 3 ? `Reporting: ${words(14, i)}` : "⏵⏵ bypass permissions on (shift+tab to cycle)",
    phase: i % 2 ? "working" : "verifying", event_kind: i % 2 ? "progress" : undefined, last_output_lines: tail,
    updated_at: now - i * 60_000, started_at: now - i * 3_600_000, running_since: i % 5 === 0 ? now - 120_000 : undefined,
    activity: `Bash ${COMMANDS[i % COMMANDS.length]!(i).split("\n")[0]}`, activity_tool: "Bash", activity_summary: COMMANDS[i % COMMANDS.length]!(i).split("\n")[0], delegated: i % 3 === 0,
  }));
}

/** The Work items, 57 of them as the snapshot carried. */
export function ownerShapedWorkItems(): Record<string, unknown>[] {
  return Array.from({ length: 57 }, (_, i) => ({
    id: `wf-${i}`, project: `project-${i % 9}`, path: `project-${i % 9}/wf-${i}.md`, title: words(6, i), mtime: at(i),
    body: `# ${words(6, i)}\n\n${Array.from({ length: 52 }, (_, l) => `- ${words(12, i + l)}`).join("\n")}\n`,
    frontmatter: { id: `wf-${i}`, kind: "task", created: at(i), worker_session: `w${i % 60}` },
  }));
}

/** The Works Brain's snapshot lists, each with three Workers. */
export function ownerShapedCurrentWork(): Record<string, unknown>[] {
  return Array.from({ length: 6 }, (_, i) => ({
    work_id: `cw${i}`, revision: 3, title: `Work ${i}: ${words(5, i)}`, status: i % 2 ? "running" : "needs_input", unread_result: false, attempt_session_id: `w${i}`,
    worker_sessions: [0, 1, 2].map((j) => ({ session_id: `w${i * 3 + j}`, name: `worker ${j}`, provider: "claude", state: j ? "running" : "done", summary: words(8, j), live: j > 0, joined_at: "2026-11-14T06:00:00Z", updated_at: "2026-11-14T06:01:00Z" })),
  }));
}
