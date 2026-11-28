import { describe, expect, open, test } from "nsh/test";
import { paired, phoneHost } from "../testing/fixtures";

// The Ledger (owner 11-27, variant A): a run of calls is one quiet line with its time and its failure count, closed
// like any other (owner 11-28: a failure never opens itself); opened, every step says what it did to what, with its result and time on the right.
const APP = "../FakeDaemonApp.tsx";
const host_worker = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: 1_762_840_800_000, process_id: 4242 };
const brain = { chat_thread_id: "t-now", host_worker };
const at = (s: number) => `2026-11-12T07:00:${String(s).padStart(2, "0")}.000Z`;

const call = (id: string, seq: number, tool: Record<string, unknown>, extra: Record<string, unknown> = {}) => ({
  id, seq, timestamp: at(seq), kind: tool.kind === "command" ? "command" : "tool", tool_name: String(tool.kind), status: tool.status, tool, ...extra,
});

const turn = [
  { id: "u1", seq: 1, timestamp: at(1), kind: "user_message", role: "user", body: "Prove the Mac update" },
  call("c2", 2, { kind: "command", target: "gh run view 4576 --repo me/x", intent: "Check the candidate run", status: "done", duration_ms: 1758 }, { command: "gh run view 4576 --repo me/x", output: "in_progress" }),
  call("c3", 3, { kind: "command", target: "ssh mac 'screencapture -x /tmp/a.png'", intent: "Test screencapture over SSH", status: "failed", result: "exit 1", duration_ms: 515 },
    { command: "ssh mac 'screencapture -x /tmp/a.png'", output: "could not create image from display", exit_code: 1 }),
  call("c4", 4, { kind: "wait", status: "done", duration_ms: 60_000 }),
  call("c5", 5, { kind: "read", target: "/home/me/w/cmd/mewla/computer_use.go", status: "done", result: "lines 200–330", duration_ms: 46 }),
  call("c6", 6, { kind: "edit", target: "/home/me/w/mac.py", status: "done", added: 12, removed: 3, duration_ms: 463 }),
  { id: "a7", seq: 7, timestamp: at(7), kind: "assistant_message", role: "assistant", body: "Screen Recording is off on the Mac." },
];

async function brainWith(events: Record<string, unknown>[]) {
  const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, conversations: { "brain-host": events } }) });
  await app.waitFor("id:composer", { timeout: 10_000 });
  return app;
}

describe("the tool Ledger", () => {
  test("a run is one line with its time and its failure count; the failed step stays closed until a tap, and a tap closes it again", async () => {
    const app = await brainWith(turn);
    await app.waitFor("text:/^Ran 2 commands, edited 1 file, read 1 file|^Edited 1 file, ran 2 commands, read 1 file/", { timeout: 10_000 });
    expect(app.shows("text:/· 1 failed$/")).toBe(true);
    // Closed, the run shows none of its steps, the failed one included: no row, no command, no output.
    expect(app.shows("text:/Test screencapture over SSH/")).toBe(false);
    expect(app.shows("text:/could not create image from display/")).toBe(false);
    expect(app.shows("text:/Check the candidate run/")).toBe(false);
    // Opened, the failed step is a closed row; a tap on it shows its command and output, another tap puts them away.
    await app.press("id:activity");
    await app.waitFor("id:ledger-step-failed", { timeout: 5_000 });
    expect(app.shows("text:/could not create image from display/")).toBe(false);
    await app.press("id:ledger-step-failed");
    await app.waitFor("text:/could not create image from display/", { timeout: 5_000 });
    await app.press("id:ledger-step-failed");
    expect(app.shows("text:/could not create image from display/")).toBe(false);
  }, 20_000);

  test("a lone failed call is a closed row that says it failed, and opens on a tap", async () => {
    const app = await brainWith([turn[0], turn[2], turn[6]]);
    await app.waitFor("id:ledger-step-failed", { timeout: 10_000 });
    expect(app.shows("text:/exit 1/")).toBe(true);
    expect(app.shows("text:/could not create image from display/")).toBe(false);
    await app.press("id:ledger-step-failed");
    await app.waitFor("text:/could not create image from display/", { timeout: 5_000 });
  }, 20_000);

  test("opened, every step names what it did, and a Sleep is no row", async () => {
    const app = await brainWith(turn);
    await app.waitFor("id:ledger-run", { timeout: 10_000 });
    await app.press("id:activity");
    await app.waitFor("text:/Check the candidate run/", { timeout: 5_000 });
    expect(app.shows("text:/Read cmd\\/mewla\\/computer_use\\.go|Read computer_use\\.go/")).toBe(true);
    expect(app.shows("text:/lines 200–330/")).toBe(true);
    expect(app.shows("text:/Edit /")).toBe(true);
    expect(app.shows("text:/\\+12/")).toBe(true);
    expect(app.shows("text:/^Wait|Sleep/")).toBe(false);
  }, 20_000);
});
