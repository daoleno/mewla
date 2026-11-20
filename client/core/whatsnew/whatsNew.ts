import { compareVersions, parseVersion } from "../update/version";

/**
 * What's New (owner 11-16, Brain's SPEC): after Mewla changed version, the first quiet moment on
 * Brain home shows a sheet of what is new, once; every update also leaves a line in Brain's chat.
 *
 * The notes are one file per release (`release-notes/<version>.json`), bundled into the app and the
 * daemon by `scripts/release-notes.py`. What the person has seen lives on the computer
 * (`whats_new` on the daemon), per highlight id, so seeing it on the phone means the web does not
 * show it again. This file is the rules; client/ui/whatsnew draws them.
 */

export type WhatsNewTryKind = "open" | "prompt";

/** Where "Try it" goes: a place to open, or a prompt written into Brain's composer. */
export interface WhatsNewTry {
  kind: WhatsNewTryKind;
  target: string;
}

export interface WhatsNewHighlight {
  id: string;
  title: string;
  line: string;
  media: { scene: string };
  try?: WhatsNewTry;
}

export interface WhatsNewRelease {
  version: string;
  date: string;
  highlights: WhatsNewHighlight[];
  fixes: string[];
}

/** One page of the sheet: a highlight and the release it came in. */
export interface WhatsNewPage extends WhatsNewHighlight {
  version: string;
}

/** An update the person's Mewla made: the app's or the computer's, once per version. */
export interface WhatsNewUpdate {
  version: string;
  /** Milliseconds since the epoch. */
  at: number;
}

/** The computer's record of what is news to this person (the daemon's `whats_new`). */
export interface WhatsNewRecord {
  /**
   * Versions at or below it were never news: the version Mewla was installed at. Empty when
   * Mewla was there before it kept this record, so every release with notes counts.
   */
  baseline: string;
  seen: string[];
  updates: WhatsNewUpdate[];
  /** `mewla dev whats-new`: show the newest release's pages once, whatever the versions say. */
  replay?: boolean;
}

/** At most this many pages in one sheet, however many releases were missed. */
export const MAX_PAGES = 3;

/** How long Brain home has to be quiet before the sheet comes up. */
export const QUIET_MS = 1500;

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function parseHighlight(raw: unknown): WhatsNewHighlight | null {
  if (!isRecord(raw)) return null;
  const { id, title, line, media } = raw;
  if (typeof id !== "string" || typeof title !== "string" || typeof line !== "string") return null;
  if (!isRecord(media) || typeof media.scene !== "string") return null;
  const out: WhatsNewHighlight = { id, title, line, media: { scene: media.scene } };
  const action = raw.try;
  if (isRecord(action) && (action.kind === "open" || action.kind === "prompt") && typeof action.target === "string") {
    out.try = { kind: action.kind, target: action.target };
  }
  return out;
}

/** A notes bundle (`{releases: [...]}`), read leniently: a malformed release or highlight is left out. */
export function parseReleases(raw: unknown): WhatsNewRelease[] {
  const list = isRecord(raw) && Array.isArray(raw.releases) ? raw.releases : Array.isArray(raw) ? raw : [];
  const out: WhatsNewRelease[] = [];
  for (const item of list) {
    if (!isRecord(item) || typeof item.version !== "string" || !parseVersion(item.version)) continue;
    const highlights = Array.isArray(item.highlights)
      ? item.highlights.map(parseHighlight).filter((h): h is WhatsNewHighlight => h !== null)
      : [];
    const fixes = Array.isArray(item.fixes) ? item.fixes.filter((f): f is string => typeof f === "string") : [];
    out.push({ version: item.version, date: typeof item.date === "string" ? item.date : "", highlights, fixes });
  }
  return sortNewestFirst(out);
}

function sortNewestFirst(releases: WhatsNewRelease[]): WhatsNewRelease[] {
  return [...releases].sort((a, b) => compareVersions(b.version, a.version) ?? 0);
}

/**
 * The app's own notes and the computer's, one release per version. The app's copy wins: it is
 * the one whose pictures this app can draw. The computer's fills in releases newer than the app.
 */
export function mergeReleases(own: WhatsNewRelease[], computer: WhatsNewRelease[]): WhatsNewRelease[] {
  const byVersion = new Map<string, WhatsNewRelease>();
  for (const release of computer) byVersion.set(release.version, release);
  for (const release of own) byVersion.set(release.version, release);
  return sortNewestFirst([...byVersion.values()]);
}

/** The computer's record, read leniently; null when it is not one (an older computer has none). */
export function parseRecord(raw: unknown): WhatsNewRecord | null {
  if (!isRecord(raw) || typeof raw.baseline !== "string") return null;
  const seen = Array.isArray(raw.seen) ? raw.seen.filter((id): id is string => typeof id === "string") : [];
  const updates: WhatsNewUpdate[] = [];
  if (Array.isArray(raw.updates)) {
    for (const item of raw.updates) {
      if (isRecord(item) && typeof item.version === "string" && typeof item.at === "number" && Number.isFinite(item.at)) {
        updates.push({ version: item.version, at: item.at });
      }
    }
  }
  return raw.replay === true ? { baseline: raw.baseline, seen, updates, replay: true } : { baseline: raw.baseline, seen, updates };
}

/** The newer of two versions; either may be missing. */
export function newestOf(...versions: (string | null | undefined)[]): string | null {
  let best: string | null = null;
  for (const version of versions) {
    if (!version || !parseVersion(version)) continue;
    if (best === null || (compareVersions(version, best) ?? 0) > 0) best = version;
  }
  return best;
}

/** Whether a release is news: newer than the baseline and no newer than the Mewla the person runs. */
function inWindow(version: string, baseline: string, upTo: string): boolean {
  if (baseline && (compareVersions(version, baseline) ?? 1) <= 0) return false;
  return (compareVersions(version, upTo) ?? 1) <= 0;
}

/**
 * The pages to show now: unseen highlights of the releases that are news, newest release first,
 * at most `max`. Several missed releases share the pages: each release's first highlight, then
 * each one's second, so a missed release keeps its headline. A release with no highlights (fixes
 * only) gives no page.
 */
export function pendingPages(
  releases: WhatsNewRelease[],
  record: WhatsNewRecord,
  upTo: string | null,
  max = MAX_PAGES,
): WhatsNewPage[] {
  if (!upTo) return [];
  const seen = new Set(record.seen);
  const columns = sortNewestFirst(releases)
    .filter((release) => inWindow(release.version, record.baseline, upTo))
    .map((release) => release.highlights.filter((h) => !seen.has(h.id)).map((h) => ({ ...h, version: release.version })));
  const out: WhatsNewPage[] = [];
  for (let row = 0; out.length < max && columns.some((column) => row < column.length); row += 1) {
    for (const column of columns) {
      if (row < column.length && out.length < max) out.push(column[row]);
    }
  }
  return out;
}

/**
 * Every unseen highlight of the releases that are news: what one sheet settles. Its pages are the best of them; the
 * rest are not offered in another sheet after it (dismissed is gone for those versions).
 */
export function newsIds(releases: WhatsNewRelease[], record: WhatsNewRecord, upTo: string | null): string[] {
  if (!upTo) return [];
  const seen = new Set(record.seen);
  return releases
    .filter((release) => inWindow(release.version, record.baseline, upTo))
    .flatMap((release) => release.highlights.map((h) => h.id).filter((id) => !seen.has(id)));
}

/** Every highlight of the releases up to `upTo`: what a fresh install marks seen. */
export function highlightIdsUpTo(releases: WhatsNewRelease[], upTo: string): string[] {
  return releases
    .filter((release) => (compareVersions(release.version, upTo) ?? 1) <= 0)
    .flatMap((release) => release.highlights.map((h) => h.id));
}

/** One release's pages, for the chat line and the About row (seen or not). */
export function releasePages(release: WhatsNewRelease): WhatsNewPage[] {
  return release.highlights.slice(0, MAX_PAGES).map((h) => ({ ...h, version: release.version }));
}

/** The release with this version, or the newest at or below `upTo` (About's "What's new"). */
export function releaseOf(releases: WhatsNewRelease[], version: string): WhatsNewRelease | null {
  return releases.find((release) => release.version === version) ?? null;
}

export function latestRelease(releases: WhatsNewRelease[], upTo: string | null): WhatsNewRelease | null {
  if (!upTo) return null;
  return sortNewestFirst(releases).find((release) => (compareVersions(release.version, upTo) ?? 1) <= 0) ?? null;
}

/** The line an update leaves in Brain's chat: "Updated to 0.4.9 · What's new". */
export interface UpdateLine {
  version: string;
  at: number;
  /** Whether its release has pages (else the line opens its notes list). */
  highlights: boolean;
  /** Whether its release has notes at all: a line without them says only that Mewla updated. */
  notes: boolean;
}

export function updateLines(record: WhatsNewRecord, releases: WhatsNewRelease[]): UpdateLine[] {
  const byVersion = new Map(releases.map((release) => [release.version, release]));
  const lines = new Map<string, UpdateLine>();
  for (const update of record.updates) {
    if (lines.has(update.version)) continue;
    const release = byVersion.get(update.version);
    lines.set(update.version, {
      version: update.version,
      at: update.at,
      highlights: Boolean(release?.highlights.length),
      notes: Boolean(release && (release.highlights.length || release.fixes.length)),
    });
  }
  return [...lines.values()].sort((a, b) => a.at - b.at);
}

/** What has to be true of the app before the sheet may come up on its own. */
export interface OfferGate {
  /** Brain home is the page in front (not a pushed page, not another tab). */
  onBrainHome: boolean;
  /** The app is in front of the person. */
  active: boolean;
  /** Onboarding, pairing or sign-in is in front, or the computer is not paired and connected. */
  settingUp: boolean;
  /** The app was opened this launch from a link or a notification, and the person has not yet come back to home on their own. */
  openedForSomething: boolean;
  /** Brain's composer has focus or holds a draft. */
  composing: boolean;
  /** A sheet, menu, dialog or picture viewer is open. */
  overlayOpen: boolean;
  /** Brain's thread shows a card that needs the person (an approval, a question). */
  needsPerson: boolean;
}

export function mayOffer(gate: OfferGate): boolean {
  return gate.onBrainHome && gate.active && !gate.settingUp && !gate.openedForSomething
    && !gate.composing && !gate.overlayOpen && !gate.needsPerson;
}
