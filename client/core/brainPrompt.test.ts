import { describe, expect, test } from "bun:test";
import { brainHostActionPrompt } from "./brainPrompt";

const prompt = { id: "p-1", kind: "permission", provider: "claude", title: "Run this command?", request: "rm -rf build", options: [{ number: 1, label: "Yes", selected: true }, { number: 2, label: "No" }] };
const snapshot = { chat_thread_id: "t1", host_worker: { id: "brain-host", name: "Brain", status: "blocked", process_id: 42, started_at: 1_700_000_000_000, prompt } };

describe("the prompt Brain's own Session is blocked on (soft-final/SPEC.md §1.1)", () => {
  test("is a card with the prompt's options, scoped to this run of the host", () => {
    const card = brainHostActionPrompt({ snapshot, connected: true, gone: new Set(), questionOpen: false });
    expect(card?.promptId).toBe("p-1");
    expect(card?.options.map((option) => option.number)).toEqual([1, 2]);
  });
  test("none while disconnected, once the computer said it passed, or while the transcript's question shows", () => {
    expect(brainHostActionPrompt({ snapshot, connected: false, gone: new Set(), questionOpen: false })).toBeNull();
    expect(brainHostActionPrompt({ snapshot, connected: true, gone: new Set(["p-1"]), questionOpen: false })).toBeNull();
    expect(brainHostActionPrompt({ snapshot, connected: true, gone: new Set(), questionOpen: true })).toBeNull();
    expect(brainHostActionPrompt({ snapshot: { ...snapshot, host_worker: { ...snapshot.host_worker, prompt: undefined } }, connected: true, gone: new Set(), questionOpen: false })).toBeNull();
  });
});
