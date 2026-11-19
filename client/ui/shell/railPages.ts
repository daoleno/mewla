import { cleanup, signal, untracked, type ViewRef } from "nsh";
import type { IconName } from "../kit/Icon";

/** One of a page's controls in the rail (client/DESIGN.md "The side rail"): an icon, its English label, its press. */
export type RailAction = {
  id: string;
  icon: IconName;
  label: string;
  onPress: () => void;
  /** Shown only while this holds (Stop while a turn runs, Git only in a repository). */
  shown?: () => boolean;
  /** Stays in place at 40 % and does nothing (a download that can't start). */
  disabled?: () => boolean;
  /** The button, for a menu that sits at it on wide. */
  ref?: (node: ViewRef) => void;
  /** Comes and goes while the page is shown (Stop, a changed file's Refresh): it sits after the steady controls, so
   * appearing never moves one under a finger (`railPageOrder`). */
  transient?: boolean;
};

export type RailPressable = { id: string; label: string; icon?: IconName; onPress: () => void; disabled?: () => boolean; ref?: (node: ViewRef) => void };

/**
 * The pages in the stack, oldest first, each with its rail controls: a page joins as it is made and leaves as it
 * goes, so the last is the one shown. Every `NavPage` joins, one without controls too, so the page under it never
 * lends it its own.
 */
let made = 0;
export const railPages = signal<readonly { id: number; actions: () => readonly RailAction[] }[]>([]);

/** Puts the page being made in the rail's stack with its controls (read when shown, so they follow the page). */
export function joinRail(actions: () => readonly RailAction[] | undefined): void {
  const id = ++made;
  railPages.set([...untracked(railPages), { id, actions: () => actions() ?? [] }]);
  cleanup(() => railPages.set(untracked(railPages).filter((page) => page.id !== id)));
}

