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

/**
 * Answers the prompt open in a Session's terminal: a menu by its number, or a
 * question (AskUserQuestion read off the screen) by its answers. Rejects with
 * `code: "answer_prompt_gone"` once the prompt has passed.
 */
export async function answerPrompt(transport: Transport, workerId: string, promptId: string, answer: { number?: number; answers?: ChoiceAnswer[] }): Promise<void> {
  await transport.request(
    "answer_prompt",
    {
      worker_id: workerId,
      prompt_answer: {
        prompt_id: promptId,
        ...(answer.number !== undefined ? { number: answer.number } : {}),
        ...(answer.answers ? { answers: answer.answers.map((a) => ({ selected: a.selected, other: a.other })) } : {}),
      },
    },
    { reply: "prompt_answered", timeoutMs: 30_000 },
  );
}

/** Answers a decision the agent reported itself (its approval request) with one of its options. */
export async function decideApproval(transport: Transport, workerId: string, approvalId: string, optionId: string): Promise<void> {
  await transport.request("approval_decision", { worker_id: workerId, id: approvalId, option_id: optionId }, { reply: "approval_decided", timeoutMs: 8_000 });
}

/**
 * Steps a Session's agent back to before one of the person's messages (its CLI
 * must advertise `message_rewind`). `composerClean` false: it stepped back,
 * but its input box still holds the old prompt.
 */
export async function rewindMessage(transport: Transport, workerId: string, eventId: string): Promise<{ composerClean: boolean }> {
  const reply = await transport.request("rewind_message", { worker_id: workerId, message_rewind: { event_id: eventId } }, { reply: "message_rewound", timeoutMs: 30_000 });
  return { composerClean: reply.composer_clean !== false };
}
