import { bodyPurpose, signedJSON } from "../more/daemon";
import type { StoredServer } from "../storedServerContract";

/** What the current computer says about Phone use (daemon/server/phone.go, agentuse.PhoneStatus). */
export interface PhoneDaemonStatus {
  enabled: boolean;
  connected: boolean;
  state?: string;
  model?: string;
  sdk?: number;
  /** The agent using the phone now. */
  active?: { worker: string; name?: string; since: string };
}

export type PhoneChange = { enabled: boolean } | { stop_agent: true };

/** Signed `GET /phone`. */
export function getPhoneStatus(server: StoredServer): Promise<PhoneDaemonStatus> {
  return signedJSON<PhoneDaemonStatus>(server, "GET", "/phone", "mewla-phone:GET:/phone", { timeoutMs: 15_000 });
}

/** Applies one change: the computer's own switch for its Workers, or Stop. The signature covers the body. */
export function changePhone(server: StoredServer, change: PhoneChange): Promise<PhoneDaemonStatus> {
  return signedJSON<PhoneDaemonStatus>(server, "POST", "/phone", bodyPurpose("mewla-phone:POST:/phone"), { body: change, timeoutMs: 15_000 });
}
