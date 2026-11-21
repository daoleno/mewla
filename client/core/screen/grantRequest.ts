import { signedJSON } from "../more/daemon";
import type { StoredServer } from "../storedServerContract";

/** What the person can do with a grant request card: Open System Settings, Not now, and the restart a grant sometimes needs. */
export type GrantRequestAction = "open_settings" | "not_now" | "restart";

/** The card's button, on the computer that asked (this Mac's daemon): its answer is the card's new state, which the thread also pushes. */
export function grantRequestAction(server: StoredServer, id: string, action: GrantRequestAction): Promise<unknown> {
  return signedJSON(server, "POST", "/brain/grant-request", (bodySha256) => `mewla-grant-request:POST:/brain/grant-request:sha256=${bodySha256}`, {
    body: { id, action },
    timeoutMs: 15_000,
  });
}
