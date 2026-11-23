import { host } from "nsh";
import { changeAgentUse, getAgentUse, type AgentUseStatus } from "../../core/settings/computer";
import { grantOf } from "../../core/settings/macGrants";
import type { StoredServer } from "../../core/storedServerContract";
import { isThisMac } from "./local";

/**
 * On this Mac: System Settings on the permission's list, with the host's panel beside it holding Mewla's icon to drag in
 * (client/macos/src/permissions.rs). The same panel brings Mewla back after its row was removed. `state` is the
 * daemon's reading of the grant: where Mewla.app has no row that counts and the daemon does not count the grant, the
 * host first clears Mewla's own rows. False where
 * there is no such panel (another computer, a phone, a permission that is not a Mac's).
 */
export function showPermissionGuide(server: StoredServer | null | undefined, permission: string, state?: string): boolean {
  if (!server || !isThisMac(server) || (permission !== "screen_recording" && permission !== "accessibility")) return false;
  const data = state ? { permission, state } : { permission };
  void host.call("app.permissionGuide", data).catch((error) => console.warn("permission panel:", error));
  watchUntilGiven(server, permission);
  return true;
}

let watching: ReturnType<typeof setInterval> | null = null;

/**
 * Until the daemon reads the grant as given (a fresh process's answer, never a kept one), up to 15 minutes: then the
 * panel closes and Mewla comes to the front, as the host also does on its own reading.
 */
function watchUntilGiven(server: StoredServer, permission: string): void {
  if (watching) clearInterval(watching);
  const until = Date.now() + 15 * 60_000;
  const timer = setInterval(() => {
    if (Date.now() > until) return void clearInterval(timer);
    void getAgentUse(server).then((status) => {
      const state = grantOf(status.computer?.permissions, permission)?.state;
      if (watching !== timer || (state !== "granted" && state !== "restart")) return;
      clearInterval(timer);
      watching = null;
      void host.call("app.permissionGuide", { given: permission }).catch(() => {});
    }).catch(() => {});
  }, 1_500);
  watching = timer;
}

/**
 * Privacy & access's Allow: the panel, and the daemon told to follow the grant, which it applies by itself once given
 * (a restart in place where macOS needs one). Its answer is the computer's new status.
 */
export async function allowGrant(server: StoredServer, permission: string, state?: string): Promise<AgentUseStatus> {
  showPermissionGuide(server, permission, state);
  return changeAgentUse(server, { follow: permission });
}
