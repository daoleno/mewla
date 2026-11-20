import { host, signal, untracked } from "nsh";
import { titleBar, Window, type TitleBarOptions } from "nsh/desktop";
import { pairWith, readPairingLink } from "../../core/pairFlow";
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

/** Why the app could not join its own daemon; empty while it starts or once it has. */
export const localProblem = signal("");

/** This Mac's daemon's public key, once the app has joined it. */
const localDaemonKey = signal("");

/** Whether `server` is this Mac's own daemon (This Mac's page, DESIGN-MAC.md §2.4). */
export function isThisMac(server: { daemonPublicKey: string } | null | undefined): boolean {
  return Boolean(server && isMacApp() && localDaemonKey() && sameKey(server.daemonPublicKey, localDaemonKey()));
}

const sameKey = (a: string, b: string) => a.trim().toLowerCase() === b.trim().toLowerCase();

/**
 * Makes this computer's daemon the current server: the one already paired with it, or a new pairing over its own
 * address (loopback), with no confirmation, since it is this computer.
 */
export async function joinLocalDaemon(): Promise<void> {
  localProblem.set("");
  try {
    const pairing = await daemonPairing();
    localDaemonKey.set(pairing.daemon_public_key);
    const known = untracked(servers).find((server) => sameKey(server.daemonPublicKey, pairing.daemon_public_key));
    if (known) {
      if (untracked(current)?.id !== known.id) switchTo(known);
      return;
    }
    const payload = pairing.local_link ? readPairingLink(pairing.local_link) : null;
    if (!payload) throw new Error("This computer's Mewla gave no address.");
    const params = host.params as { computerName?: string };
    await addPairedServer(await pairWith({ ...payload, name: params.computerName || payload.name }));
  } catch (error) {
    localProblem.set(error instanceof Error ? error.message : String(error));
  }
}
