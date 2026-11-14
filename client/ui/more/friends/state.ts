import { effect, openUrl, signal, untracked, lifecycle } from "nsh";
import type { FriendsStatus } from "../../../core/friends/api";
import {
  deleteFriendsAccount,
  disableFriends,
  enableFriends,
  getFriendsStatus,
  pollComputerSignIn,
  setAutoMedia,
  setAutoPost,
  startComputerSignIn,
  type LoginStart,
} from "../../../core/friends/requests";
import { signInComputer } from "../../../core/friends/signIn";
import type { StoredServer } from "../../../core/storedServerContract";
import { daemon } from "../daemon";
import { pet } from "../../pets/pets";

/**
 * Friends as the current computer has it. The computer is the only truth: it holds
 * the sign-in, the drafts and the link to the person, so a switch drops what was
 * known and reads the next computer.
 */
export type FriendsPhase = "off" | "signing_in" | "working" | "on";

interface FriendsModel {
  serverId: string;
  /** What the computer says; null until it answered. */
  status: FriendsStatus | null;
  /** The code to enter while the computer signs in to its Mewla account. */
  signIn: LoginStart | null;
  working: boolean;
  error: string | null;
}

const EMPTY: FriendsModel = { serverId: "", status: null, signIn: null, working: false, error: null };

export const friends = signal<FriendsModel>(EMPTY);

/** Bumped by a switch, a cancel or a new turn-on: an older loop stops. */
let token = 0;

function server(): StoredServer | null {
  return daemon()?.server ?? null;
}

function patch(serverId: string, next: Partial<FriendsModel>): void {
  if (friends().serverId === serverId) friends.set({ ...friends(), ...next });
}

export function phase(): FriendsPhase {
  const m = friends();
  return m.signIn ? "signing_in" : m.working ? "working" : m.status?.enabled ? "on" : "off";
}

export const friendsEnabled = () => friends().status?.enabled === true;
export const draftCount = () => friends().status?.drafts.length ?? 0;
export const unread = () => friends().status?.me?.unread ?? 0;
export const available = () => daemon() !== null;

let inFlight: Promise<void> | null = null;

/** Reads the computer again. A computer that does not answer is not "off": the old status stays. */
export function refreshFriends(): Promise<void> {
  const target = server();
  if (!target) return Promise.resolve();
  if (inFlight) return inFlight;
  inFlight = getFriendsStatus(target)
    .then((status) => patch(target.id, { status }))
    .catch(() => {})
    .finally(() => {
      inFlight = null;
    });
  return inFlight;
}

function adopt(next: StoredServer | null): void {
  const id = next?.id ?? "";
  if (friends().serverId === id) return;
  token++;
  inFlight = null;
  friends.set({ ...EMPTY, serverId: id });
  if (next) void refreshFriends();
}

/**
 * A page that shows Friends follows the current computer and reads it again when the
 * app comes back to the front.
 */
export function followFriends(): void {
  effect(() => {
    const next = daemon()?.server ?? null;
    untracked(() => adopt(next));
  });
  effect(() => {
    if (lifecycle() === "active") untracked(() => void refreshFriends());
  });
}

function deviceTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "";
  } catch {
    return "";
  }
}

function message(reason: unknown, fallback: string): string {
  return reason instanceof Error && reason.message ? reason.message : fallback;
}

/** On: signs the computer in first when it is not, then creates the Friends person named after the pet. */
export async function turnOn(): Promise<void> {
  const target = server();
  if (!target) {
    friends.set({ ...friends(), error: "Connect a computer first" });
    return;
  }
  const mine = ++token;
  const live = () => token === mine && friends().serverId === target.id;
  patch(target.id, { error: null, working: true });
  try {
    const status = await getFriendsStatus(target);
    if (!live()) return;
    patch(target.id, { status });
    if (!status.signed_in) {
      const signedIn = await signInComputer({
        start: () => startComputerSignIn(target),
        poll: (id) => pollComputerSignIn(target, id),
        now: () => Date.now(),
        wait: (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
        show: (start) => live() && patch(target.id, { signIn: start }),
        current: live,
      });
      if (!signedIn) return;
    }
    const chosen = pet();
    const next = await enableFriends(target, chosen.name, chosen.id, deviceTimeZone());
    if (live()) patch(target.id, { status: next });
  } catch (reason) {
    if (live()) patch(target.id, { error: message(reason, "Friends did not turn on") });
  } finally {
    if (live()) patch(target.id, { signIn: null, working: false });
  }
}

/** Off: the switch flips at once and flips back if the computer cannot be told. The account, posts and drafts stay. */
export async function turnOff(): Promise<void> {
  const target = server();
  if (!target) return;
  const before = friends().status;
  patch(target.id, { error: null, status: before ? { ...before, enabled: false, me: undefined, drafts: [] } : before });
  try {
    patch(target.id, { status: await disableFriends(target) });
  } catch (reason) {
    patch(target.id, { status: before, error: message(reason, "Friends did not turn off") });
  }
}

export function cancelSignIn(): void {
  token++;
  const id = friends().serverId;
  patch(id, { signIn: null, working: false });
}

export function openSignInPage(): void {
  const start = friends().signIn;
  const url = start?.verification_uri_complete || start?.verification_uri;
  if (url) void openUrl(url).catch(() => {});
}

/** Deletes the Friends account, what it made and the computer's drafts; true when done. */
export async function deleteAccount(): Promise<boolean> {
  const target = server();
  if (!target) return false;
  patch(target.id, { error: null });
  try {
    patch(target.id, { status: await deleteFriendsAccount(target) });
    return true;
  } catch (reason) {
    patch(target.id, { error: message(reason, "Friends account was not deleted") });
    return false;
  }
}

export async function changeAutoPost(enabled: boolean): Promise<void> {
  const target = server();
  if (!target) return;
  patch(target.id, { error: null });
  try {
    patch(target.id, { status: await setAutoPost(target, enabled, deviceTimeZone()) });
  } catch (reason) {
    patch(target.id, { error: message(reason, "Could not change auto-post") });
  }
}

export async function changeAutoMedia(media: boolean): Promise<void> {
  const target = server();
  if (!target) return;
  patch(target.id, { error: null });
  try {
    patch(target.id, { status: await setAutoMedia(target, media, deviceTimeZone()) });
  } catch (reason) {
    patch(target.id, { error: message(reason, "Could not change what auto-post may attach") });
  }
}

/** A newer status (after a draft was posted or discarded) replaces what was read. */
export function setStatus(status: FriendsStatus): void {
  patch(friends().serverId, { status });
}

/** The current computer, for a page's own requests. */
export function friendsServer(): StoredServer | null {
  return server();
}

/** The current computer's id (reactive); "" with none. */
export function currentServerId(): string {
  return daemon()?.server.id ?? "";
}

/**
 * The computer to send an action to about what a page shows from computer `shownId`:
 * null once the current computer is another one, so a like, a comment, a follow or a
 * block never goes to a computer other than the one whose posts and people are shown.
 */
export function serverFor(shownId: string): StoredServer | null {
  const target = server();
  return target && target.id === shownId ? target : null;
}

/**
 * A Friends page follows the current computer and its place in the stack: `load`
 * runs when it opens, again when it comes back to the top of its stack (Back from the
 * page above) or the app comes back to the front, and after `clear` when the current
 * computer changes, so what the last computer said never stays on screen.
 */
export function followPage(options: { top: () => boolean; clear: () => void; load: () => void }): void {
  let shownId: string | null = null;
  let shown = false;
  effect(() => {
    const id = currentServerId();
    const visible = options.top() && lifecycle() === "active";
    untracked(() => {
      if (shownId !== null && id !== shownId) {
        shownId = id;
        shown = visible;
        options.clear();
        options.load();
        return;
      }
      const first = shownId === null;
      shownId = id;
      if (first || (visible && !shown)) options.load();
      shown = visible;
    });
  });
}
