import { createContext, useContext } from "react";

/**
 * What ↑ and Esc do in a chat composer beyond the box itself. The chat that
 * owns the conversation provides them; the input reads them, so the keys
 * need no props through every layer of the composer.
 */
export interface ComposerKeyActions {
  /** ↑ in an empty box: edit the last message. False when it cannot be edited. */
  editLast?(): boolean;
  /** Esc: leave an edit in progress. False when there is none. */
  cancelEdit?(): boolean;
}

export const ComposerKeyActionsContext = createContext<ComposerKeyActions>({});

export function useComposerKeyActions(): ComposerKeyActions {
  return useContext(ComposerKeyActionsContext);
}
