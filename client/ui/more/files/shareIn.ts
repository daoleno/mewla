import { effect, host, platform, signal, untracked } from "nsh";
import { ShareInbox, type ShareConditions, type ShareTarget, type SharedBatch } from "../../../core/files/shareInbox";
import type { UploadDocumentAsset } from "../../../core/files/uploads";

/**
 * What other apps hand to Mewla (Android: the share sheet and Open with; the host copies each share, see
 * client/android/…/share/ShareIn.kt), arriving as attachments in Brain's composer. The host answers:
 * - `share.take` → `SharedBatch[]`: the shares not yet acknowledged, oldest first; an asset's `uri` is a file:// URI of the
 *   host's copy, which `files.upload` streams like a picked file's;
 * - `share.ack {ids}`: these are in a composer, no longer offered;
 * - `share.release {uris}`: these copies are no longer wanted: the host deletes them;
 * - `share.arrived` (event): a share was made while the app runs: take it.
 * A host without them (iOS, the web, a Mac, an older Android app) rejects the calls: there is nothing to receive.
 */

const hosted = () => platform.os === "android" && host.available();

/** The copies the host holds for chips that are still here: only these are given back. */
const shared = new Set<string>();

export function markShared(assets: readonly UploadDocumentAsset[]): void {
  assets.forEach((asset) => shared.add(asset.uri));
}

/** A shared file's chip went (removed, sent, dropped): the host deletes its copy. */
export function releaseShared(uris: readonly string[]): void {
  const mine = uris.filter((uri) => shared.delete(uri));
  if (!mine.length || !hosted()) return;
  void host.call("share.release", { uris: mine }).catch(() => {});
}

const target = signal<ShareTarget | null>(null);
let inbox: ShareInbox | null = null;

/** Brain's composer takes what is shared while it is here; returns what ends it. */
export function registerShareTarget(next: ShareTarget): () => void {
  target.set(next);
  inbox?.setTarget(next);
  return () => {
    if (untracked(target) !== next) return;
    target.set(null);
    inbox?.setTarget(null);
  };
}

/**
 * Starts receiving (Android only): takes what waited for the app (a cold start's share) and what arrives while it runs.
 * `conditions` says when a share may go in; `land` brings the app to Brain's chat when one arrives.
 */
export function startShareIn(conditions: Omit<ShareConditions, "now">, land: () => void): void {
  if (!hosted() || inbox) return;
  const made = new ShareInbox(
    {
      take: () => host.call<SharedBatch[]>("share.take").then((batches) => (Array.isArray(batches) ? batches : [])),
      ack: (ids) => host.call("share.ack", { ids }).then(() => {}),
    },
    { ...conditions, now: () => Date.now() },
    land,
  );
  inbox = made;
  host.on("share.arrived", () => void made.pull());
  // What it waits on changing (a computer, the connection, room in the composer) lets it try again.
  effect(() => {
    conditions.paired();
    conditions.draft();
    conditions.pairAgain();
    conditions.connected();
    target()?.room();
    untracked(() => made.deliver());
  });
  void made.pull();
}
