import { host } from "nsh";
import { changeAgentUse, type AgentUseStatus } from "../../core/settings/computer";
import type { StoredServer } from "../../core/storedServerContract";
import { isThisMac } from "./local";

/**
 * On this Mac: System Settings on the permission's list, with the host's panel beside it holding Mewla's icon to drag in
 * (client/macos/src/permissions.rs). The same panel brings Mewla back after its row was removed. False where there is no
 * such panel (another computer, a phone, a permission that is not a Mac's).
 */
export function showPermissionGuide(server: { daemonPublicKey: string } | null | undefined, permission: string): boolean {
  if (!isThisMac(server) || (permission !== "screen_recording" && permission !== "accessibility")) return false;
  void host.call("app.permissionGuide", { permission }).catch((error) => console.warn("permission panel:", error));
  return true;
}

/**
 * Privacy & access's Allow…: the panel, and the daemon told to follow the grant, which it applies by itself once given
 * (a restart in place where macOS needs one). Its answer is the computer's new status.
 */
export async function allowGrant(server: StoredServer, permission: string): Promise<AgentUseStatus> {
  showPermissionGuide(server, permission);
  return changeAgentUse(server, { follow: permission });
}
