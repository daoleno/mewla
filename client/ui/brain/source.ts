import type { BrainSnapshot } from "../../core/brain";
import type { Chat } from "../../core/conversation/chat";
import type { Daemon } from "../../core/more/daemon";
import { brain, brainChat, brainThread, connection, current, currentTransport, openBrainThread } from "../session";
import { createBrainHostSwitch, type BrainHostSwitch } from "./hostSwitch";

/**
 * What Brain's components read of the current computer. The app's is `currentBrain`
 * (the one current server: its snapshot, connection and Brain chat); a test gives
 * its own.
 */
export type BrainSource = {
  snapshot: () => BrainSnapshot | null;
  connected: () => boolean;
  transport: () => Daemon | null;
  /** Brain's open chat (its current thread on its host), when there is one. */
  chat: () => Chat | null;
  /** Brain's current thread has loaded (New chat waits for it). */
  threadLoaded: () => boolean;
  /** The one host switch, shared by the menu's "Brain host" page and the can't-start notice. */
  hostSwitch: BrainHostSwitch;
  /** After a new chat: Brain's chat shows its current thread (not an earlier one a notification opened). */
  showCurrentThread?: () => void;
};

/** The current computer's Brain; a switch of computer forgets a host switch in progress. */
export const currentBrain: BrainSource = {
  snapshot: brain,
  connected: () => connection() === "connected",
  transport: currentTransport,
  chat: brainChat,
  threadLoaded: () => Boolean(brainChat()) && !brainThread().loading,
  hostSwitch: createBrainHostSwitch({ snapshot: brain, transport: currentTransport, serverId: () => current()?.id ?? "" }),
  showCurrentThread: () => openBrainThread(null),
};
