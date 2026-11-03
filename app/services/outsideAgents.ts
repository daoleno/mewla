import * as Crypto from "expo-crypto";
import { getOrCreateLocalDeviceIdentity } from "./auth";
import { requestFailure, signedFetch } from "./signedRequest";
import type { StoredServer } from "./storage";

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

function parse(raw: Partial<OutsideAgents>): OutsideAgents {
  return { pending: raw.pending ?? [], agents: raw.agents ?? [] };
}

/** Outside agents waiting for a decision, and the approved ones, on this computer. */
export async function listOutsideAgents(server: StoredServer): Promise<OutsideAgents> {
  const identity = await getOrCreateLocalDeviceIdentity();
  const response = await signedFetch(server, "GET", "/agents", "mewla-agents-admin:list:GET:/agents", undefined, identity);
  if (!response.ok) throw await requestFailure(server, response);
  return parse((await response.json()) as Partial<OutsideAgents>);
}

/** Approve or deny a waiting request, or revoke an approved agent. */
export async function decideOutsideAgent(
  server: StoredServer,
  action: "approve" | "deny" | "revoke",
  id: string,
): Promise<OutsideAgents> {
  const identity = await getOrCreateLocalDeviceIdentity();
  const body = JSON.stringify({ action, id });
  const digest = await Crypto.digestStringAsync(Crypto.CryptoDigestAlgorithm.SHA256, body);
  const response = await signedFetch(
    server,
    "POST",
    "/agents",
    `mewla-agents-admin:decide:POST:/agents:sha256=${digest.toLowerCase()}`,
    body,
    identity,
  );
  if (!response.ok) throw await requestFailure(server, response);
  return parse((await response.json()) as Partial<OutsideAgents>);
}
