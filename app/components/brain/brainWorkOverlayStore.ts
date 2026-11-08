import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import type { BrainObjective } from "../../store/brain";
import type { BrainWorkActionsFor } from "./BrainWorkPanel";
import type { BrainWorkSlip, BrainWorkSurface } from "./brainWorkSurface";

/**
 * What the Brain screen hands the phone's Work sheet. The sheet itself is
 * mounted once at the root (BrainWorkOverlayHost), over the tab bar and
 * header, and stays mounted with its slips drawn while it is shut, so opening
 * it commits nothing: the press only moves a shared value on the UI thread.
 * This store is the seam between the two: the screen publishes the Work, the
 * host draws it, and open/close reach the host without a render.
 */
export interface BrainWorkOverlayContent {
  surface: BrainWorkSurface;
  objective?: BrainObjective;
  chrome: TerminalThemeChrome;
  onOpenSlip(slip: BrainWorkSlip): void;
  actionsFor?: BrainWorkActionsFor;
  /** Changes when what the slips draw from `actionsFor` changes; the function itself stays one. */
  refresh?: string;
}

/** What the host gives the store: the animation, started from any thread's caller. */
export interface BrainWorkOverlayMotion {
  show(): void;
  hide(): void;
}

type Listener = () => void;

const contentListeners = new Set<Listener>();
const openListeners = new Set<Listener>();
let content: BrainWorkOverlayContent | null = null;
let open = false;
let motion: BrainWorkOverlayMotion | null = null;

function notify(listeners: Set<Listener>) {
  for (const listener of [...listeners]) listener();
}

export const brainWorkOverlay = {
  /** The Brain screen's Work, or null when there is no sheet to open (the wide column shows, or Brain is gone). */
  publish(next: BrainWorkOverlayContent | null) {
    if (next === content) return;
    content = next;
    if (!next && open) {
      open = false;
      motion?.hide();
      notify(openListeners);
    }
    notify(contentListeners);
  },
  /**
   * Opens the sheet. The shared value moves first; the open flag (back button,
   * screen-reader focus, the rest of the app hidden from it) follows and only
   * reaches the few components that read it.
   */
  open() {
    if (!content || open) return;
    motion?.show();
    open = true;
    notify(openListeners);
  },
  close() {
    if (!open) return;
    motion?.hide();
    open = false;
    notify(openListeners);
  },
  /** The host's own motion, while it is mounted. */
  attach(next: BrainWorkOverlayMotion): () => void {
    motion = next;
    return () => {
      if (motion === next) motion = null;
    };
  },
  getContent: (): BrainWorkOverlayContent | null => content,
  getOpen: (): boolean => open,
  subscribeContent(listener: Listener): () => void {
    contentListeners.add(listener);
    return () => contentListeners.delete(listener);
  },
  subscribeOpen(listener: Listener): () => void {
    openListeners.add(listener);
    return () => openListeners.delete(listener);
  },
  /** A drag-to-dismiss already moved the sheet itself; only the flag follows. */
  settleClosed() {
    if (!open) return;
    open = false;
    notify(openListeners);
  },
};
