import { bodyPurpose, signedJSON } from "../more/daemon";
import type { StoredServer } from "../storedServerContract";

/** Agents outside Mewla (Claude Code, Codex in your own terminal) that may hand work to Brain. */

export interface PendingAgent {
  id: string;
  name: string;
  /** The number the agent's terminal shows; match it before approving. */
  number: string;
  expires_at: string;
}

export interface ApprovedAgent {
  id: string;
  name: string;
  approved_at: string;
  last_used_at?: string;
}

export interface OutsideAgents {
  pending: PendingAgent[];
  agents: ApprovedAgent[];
}

function parse(raw: Partial<OutsideAgents> | null): OutsideAgents {
  return { pending: raw?.pending ?? [], agents: raw?.agents ?? [] };
}

export async function listOutsideAgents(server: StoredServer): Promise<OutsideAgents> {
  return parse(await signedJSON<Partial<OutsideAgents>>(server, "GET", "/agents", "mewla-agents-admin:list:GET:/agents"));
}

export async function decideOutsideAgent(server: StoredServer, action: "approve" | "deny" | "revoke", id: string): Promise<OutsideAgents> {
  return parse(await signedJSON<Partial<OutsideAgents>>(server, "POST", "/agents", bodyPurpose("mewla-agents-admin:decide:POST:/agents"), { body: { action, id } }));
}
