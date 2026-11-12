import { describe, expect, test } from "bun:test";
import type { Worker } from "../sessions/workers";
import { requestsOf } from "../tray/model";
import { openCardKey, promptParts, requestCardsOf, sealAction, type RequestCardsInput } from "./requestCards";

const NOW = Date.parse("2026-11-12T10:00:00Z");

function worker(id: string, patch: Partial<Worker> = {}): Worker {
  return {
    key: `s1:${id}`, id, serverId: "s1", serverName: "Desk", serverUrl: "ws://x", name: id, status: "blocked",
    cwd: "/home/me/mewla", command: "claude", provider: "claude", summary: "", last_output_lines: [], updated_at: NOW - 60_000, ...patch,
  };
}

const choice = { kind: "ask_user_question", state: "pending", questions: [{ question: "Which branch?", options: [{ label: "main" }, { label: "dev" }] }] } as never;

function cardsInput(patch: Partial<RequestCardsInput> & { workers: Worker[] }): RequestCardsInput {
  const brainHost = patch.brainHost ?? { id: "host" };
  const brainChoice = patch.brainChoice ?? null;
  const requests = requestsOf({
    workers: patch.workers, brainHost, alerts: [], now: NOW,
    brainChoice: brainChoice ? { callId: brainChoice.callId, question: "Which branch?" } : null,
  });
  return { requests, workers: patch.workers, brainHost, brainChoice, transcriptChoice: patch.transcriptChoice };
}

describe("request cards", () => {
  test("Brain's thread: Brain's question, its blocked Session's prompt and a delegated Session's approval, oldest first", () => {
    const input = cardsInput({
      brainChoice: { callId: "c1", choice },
      brainHost: { id: "host", prompt: { id: "hp", kind: "permission", provider: "claude", title: "Allow?", request: "npm test", options: [{ number: 1, label: "Yes" }, { number: 2, label: "No" }] } },
      workers: [
        worker("d", { delegated: true, approvals: [{ id: "a1", tool: "Bash", summary: "git push", options: [{ id: "y", label: "Allow", scope: "once" }, { id: "n", label: "Deny", scope: "deny" }] }] }),
        worker("mine", { prompt: { id: "p", kind: "permission", provider: "claude", title: "Allow?", options: [{ number: 1, label: "Yes" }, { number: 2, label: "No" }] } }),
      ],
    });
    const cards = requestCardsOf(input, { brain: true });
    expect(cards.map((c) => c.key)).toEqual(["brain-choice:c1", "prompt:host:hp", "approval:d:a1"]);
    expect(cards[0]).toMatchObject({ label: "Brain asks", source: "Brain", body: { kind: "choice", route: { via: "choice", workerId: "host", callId: "c1" } } });
    expect(cards[0]!.sourceWorkerId).toBeUndefined();
    expect(cards[1]).toMatchObject({ label: "Brain asks", terminal: true, sourceWorkerId: "host", body: { kind: "action" } });
    expect(cards[2]).toMatchObject({ label: "Approve", sourceWorkerId: "d", body: { kind: "action", workerId: "d" } });
    // A Session the person started shows its request only in that Session.
    expect(requestCardsOf(input, { workerId: "mine" }).map((c) => c.key)).toEqual(["prompt:mine:p"]);
    expect(requestCardsOf(input, { workerId: "d" }).map((c) => c.key)).toEqual(["approval:d:a1"]);
  });

  test("a terminal question, filled from the Session's transcript; without it, the terminal answers", () => {
    const prompt = { id: "q", kind: "choice" as const, provider: "claude", title: "", call_id: "t1", questions: [{ question: "Which branch?", options: [] }] };
    const bare = requestCardsOf(cardsInput({ workers: [worker("s", { prompt })] }), { workerId: "s" });
    expect(bare[0]!.body).toMatchObject({ kind: "attention", workerId: "s" });
    const filled = requestCardsOf(cardsInput({ workers: [worker("s", { prompt })], transcriptChoice: { workerId: "s", callId: "t1", choice } }), { workerId: "s" });
    expect(filled[0]!.body).toMatchObject({ kind: "choice", route: { via: "prompt", workerId: "s", promptId: "q" } });
  });

  test("a folder-trust prompt says so; a blocked Session with no prompt offers the terminal", () => {
    const trust = { id: "t", kind: "startup" as const, provider: "claude", title: "Do you trust the files in this folder?", options: [{ number: 1, label: "Yes, proceed" }, { number: 2, label: "No, exit" }] };
    const cards = requestCardsOf(cardsInput({ workers: [worker("s", { prompt: trust }), worker("x", { needs_attention: true, summary: "Waiting on login" })] }), { workerId: "s" });
    expect(cards[0]!.label).toBe("Trust this folder?");
    const blocked = requestCardsOf(cardsInput({ workers: [worker("x", { needs_attention: true, summary: "Waiting on login" })] }), { workerId: "x" });
    expect(blocked[0]).toMatchObject({ label: "Needs you", body: { kind: "attention", detail: "Waiting on login" } });
  });

  test("the stack opens the oldest, or the one picked while it is open; an approval seals with its command", () => {
    const input = cardsInput({ workers: [
      worker("a", { delegated: true, approvals: [{ id: "1", tool: "Bash", summary: "git push", options: [{ id: "y", label: "Allow", scope: "once" }, { id: "n", label: "Deny", scope: "deny" }] }] }),
      worker("b", { delegated: true, needs_attention: true }),
    ] });
    const cards = requestCardsOf(input, { brain: true });
    expect(openCardKey(cards, null)).toBe("approval:a:1");
    expect(openCardKey(cards, "attention:b")).toBe("attention:b");
    expect(openCardKey(cards, "gone")).toBe("approval:a:1");
    expect(sealAction(cards[0]!, "Allow", false)).toEqual({ verb: "Approved", answer: "git push", mono: true });
    expect(sealAction(cards[0]!, "Deny", true)).toEqual({ verb: "Denied", answer: "git push", mono: true });
  });
});

describe("a terminal prompt's parts", () => {
  test("the command is the line that is not a sentence, wherever Claude prints the description; no approval boilerplate", () => {
    const below = "Fetch HTTP headers from example.edu\ncurl -sI https://example.edu\nThis command requires approval\nDo you want to proceed?";
    expect(promptParts(below)).toEqual({ question: "Do you want to proceed?", command: "curl -sI https://example.edu", notes: "Fetch HTTP headers from example.edu" });
    const above = "git push origin main\nPush the release branch\nDo you want to proceed?";
    expect(promptParts(above)).toEqual({ question: "Do you want to proceed?", command: "git push origin main", notes: "Push the release branch" });
  });
});
