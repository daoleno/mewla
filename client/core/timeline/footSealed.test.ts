import { describe, expect, test } from "bun:test";
import { parseAnsweredRequest, sealedOfAnswered } from "./answered";
import { footSealed, newestRowAt, type SealedRequest } from "./requestCards";

// The 2026-11-17 incident's two records (the live ledger, the command shortened): a delegated Worker's one Bash
// call, answered "Yes" on its dialog from Mewla Web, and its hook's approval answered "Allow" by Brain 77 s later.
// Both carry the Work, so Brain's thread showed both, and no thread holds them as rows.
const command = "cd /home/u/.mewla/worktrees/upgrade-logcat-1117/daemon && go test -race -c -o /tmp/relay.test ./relay";
const owner = parseAnsweredRequest({
  id: "prompt:mh-a75e:eea585040c34:1794880134320", kind: "permission", session_id: "mh-a75e", work_id: "3d9f1f3a", title: "Bash command",
  tool: "Bash", summary: command, asked: command, answer: "Yes", outcome: "approved",
  by: { kind: "device", device_id: "bb9bf3dd", device_name: "Mewla Web" },
  asked_at: "2026-11-17T01:48:54.320Z", answered_at: "2026-11-17T01:48:59.142Z", anchor: { prompt_id: "eea585040c34" },
})!;
const brain = parseAnsweredRequest({
  id: "approval:mh-a75e:7e22c5f2", kind: "approval", session_id: "mh-a75e", work_id: "3d9f1f3a", title: "Bash",
  tool: "Bash", summary: command, asked: command, answer: "Allow", outcome: "approved",
  by: { kind: "brain", basis: "Brief: fix the relay test red at its cause" },
  answered_at: "2026-11-17T01:50:16.751Z", anchor: { approval_id: "7e22c5f2" },
})!;
const seal = (record: NonNullable<typeof owner>) => sealedOfAnswered(record, () => "");
const brainThread = { brain: true } as const;

describe("the answered lines at a thread's foot", () => {
  test("a request Brain answered leaves no line: Brain's own step says it", () => {
    const shown = footSealed([seal(owner), seal(brain)], brainThread);
    expect(shown.map((s) => s.key)).toEqual(["prompt:mh-a75e:eea585040c34"]);
    expect(shown[0]).toMatchObject({ verb: "Approved", answer: command, mono: true });
  });

  test("once the thread has a newer row the answer is history, and the foot lets it go", () => {
    const answeredAt = Date.parse("2026-11-17T01:48:59.142Z");
    expect(footSealed([seal(owner)], brainThread, undefined, answeredAt - 1_000)).toHaveLength(1);
    expect(footSealed([seal(owner)], brainThread, undefined, answeredAt + 1_000)).toHaveLength(0);
    // Hours of Brain's thread later, nothing of it is left above the composer.
    const later = newestRowAt([{ timestamp: "2026-11-17T01:40:00Z" }, { timestamp: "2026-11-17T02:38:00Z" }, {}]);
    expect(footSealed([seal(owner), seal(brain)], brainThread, undefined, later)).toEqual([]);
  });

  test("a line sealed on this device stays until the computer's record of it, and one the thread holds is that row", () => {
    const here: SealedRequest = { key: "prompt:mh-a75e:eea585040c34", verb: "Approved", answer: command, title: command, source: "Mewla Web", brain: true, workerId: "mh-a75e" };
    expect(footSealed([here], brainThread, undefined, Date.now() + 60_000)).toHaveLength(1);
    expect(footSealed([here], brainThread, new Set([here.key]))).toEqual([]);
    expect(footSealed([here], { workerId: "mh-a75e" })).toHaveLength(1);
    expect(footSealed([here], { workerId: "mh-other" })).toEqual([]);
  });

  test("the newest row is read from the thread's last rows", () => {
    expect(newestRowAt([])).toBeUndefined();
    expect(newestRowAt([{ timestamp: "2026-11-17T02:00:00Z" }, { timestamp: "2026-11-17T01:00:00Z" }])).toBe(Date.parse("2026-11-17T02:00:00Z"));
  });
});
