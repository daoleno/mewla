/**
 * Typing `@` in Brain's composer lists the executors Brain can hand a task to
 * (the Expo app's BrainExecutorMentionPicker and activeExecutorMentionAtEnd in
 * app/app/(primary)/index.tsx). A pick puts `@<id> ` in place of the `@word`.
 */
import type { BrainExecutorRef } from "./brain";
import { brainExecutorLabel, brainProviderLabel } from "./brainMenu";

export type ExecutorMention = { start: number; query: string };

/** The `@word` being typed at the end of the draft: an `@` at the start or after a space, then id characters. */
export function activeExecutorMentionAtEnd(value: string): ExecutorMention | null {
  for (let cursor = value.length - 1; cursor >= 0; cursor -= 1) {
    const char = value[cursor];
    if (char === "@") {
      return cursor === 0 || /\s/.test(value[cursor - 1]) ? { start: cursor, query: value.slice(cursor + 1) } : null;
    }
    if (!/[a-z0-9_.-]/i.test(char)) return null;
  }
  return null;
}

/** The executors a mention offers: those whose id, name or provider starts with the query. */
export function executorMentionChoices(executors: readonly BrainExecutorRef[], query: string): BrainExecutorRef[] {
  const needle = query.trim().toLowerCase();
  const candidates = executors.filter((executor) => executor.id.trim());
  if (!needle) return candidates;
  return candidates.filter((executor) =>
    executor.id.toLowerCase().startsWith(needle)
    || brainExecutorLabel(executor).toLowerCase().startsWith(needle)
    || brainProviderLabel(executor.provider).toLowerCase().startsWith(needle));
}

/** The draft with the `@word` replaced by the picked executor's handle and a space. */
export function insertExecutorMention(draft: string, mention: ExecutorMention, executorId: string): string {
  return `${draft.slice(0, mention.start)}@${executorId} `;
}
