/**
 * A Mac's permissions for the screen (Screen Recording, Accessibility) as its daemon reads them afresh on every status
 * (`/agent-use` computer.permissions): granted; off (Mewla is in the list, switched off); missing (not in the list:
 * never asked, its row removed with −, or a row of another signature of Mewla); restart (given, in effect once Mewla
 * starts again, which it does by itself once quiet).
 */
export type GrantState = "granted" | "off" | "missing" | "restart";

export interface ComputerGrant {
  id: string;
  name: string;
  state: GrantState;
}

/** What a grant's row offers: Allowed; Allow (System Settings and the panel to drag Mewla in); Restart Mewla. */
export type GrantRow = "allowed" | "allow" | "restart";

export function grantRow(state: GrantState | undefined): GrantRow | null {
  switch (state) {
    case "granted":
      return "allowed";
    case "off":
    case "missing":
      return "allow";
    case "restart":
      return "restart";
    default:
      return null;
  }
}

/** The grant `id` in a computer's status, or undefined when the computer reports none (not a Mac, or an older one). */
export function grantOf(grants: readonly ComputerGrant[] | undefined, id: string): ComputerGrant | undefined {
  return grants?.find((grant) => grant.id === id);
}
