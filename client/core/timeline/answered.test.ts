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
  test("seals the card it answers, with the answer and who and when", () => {
    const parsed = parseAnsweredRequest(record)!;
    expect(answeredKeys(parsed)).toEqual(["prompt:mh-5935:c6cd9885f6ca"]);
    expect(sealedOfAnswered(parsed, () => "15:24")).toEqual({
      key: "prompt:mh-5935:c6cd9885f6ca", verb: "Approved", answer: "Yes", title: record.asked, source: "Proof phone · 15:24", brain: false, workerId: "mh-5935",
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
    expect(sealedOfAnswered(terminal, () => "")).toMatchObject({ verb: "Answered", answer: record.asked, source: "In the terminal" });
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
