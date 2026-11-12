/**
 * Brain's own things, for the home header and the tray to host
 * (soft-final/SPEC.md §1.6). Each takes the current computer's state
 * (`currentBrain` by default) and its callbacks as props; none assumes where it is mounted.
 */
export { createBrainHostSwitch, type BrainHostSwitch } from "./hostSwitch";
export { BrainMenu, NEW_CHAT_FAILED } from "./BrainMenu";
export { currentBrain, type BrainSource } from "./source";
export { BrainHostPanel, BrainHostSheet } from "./BrainHostSheet";
