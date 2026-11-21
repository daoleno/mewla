import type { BrainExecutorRef, BrainExecutorState } from "../brain";
import { commandBinary } from "./agentCommands";

/**
 * Whether the computer has the program of a launch command, as Brain's snapshot says it
 * (the executors it runs agents with and which of them it found: what the Brain host picker
 * reads): false only when the snapshot lists an executor that runs this program and none of
 * them was found. A shell (no command), a program no executor runs, and a snapshot that has
 * not arrived or says nothing of the executors are all "unknown", which the computer
 * answers itself at the launch: a stale snapshot never stops one.
 */
export function agentInstalled(
  command: string,
  executors: readonly BrainExecutorRef[] | undefined,
  states: Readonly<Record<string, BrainExecutorState>> | undefined,
): boolean {
  const program = commandBinary(command);
  if (!program || !states) return true;
  const runners = (executors ?? []).filter((executor) => commandBinary(executor.command) === program || (!executor.command && executor.id.toLowerCase() === program));
  if (runners.length === 0) return true;
  return runners.some((executor) => states[executor.id]?.installed);
}

/** The line that says some agents are not here and where to get them (the Brain host picker's). */
export const INSTALL_MORE_LINE = "Install more with mewla deps";
