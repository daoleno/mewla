import { describe, expect, test } from "bun:test";
import { answeredKeys, parseAnsweredRequest, sealedOfAnswered } from "./answered";

// WIRE §2's real record: a Claude permission answered from a phone.
const record = {
  id: "prompt:mh-5935:c6cd9885f6ca", kind: "permission", session_id: "mh-5935", title: "Waiting for Claude permission",
  asked: "Bash command git push origin main Push the release branch Do you want to proceed?", answer: "Yes", outcome: "approved",
  by: { kind: "device", device_id: "proof-d5eb", device_name: "Proof phone" },
  asked_at: "2026-11-12T07:24:26.390353131Z", answered_at: "2026-11-12T07:24:49.945041535Z", anchor: { prompt_id: "c6cd9885f6ca" },
};

describe("an answered request", () => {
  test("an approval names its command in mono, not the button it was answered with", () => {
    const approval = parseAnsweredRequest({ ...record, kind: "approval", asked: "git push origin chore/node-22", answer: "Allow" })!;
    expect(sealedOfAnswered(approval, () => "")).toMatchObject({ verb: "Approved", answer: "git push origin chore/node-22", mono: true });
  });

  test("a request that passed unanswered shows its command in mono, and prose as prose", () => {
    const passed = parseAnsweredRequest({ ...record, kind: "approval", asked: "curl -sI https://example.edu", answer: "", outcome: "not_answered" })!;
    expect(sealedOfAnswered(passed, () => "")).toMatchObject({ verb: "Not answered", answer: "curl -sI https://example.edu", mono: true });
    const question = parseAnsweredRequest({ ...record, kind: "question", asked: "Which name do we ship?", answer: "", outcome: "not_answered" })!;
    expect(sealedOfAnswered(question, () => "")).toMatchObject({ verb: "Not answered", mono: false });
  });

  test("seals the card it answers, with the answer and who and when", () => {
    const parsed = parseAnsweredRequest(record)!;
    expect(answeredKeys(parsed)).toEqual(["prompt:mh-5935:c6cd9885f6ca"]);
    expect(sealedOfAnswered(parsed, () => "15:24")).toEqual({
      key: "prompt:mh-5935:c6cd9885f6ca", verb: "You chose", answer: "Yes", mono: false, title: record.asked, source: "Proof phone · 15:24", brain: false, workerId: "mh-5935",
    });
  });

  test("an approval, Brain's choice and a Work question have their own keys; Brain's thread shows Work and Brain records", () => {
    const approval = parseAnsweredRequest({ ...record, id: "approval:x", kind: "approval", anchor: { approval_id: "a1" }, work_id: "w1", outcome: "denied", answer: "Deny" })!;
    expect(answeredKeys(approval)).toEqual(["approval:mh-5935:a1"]);
    expect(sealedOfAnswered(approval, () => "")).toMatchObject({ verb: "Denied", brain: true });
    const choice = parseAnsweredRequest({ ...record, kind: "choice", session_id: undefined, thread_id: "t1", anchor: { call_id: "c9" }, by: { kind: "brain", basis: "ship it" } })!;
    expect(answeredKeys(choice)).toEqual(["brain-choice:c9"]);
    expect(sealedOfAnswered(choice, () => "9:00").source).toBe("Brain · 9:00");
    const work = parseAnsweredRequest({ ...record, kind: "work_question", session_id: undefined, work_id: "w2", anchor: undefined })!;
    expect(answeredKeys(work)).toEqual(["work:w2"]);
  });

  test("answered in the terminal reads so; a record without what it needs is none", () => {
    const terminal = parseAnsweredRequest({ ...record, answer: undefined, outcome: "answered", by: { kind: "terminal" } })!;
    expect(sealedOfAnswered(terminal, () => "")).toMatchObject({ verb: "You chose", answer: record.title, source: "In the terminal" });
    // Main 5cd3ec63's record of a Claude permission answered in the terminal: the screen run together, no answer.
    const screen = parseAnsweredRequest({ ...record, title: "Bash command", answer: undefined, outcome: "answered", by: { kind: "terminal" },
      asked: 'Tip: auto mode handles these prompts for you — choose "switch to auto mode" below Fetch HTTP headers from example.org curl -sI https://example.org This command requires approval Do you want to proceed?' })!;
    expect(sealedOfAnswered(screen, () => "")).toMatchObject({ answer: "Bash command", mono: false });
    const question = parseAnsweredRequest({ ...record, kind: "question", answer: undefined, outcome: "answered" })!;
    expect(sealedOfAnswered(question, () => "").answer).toBe(record.asked);
    expect(parseAnsweredRequest({ ...record, outcome: "maybe" })).toBeNull();
    expect(parseAnsweredRequest({ ...record, by: {} })).toBeNull();
    expect(parseAnsweredRequest(null)).toBeNull();
  });
});

describe("an answered row in a thread", () => {
  test("keeps its record when the snapshot is normalized again", async () => {
    const { normalizeCodexConversation } = await import("../conversation/conversation");
    const row = { id: "answered:" + record.id, seq: 2, timestamp: record.asked_at, kind: "request_answered", source: "request_answered", role: "system", body: "Yes", status: "approved", request: record };
    const once = normalizeCodexConversation({ available: true, events: [row] });
    const twice = normalizeCodexConversation(once);
    expect(twice.events[0]).toMatchObject({ kind: "status", source: "request_answered", answered: { id: record.id, outcome: "approved" } });
  });
});

describe("a record that names its call (WIRE §8)", () => {
  const named = { ...record, title: "Bash command", tool: "Bash", summary: "curl -sI https://example.org", asked: "curl -sI https://example.org" };

  test("a permission answered in the terminal says how it ended and the command, in mono", () => {
    const approved = parseAnsweredRequest({ ...named, answer: undefined, by: { kind: "terminal" } })!;
    expect(sealedOfAnswered(approved, () => "")).toMatchObject({ verb: "Approved", answer: "curl -sI https://example.org", mono: true, source: "In the terminal" });
    const denied = parseAnsweredRequest({ ...named, answer: undefined, outcome: "denied", by: { kind: "terminal" } })!;
    expect(sealedOfAnswered(denied, () => "")).toMatchObject({ verb: "Denied", answer: "curl -sI https://example.org", mono: true });
  });

  test("answered from a phone, it names the command too, not the button", () => {
    expect(sealedOfAnswered(parseAnsweredRequest(named)!, () => "")).toMatchObject({ verb: "Approved", answer: "curl -sI https://example.org", mono: true });
  });

  test("a terminal answer not yet ruled, and an old record without the fields, keep today's words", () => {
    const pending = parseAnsweredRequest({ ...named, answer: undefined, outcome: "answered", by: { kind: "terminal" } })!;
    expect(sealedOfAnswered(pending, () => "")).toMatchObject({ verb: "You chose", answer: "Bash command" });
    expect(sealedOfAnswered(parseAnsweredRequest(record)!, () => "")).toMatchObject({ verb: "You chose", answer: "Yes" });
  });
});

describe("a record written again under its id (WIRE §8)", () => {
  test("is the newer version of the same line: the thread keeps one, now ruled", async () => {
    const { normalizeCodexConversation } = await import("../conversation/conversation");
    const { buildTimeline } = await import("./model");
    const first = { ...record, answer: undefined, outcome: "answered", by: { kind: "terminal" }, summary: "curl -sI https://example.org", tool: "Bash" };
    const row = (seq: number, request: object) => ({ id: `answered:${seq}`, seq, timestamp: record.answered_at, kind: "request_answered", source: "request_answered", role: "system", body: "", request });
    const conversation = normalizeCodexConversation({ available: true, events: [row(2, first), row(3, { ...first, outcome: "approved" })] });
    const answered = buildTimeline(conversation.events).filter((item) => item.type === "answered");
    expect(answered).toHaveLength(1);
    expect(answered[0]).toMatchObject({ id: "answered:2", record: { outcome: "approved" } });
  });
});
