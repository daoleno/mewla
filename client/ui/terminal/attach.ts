import { cleanup, signal } from "nsh";
import { createAttachmentUploads, type AttachmentUploads } from "../more/files/upload";

/**
 * A terminal's Attach (SPEC §16.4; on the Mac DESIGN-MAC.md §2.3.1): picked files go to the computer, then their
 * paths are typed at the prompt (TerminalPane). The phone's key bar and Mewla for Mac's header action share one.
 */
export type TerminalAttach = {
  uploads: AttachmentUploads;
  /** What a pick that could not start says (not connected, too many); "" once the next one starts. */
  problem: () => string;
  dismissProblem: () => void;
  attach: () => void;
};

/** The newest terminal Attach on screen, for Mewla Dev's proof script (mac.ts `attach`). */
let newest: TerminalAttach | null = null;
export function newestTerminalAttach(): TerminalAttach | null {
  return newest;
}

/** Made inside the component that owns it: it goes with it. */
export function createTerminalAttach(): TerminalAttach {
  const uploads = createAttachmentUploads();
  const problem = signal("");
  const made: TerminalAttach = {
    uploads,
    problem: () => problem(),
    dismissProblem: () => problem.set(""),
    attach: () => {
      problem.set("");
      void uploads.pickAndUpload().catch((error) => problem.set(error instanceof Error ? error.message : "Could not attach the file."));
    },
  };
  newest = made;
  cleanup(() => {
    uploads.dispose();
    if (newest === made) newest = null;
  });
  return made;
}
