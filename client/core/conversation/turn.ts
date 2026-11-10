import type { Transport } from "../transport";
import type { ChoiceAnswer } from "../timeline/claudeChoiceModel";

/** Stops the agent's running turn (Ctrl-C to the Session); settles when the daemon sent it. */
export async function stopTurn(transport: Transport, workerId: string): Promise<void> {
  await transport.request("send_action", { worker_id: workerId, action: "pause" }, { reply: "action_sent", errors: ["action_failed", "error"], timeoutMs: 15_000 });
}

/**
 * Answers the question Brain's host asked in its conversation (AskUserQuestion).
 * Rejects with `code: "answer_choice_gone"` once the question has passed.
 */
export async function answerChoice(transport: Transport, workerId: string, callId: string, answers: ChoiceAnswer[]): Promise<void> {
  await transport.request(
    "answer_choice",
    { worker_id: workerId, choice_answer: { call_id: callId, answers: answers.map((a) => ({ selected: a.selected, other: a.other })) } },
    { reply: "choice_answered", timeoutMs: 30_000 },
  );
}
