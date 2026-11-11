import { lifecycle, platform } from "nsh";

/**
 * Whether the app is in front, for what streams or polls while it is (reactive).
 * A phone leaves when another app or the system covers it. A browser page stays
 * in front while another window has the focus, as the Expo web app's did: only
 * another tab or a minimized window hides it.
 */
export function inFront(): boolean {
  const state = lifecycle();
  return platform.os === "web" ? state !== "background" : state === "active";
}
