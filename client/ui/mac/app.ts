import { host, platform } from "nsh";

/**
 * Mewla for Mac is this computer's Mewla: its host runs the daemon (client/macos), and the app is that daemon's
 * own client, paired with it without a step the person sees.
 */
export const isMacApp = (): boolean => platform.os === "macos" && host.available();
