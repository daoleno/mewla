/**
 * Brain's own things, for the home header and the tray to host
 * (soft-final/SPEC.md §1.3, §1.4, §1.6). Each takes the current computer's state
 * and its callbacks as props; none assumes where it is mounted.
 */
export { BrainMenu } from "./BrainMenu";
export { currentBrain } from "./source";
export { BrainHostSheet } from "./BrainHostSheet";
export { MentionPanel } from "./MentionPanel";
export { BrainChatFailed, BrainChatUnavailable } from "./BrainStates";
export { createBrainComposer } from "./composer";
export { brainChatState, brainChatUnavailable, brainComposerBlock } from "../../core/brain";
