import { computed, effect, keyboardFrame, lifecycle, signal, Storage, untracked } from "nsh";
import manifest from "../nsh.json";
import bundled from "./notes.json";
import {
  latestRelease,
  mayOffer,
  mergeReleases,
  newestOf,
  newsIds,
  parseRecord,
  parseReleases,
  pendingPages,
  QUIET_MS,
  releaseOf,
  releasePages,
  updateLines,
  type UpdateLine,
  type WhatsNewPage,
  type WhatsNewRecord,
  type WhatsNewRelease,
  type WhatsNewTry,
} from "../../core/whatsnew/whatsNew";
import { compareVersions } from "../../core/update/version";
import { derived } from "../kit/derived";
import { computerUpdate } from "../computer/state";
import { inFront } from "../kit/inFront";
import { justPaired, pairingBusy } from "../pairing";
import { brainDraft, connection, current, currentTransport, ready, routedBrainThread, servers } from "../session";
import { sheetsOpen } from "../shell/AppSheet";
import { goHome, homeShown, openPage, trayOpen } from "../shell/nav";
import { writeIntoDraft } from "../chat/Composer";
import { newSessionOpen } from "../screens/NewSession";

/**
 * What's New (core/whatsnew/whatsNew.ts has the rules): the record the current computer keeps of what is
 * news to this person, the sheet, and the quiet moment on Brain home it comes up in.
 */

export const APP_VERSION: string = manifest.version;
const own = parseReleases(bundled);

/** The current computer's record; null until read (and on a computer without one). */
const record = signal<WhatsNewRecord | null>(null);
/** The computer's notes: releases newer than this app come from it. */
const computerReleases = signal<WhatsNewRelease[]>([]);
export const releases = computed(() => mergeReleases(own, computerReleases()));

/**
 * The newest Mewla the person runs here: this app's or its computer's (the web UI is the computer's own).
 * A replay (`mewla dev whats-new`) takes the newest notes there are.
 */
const upTo = computed(() => {
  if (record()?.replay) return releases()[0]?.version ?? null;
  return newestOf(APP_VERSION, computerUpdate()?.version);
});

/**
 * This device's own start: the app's version when it was installed here with no computer yet. A new phone or
 * browser is no update, so what came before it is not offered on it (the record is the computer's, and the
 * person's other devices still offer it).
 */
const LOCAL_KEY = "mewla:whats_new";
type Local = { app?: string; since?: string };
const local = signal<Local | null>(null);
/** The app updated since it last ran (or ran before it kept this): the computer is told once connected. */
let noteApp = false;

effect(() => {
  if (!ready() || untracked(local)) return;
  void Storage.getItem(LOCAL_KEY).then((raw) => {
    let saved: Local = {};
    try {
      const value = raw ? (JSON.parse(raw) as Local) : {};
      if (value && typeof value === "object") saved = value;
    } catch {
      // unreadable: as never kept
    }
    const fresh = !saved.app && untracked(servers).length === 0;
    if (!fresh && (!saved.app || (compareVersions(APP_VERSION, saved.app) ?? 0) > 0)) noteApp = true;
    const next: Local = { app: APP_VERSION, since: fresh ? APP_VERSION : saved.since };
    local.set(next);
    if (next.app !== saved.app || next.since !== saved.since) void Storage.setItem(LOCAL_KEY, JSON.stringify(next)).catch(() => {});
  }).catch(() => local.set({ app: APP_VERSION }));
});

type Reply = { record?: unknown; releases?: unknown };

function take(reply: Reply, serverId: string): void {
  if (untracked(current)?.id !== serverId) return;
  record.set(parseRecord(reply.record));
  if (reply.releases !== undefined) computerReleases.set(parseReleases(reply.releases));
}

async function ask(type: string, body: Record<string, unknown> = {}): Promise<void> {
  const transport = untracked(currentTransport);
  const serverId = untracked(current)?.id;
  if (!transport || !serverId) return;
  const reply = await transport.request<Reply>(type, body, { reply: "whats_new", timeoutMs: 10_000 });
  take(reply, serverId);
}

// Each computer keeps its own record: read on every connect, cleared on a switch.
let readFor = "";
effect(() => {
  const id = current()?.id ?? "";
  const connected = connection() === "connected";
  if (id !== readFor) {
    record.set(null);
    computerReleases.set([]);
  }
  if (!connected || !id) return;
  readFor = id;
  untracked(() => {
    void ask("whats_new_get").then(() => {
      if (noteApp && untracked(local)) {
        noteApp = false;
        return ask("whats_new_note", { version: APP_VERSION });
      }
    }).catch(() => {});
  });
});

/** The record as this device counts it: nothing from before this device's own start. */
function counted(r: WhatsNewRecord): WhatsNewRecord {
  const since = local()?.since;
  if (!since || r.replay) return r;
  return { ...r, baseline: newestOf(r.baseline || null, since) ?? since };
}

/** What would show now, newest first, at most three. */
// Derived, not computed (client/README.md): a run NSH cut off keeps its last value, and the next change makes it again.
export const pending = derived((): WhatsNewPage[] => {
  const r = record();
  if (!r || !local()) return [];
  return pendingPages(releases(), counted(r), upTo());
});

/** The lines updates leave in Brain's chat. */
export const lines = derived((): UpdateLine[] => {
  const r = record();
  return r ? updateLines(r, releases()) : [];
});

/** The open sheet: its pages, or (a fixes-only release) its fixes. */
export interface WhatsNewSheetState {
  version: string;
  pages: WhatsNewPage[];
  fixes: string[];
}
export const sheet = signal<WhatsNewSheetState | null>(null);

function show(state: WhatsNewSheetState, settles: string[] = state.pages.map((page) => page.id)): void {
  sheet.set(state);
  const ids = [...new Set([...state.pages.map((page) => page.id), ...settles])];
  if (ids.length || untracked(record)?.replay) void ask("whats_new_seen", { ids }).catch(() => {});
}

/** A release's notes on demand (its chat line, About's What's new): its pages, else its fixes. */
export function openWhatsNew(version?: string): void {
  const all = untracked(releases);
  const release = version ? releaseOf(all, version) : latestRelease(all, untracked(upTo) ?? APP_VERSION);
  if (!release) return;
  show({ version: release.version, pages: releasePages(release), fixes: release.highlights.length ? [] : release.fixes });
}

export function closeWhatsNew(): void {
  sheet.set(null);
}

/** Brain's composer has focus or holds a draft (Home sets it). */
export const brainComposing = signal(false);

/**
 * The app was opened for something (a notification, a link): the sheet waits until the person comes back to Brain
 * home on their own (after leaving it) or the app comes to the front again.
 */
const openedFor = signal(false);
export function openedForSomething(): void {
  openedFor.set(true);
}
let wasHome = untracked(homeShown);
let wasFront = untracked(inFront);
effect(() => {
  const home = homeShown();
  const front = inFront();
  if ((home && !wasHome) || (front && !wasFront && lifecycle() === "active")) untracked(() => openedFor() && openedFor.set(false));
  wasHome = home;
  wasFront = front;
});

/** Brain's thread shows a card that needs the person (Home sets it). */
export const brainNeedsPerson = signal(false);

const quiet = derived(() => mayOffer({
  onBrainHome: homeShown() && !trayOpen() && !routedBrainThread(),
  active: inFront(),
  settingUp: !current() || Boolean(justPaired()) || pairingBusy() || connection() !== "connected",
  openedForSomething: openedFor(),
  composing: brainComposing() || keyboardFrame().height > 0,
  overlayOpen: sheetsOpen() > 0 || Boolean(sheet()),
  needsPerson: brainNeedsPerson(),
}));

/** Whether Brain home is quiet enough for the sheet (exported for tests and the dev row). */
export const quietHome = quiet;

// The quiet moment: Brain home quiet for QUIET_MS with something to show. Any change restarts the wait.
effect(() => {
  if (!quiet() || pending().length === 0) return;
  const timer = setTimeout(() => void offer(), QUIET_MS);
  return () => clearTimeout(timer);
});

async function offer(): Promise<void> {
  // Read again first: the person may have just seen it on another device.
  await ask("whats_new_get").catch(() => {});
  const pages = untracked(pending);
  const r = untracked(record);
  if (!pages.length || !r || !untracked(quiet)) return;
  // The sheet settles every release it covers, the highlights it had no room for too.
  show({ version: pages[0].version, pages, fixes: [] }, newsIds(untracked(releases), counted(r), untracked(upTo)));
}

/** A page's Try it: the sheet closes and the thing opens. */
export function tryIt(action: WhatsNewTry): void {
  sheet.set(null);
  if (action.kind === "prompt") {
    goHome();
    writeIntoDraft(untracked(brainDraft), action.target);
    return;
  }
  switch (action.target) {
    case "brain":
      return goHome();
    case "sessions":
      return openPage({ name: "allSessions" });
    case "new-session":
      goHome();
      return newSessionOpen.set(true);
    case "screen":
      return openPage({ name: "screen" });
    case "calendar":
      return openPage({ name: "calendar" });
    case "computer":
      return openPage({ name: "computer" });
    case "settings":
      return openPage({ name: "settings" });
  }
}
