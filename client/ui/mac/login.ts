import { signal, untracked } from "nsh";
import { Application } from "nsh/desktop";

/** Start at login (`Application.launchAtLogin`): the cat's menu's check mark and Settings › General's switch. */
export const atLogin = signal(false);

/** Reads it from the system (the login item may have been removed in System Settings). */
export function readStartAtLogin(): void {
  void Application.launchAtLogin().then((on) => atLogin.set(on)).catch(() => {});
}

/** Turns it on or off; the switch and the check mark follow once the system has it. */
export function setStartAtLogin(next: boolean): Promise<void> {
  return Application.setLaunchAtLogin(next).then(() => atLogin.set(next)).catch((error) => console.warn("start at login:", error));
}

export const toggleStartAtLogin = (): void => void setStartAtLogin(!untracked(atLogin));
