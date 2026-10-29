import { describe, expect, test } from "bun:test";
import type { WorkerApproval, WorkerPrompt } from "../../store/workers";
import {
  hasActionableCard,
  resolveBlockedTerminal,
  type BlockedTerminalInput,
} from "./blockedTerminalSurface";

const permission: WorkerPrompt = {
  id: "perm-1",
  kind: "permission",
  provider: "claude",
  title: "Bash command",
  options: [
    { number: 1, label: "Yes", selected: true },
    { number: 2, label: "No" },
  ],
};

const choice: WorkerPrompt = {
  id: "choice-1",
  kind: "choice",
  provider: "claude",
  title: "Waiting for your choice",
  call_id: "call-1",
  questions: [{ question: "Which?", options: [{ number: 1, label: "A" }, { number: 2, label: "B" }] }],
};

const hollowChoice: WorkerPrompt = {
  ...choice,
  id: "choice-2",
  questions: [{ question: "Which?", options: [] }],
};

const emptyMenu: WorkerPrompt = { ...permission, id: "perm-empty", options: [] };

const approval: WorkerApproval = {
  id: "approval-1",
  tool: "Bash",
  summary: "touch /tmp/x",
  options: [{ id: "allow", label: "Allow", scope: "once" }, { id: "deny", label: "Deny", scope: "deny" }],
} as WorkerApproval;

function input(overrides: Partial<BlockedTerminalInput> = {}): BlockedTerminalInput {
  return { status: "blocked", live: true, identity: "w1|10|20", dismissedKey: null, ...overrides };
}

describe("resolveBlockedTerminal", () => {
  test("blocked with no prompt shows the live terminal", () => {
    expect(resolveBlockedTerminal(input())).toEqual({
      forceTerminal: true,
      blockKey: "w1|10|20|",
      dismissedKey: null,
    });
  });

  test("blocked on a prompt the app cannot drive shows the live terminal", () => {
    expect(resolveBlockedTerminal(input({ prompt: emptyMenu })).forceTerminal).toBe(true);
    expect(resolveBlockedTerminal(input({ prompt: hollowChoice })).forceTerminal).toBe(true);
  });

  test("blocked with a drivable prompt or an agent decision keeps the chat and its card", () => {
    for (const card of [{ prompt: permission }, { prompt: choice }, { approvals: [approval] }]) {
      expect(resolveBlockedTerminal(input(card))).toEqual({
        forceTerminal: false,
        blockKey: null,
        dismissedKey: null,
      });
    }
    expect(hasActionableCard({ prompt: permission })).toBe(true);
    expect(hasActionableCard({})).toBe(false);
  });

  test("a Session that is not blocked is unchanged", () => {
    for (const status of ["running", "done", "failed", "unknown", undefined] as const) {
      expect(resolveBlockedTerminal(input({ status })).forceTerminal).toBe(false);
    }
  });

  test("does not force again once the person chose the chat, until the block changes or clears", () => {
    const first = resolveBlockedTerminal(input());
    const dismissedKey = first.blockKey;
    // The person went back to the chat: the same block stays in the chat.
    expect(resolveBlockedTerminal(input({ dismissedKey }))).toEqual({
      forceTerminal: false,
      blockKey: dismissedKey,
      dismissedKey,
    });
    // A different undrivable prompt is a new block.
    expect(resolveBlockedTerminal(input({ prompt: emptyMenu, dismissedKey })).forceTerminal).toBe(true);
    // Another run of the Session is a new block.
    expect(resolveBlockedTerminal(input({ identity: "w1|11|21", dismissedKey })).forceTerminal).toBe(true);
    // The block clears: the dismissal is forgotten, so the next block forces again.
    const cleared = resolveBlockedTerminal(input({ status: "running", dismissedKey }));
    expect(cleared.dismissedKey).toBeNull();
    expect(resolveBlockedTerminal(input({ dismissedKey: cleared.dismissedKey })).forceTerminal).toBe(true);
    // A card appearing ends the block too.
    expect(resolveBlockedTerminal(input({ prompt: permission, dismissedKey })).dismissedKey).toBeNull();
  });

  test("a retained snapshot from before a reconnect does not move the view or forget a dismissal", () => {
    const dismissedKey = "w1|10|20|";
    expect(resolveBlockedTerminal(input({ live: false }))).toEqual({
      forceTerminal: false,
      blockKey: null,
      dismissedKey: null,
    });
    expect(resolveBlockedTerminal(input({ live: false, dismissedKey })).dismissedKey).toBe(dismissedKey);
    expect(resolveBlockedTerminal(input({ live: true, dismissedKey })).forceTerminal).toBe(false);
  });
});
