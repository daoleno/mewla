import type { WorkerStatus } from "../../constants/tokens";
import type { WorkerApproval, WorkerPrompt } from "../../store/workers";
import { choiceFromPrompt } from "./claudeChoiceModel";
import { buildTerminalActionPrompt } from "./TerminalActionPromptModel";

export type BlockedTerminalInput = {
  status?: WorkerStatus;
  /** The blocking prompt the daemon read from the terminal. */
  prompt?: WorkerPrompt;
  /** Decisions the agent reported itself. */
  approvals?: WorkerApproval[];
  /**
   * The daemon connection is up and the Session list was received on it. A
   * retained pre-disconnect `blocked` must not pull the view anywhere.
   */
  live: boolean;
  /** Names this run of the Session: worker id, process id and start time. */
  identity: string;
  /** The block the person already left the terminal for, while it persists. */
  dismissedKey: string | null;
};

export type BlockedTerminalDecision = {
  /** Show the live terminal instead of the chat. */
  forceTerminal: boolean;
  /** The block being shown; null when there is none. */
  blockKey: string | null;
  /** What to remember as dismissed after this decision. */
  dismissedKey: string | null;
};

/**
 * Whether a prompt card could be drawn for this Session: a decision the agent
 * reported, a menu the daemon read, or a choice with every question's options.
 * A card is fully tap-interactive or not shown, so this is the same test the
 * Interface builds its cards with.
 */
export function hasActionableCard({
  prompt,
  approvals,
}: Pick<BlockedTerminalInput, "prompt" | "approvals">): boolean {
  return (
    buildTerminalActionPrompt({ prompt, approvals }) !== null ||
    choiceFromPrompt(prompt) !== null
  );
}

/**
 * A Session that is blocked on something the Interface has no card for shows
 * its live terminal, where the person can act on it. The decision is per
 * block: when the person goes back to the chat while the same block persists,
 * it is not forced again. It starts over when the block clears or a card
 * appears (an answered prompt followed by an undrivable one is a new block).
 */
export function resolveBlockedTerminal(input: BlockedTerminalInput): BlockedTerminalDecision {
  if (!input.live) {
    return { forceTerminal: false, blockKey: null, dismissedKey: input.dismissedKey };
  }
  if (input.status !== "blocked" || hasActionableCard(input)) {
    return { forceTerminal: false, blockKey: null, dismissedKey: null };
  }
  const blockKey = `${input.identity}|${input.prompt?.id ?? ""}`;
  return {
    forceTerminal: input.dismissedKey !== blockKey,
    blockKey,
    dismissedKey: input.dismissedKey,
  };
}
