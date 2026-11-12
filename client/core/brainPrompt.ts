/**
 * The prompt Brain's own Session is blocked on in its terminal (a menu or an
 * approval the daemon read off it, `brain_snapshot.host_worker.prompt`), and its
 * answer. A request card in Brain's thread shows it (soft-final/SPEC.md §1.1);
 * requests are never docked above the composer.
 */
import type { BrainSnapshot } from "./brain";
import { answerPrompt, decideApproval } from "./conversation/turn";
import { buildTerminalActionPrompt, type TerminalActionPrompt, type TerminalActionPromptOption } from "./session/terminalActionPrompt";
import { normalizePrompt } from "./sessions/workers";
import type { Transport } from "./transport";

/**
 * The card for the host's prompt, or null: none while disconnected, once the
 * computer said it passed (`gone`), or while a question from the transcript is
 * showing (that one is the choice card's).
 */
export function brainHostActionPrompt(input: {
  snapshot: BrainSnapshot | null | undefined;
  connected: boolean;
  gone: ReadonlySet<string>;
  questionOpen: boolean;
}): TerminalActionPrompt | null {
  if (!input.connected || input.questionOpen) return null;
  const host = input.snapshot?.host_worker;
  const prompt = normalizePrompt(host?.prompt);
  if (!host?.id || !prompt || input.gone.has(prompt.id)) return null;
  return buildTerminalActionPrompt({ prompt, scopeKey: `${host.id}|${host.process_id ?? ""}|${host.started_at ?? ""}` });
}

/** Answers it on the host: an approval through its decision, a menu by its number. Rejects as the computer refused. */
export async function answerBrainHostPrompt(transport: Transport, hostId: string, prompt: TerminalActionPrompt, option: TerminalActionPromptOption): Promise<void> {
  if (option.approvalOptionId && prompt.approvalId) await decideApproval(transport, hostId, prompt.approvalId, option.approvalOptionId);
  else if (option.number !== undefined && prompt.promptId) await answerPrompt(transport, hostId, prompt.promptId, { number: option.number });
}
