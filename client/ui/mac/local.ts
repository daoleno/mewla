import { host, signal, untracked } from "nsh";
import { titleBar, Window, type TitleBarOptions } from "nsh/desktop";
import { httpOrigin, NOT_RUNNING, ownEntryPlan } from "../../core/mac/thisMac";
import { PairingUnreachableError } from "../../core/pairing";
import { pairWith, readPairingLink } from "../../core/pairFlow";
import { platform } from "../../core/platform";
import { getServers, removeServer, saveServer } from "../../core/servers";
import { checkComputer } from "../../core/settings/computer";
import { addPairedServer, current, servers, switchTo } from "../session";
import { isMacApp } from "./app";

export { isMacApp };

/**
 * The window's frame (DESIGN-MAC.md §2.3): full-size content without a title bar, the traffic lights at 16, 17 in
 * the left pane, on a row of their own above its header; the right pane's header runs at the window's top.
 */
export const MAC_TITLE_BAR: TitleBarOptions = { style: "hidden", height: 46, controlsOffset: { x: 16, y: 17 } };

/** The window, shown again after ⌘W hid it, in front. */
export function showMainWindow(): void {
  const main = Window.get("main");
  main?.show();
  main?.focus();
}

/** The traffic lights' row at the top of the left pane: the hidden title bar's height (0 in full screen, off the Mac). */
export const macLightsRow = (): number => (isMacApp() ? titleBar().height : 0);

/** `mewla pair -json` of the app's own daemon, as the host's `app.pairing` gives it. */
export type DaemonPairing = {
  mac_lan?: boolean;
  expires_at: string;
  daemon_id: string;
  daemon_public_key: string;
  phone_address?: string;
  phone_link?: string;
  phone_qr?: { size: number; rows: string[] };
  wifi_address?: string;
  wifi_link?: string;
  wifi_qr?: { size: number; rows: string[] };
  local_link?: string;
};

/** A fresh one-time pairing of this computer's daemon (15 minutes): the phone QR and the app's own link. */
export async function daemonPairing(): Promise<DaemonPairing> {
  return (await host.call("app.pairing", {}, { timeout: 120_000 })) as DaemonPairing;
}

export async function setMacLAN(enabled: boolean): Promise<void> {
  await host.call("app.setLAN", { enabled }, { timeout: 100_000 });
}

/** Why the app could not join its own daemon, in one plain line; empty while it starts or once it has. */
export const localProblem = signal("");

/** This Mac's daemon's public key: kept across launches, so this Mac's entry is known before its daemon answers. */
const LOCAL_KEY = "mewla:v1:mac_local_daemon_key";
const localDaemonKey = signal("");

/** This Mac's daemon's own (loopback) http origin, once it answered this launch. */
const localOrigin = signal("");

/** Whether this launch's join has given this Mac its entry. */
export const localJoined = signal(false);

/**
 * The person chose This Mac in the computers list before it has an entry: the window shows it starting, or why not,
 * with Try Again, until the join makes it the computer (or they go back).
 */
export const thisMacChosen = signal(false);

/** This Mac's name before its entry exists: the Mac's own (System Settings › Sharing). */
export function thisMacName(): string {
  return (host.params as { computerName?: string }).computerName || "This Mac";
}

/** Whether `server` is this Mac's own daemon (This Mac's page, DESIGN-MAC.md §2.4). */
export function isThisMac(server: { daemonPublicKey: string } | null | undefined): boolean {
  return Boolean(server && isMacApp() && localDaemonKey() && sameKey(server.daemonPublicKey, localDaemonKey()));
}

const sameKey = (a: string, b: string) => a.trim().toLowerCase() === b.trim().toLowerCase();

/**
 * This Mac's own daemon's address (loopback), whichever computer is current: the terminal's page files are the app's
 * own (the daemon embeds them), so a Session's terminal on any computer loads them from here. It is the daemon's own
 * loopback address, never a stored computer's (owner 11-23: with this Mac's entry removed, or stored by the hosted
 * name, no terminal opened). Empty before the daemon answered.
 */
export function localDaemonURL(): string {
  return localOrigin();
}

let retry: ReturnType<typeof setTimeout> | null = null;

/**
 * This Mac is the app's computer, always (owner 11-23): one entry for its daemon, at its own loopback address, with no
 * pairing step and no confirmation. A missing entry is paired again; an old one stored by the hosted name or Mewla
 * Relay moves to loopback in place (same id: its Sessions and name stay); a second one goes; a daemon that no longer
 * knows this app is paired again. Until the daemon answers, why not is said, and the app asks again every 20 s.
 */
let joining: Promise<void> | null = null;

export function joinLocalDaemon(): Promise<void> {
  joining ??= join().finally(() => (joining = null));
  return joining;
}

async function join(): Promise<void> {
  if (retry) clearTimeout(retry);
  retry = null;
  if (!untracked(localDaemonKey)) {
    const known = await platform().storage.getItem(LOCAL_KEY).catch(() => null);
    if (known && !untracked(localDaemonKey)) localDaemonKey.set(known);
  }
  try {
    const pairing = await daemonPairing();
    localDaemonKey.set(pairing.daemon_public_key);
    void platform().storage.setItem(LOCAL_KEY, pairing.daemon_public_key).catch(() => {});
    const payload = pairing.local_link ? readPairingLink(pairing.local_link) : null;
    const plan = payload
      ? ownEntryPlan(untracked(servers), pairing.daemon_public_key, payload.url, untracked(current)?.id ?? null)
      : { keep: untracked(servers).find((server) => sameKey(server.daemonPublicKey, pairing.daemon_public_key)) ?? null, drop: [], rewrite: false };
    for (const id of plan.drop) await removeServer(id);
    let own = plan.keep;
    if (own && plan.rewrite && payload) {
      own = await saveServer({ id: own.id, name: own.name, url: payload.url, daemonId: own.daemonId, daemonPublicKey: own.daemonPublicKey, transportKind: "manual" });
    }
    if (plan.drop.length || plan.rewrite) servers.set(await getServers());
    localOrigin.set(payload ? httpOrigin(payload.url) : own ? httpOrigin(own.url) : "");
    localProblem.set("");
    if (own) {
      localJoined.set(true);
      thisMacChosen.set(false);
      if (untracked(current)?.id !== own.id) switchTo(own);
      // A daemon that revoked this app (the entry removed while it answered) is paired again, in place.
      const entry = own;
      if (payload && (await checkComputer(entry)).state === "unpaired") {
        await saveServer({ ...(await pairWith(payload)), id: entry.id, name: entry.name });
        servers.set(await getServers());
        switchTo(untracked(servers).find((server) => server.id === entry.id) ?? null);
      }
      return;
    }
    if (!payload) throw new Error("This Mac's Mewla gave no address.");
    const params = host.params as { computerName?: string };
    await addPairedServer(await pairWith({ ...payload, name: params.computerName || payload.name }));
    localJoined.set(true);
    thisMacChosen.set(false);
  } catch (error) {
    // Nothing answered at its own loopback address: its Mewla is not running (never the phone's adb-reverse advice).
    localProblem.set(error instanceof PairingUnreachableError ? NOT_RUNNING : error instanceof Error ? error.message : String(error));
    retry = setTimeout(() => void joinLocalDaemon(), 20_000);
  }
}
