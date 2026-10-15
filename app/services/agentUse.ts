import * as Crypto from "expo-crypto";
import { getOrCreateLocalDeviceIdentity, type LocalDeviceIdentity } from "./auth";
import type { StoredServer } from "./storage";
import { requestFailure, signedFetch } from "./signedRequest";

export type AgentUseFix = "install_extension" | "open_extension" | "retry" | "open_settings";

/** What agents can use on one computer, as that computer reports it. */
export interface AgentUseStatus {
  /** The one switch: agents can use this computer. On by default. */
  enabled: boolean;
  browser: {
    state: "ready" | "off" | "no_chrome" | "no_extension" | "no_key" | "no_node" | "no_desktop" | "preparing" | "failed";
    /** One line to show as is. */
    summary: string;
    /** The action the computer can take to fix `state`. */
    fix?: AgentUseFix;
    profile?: string;
    key_pasted?: boolean;
  };
  /** The screen, keyboard and pointer; reported by computers with Screen. */
  computer?: {
    state: "ready" | "off" | "unsupported" | "screen_off" | "permission" | "unavailable";
    summary: string;
    fix?: "open_settings";
    permission?: string;
    /** The agent using the screen now. */
    active?: { worker: string; name?: string; since: string };
  };
}

export type AgentUseChange = { enabled: boolean } | { fix: AgentUseFix } | { key: string } | { stop_agent: true };

export async function getAgentUse(
  server: StoredServer,
  loadIdentity: () => Promise<LocalDeviceIdentity> = getOrCreateLocalDeviceIdentity,
): Promise<AgentUseStatus> {
  const identity = await loadIdentity();
  const response = await signedFetch(server, "GET", "/agent-use", "mewla-agent-use:GET:/agent-use", undefined, identity);
  if (!response.ok) throw await requestFailure(server, response);
  return (await response.json()) as AgentUseStatus;
}

/** Applies one change. The signature covers the body, which may carry a key. */
export async function changeAgentUse(
  server: StoredServer,
  change: AgentUseChange,
  loadIdentity: () => Promise<LocalDeviceIdentity> = getOrCreateLocalDeviceIdentity,
): Promise<AgentUseStatus> {
  const identity = await loadIdentity();
  const body = JSON.stringify(change);
  const digest = await Crypto.digestStringAsync(Crypto.CryptoDigestAlgorithm.SHA256, body);
  const response = await signedFetch(
    server,
    "POST",
    "/agent-use",
    `mewla-agent-use:POST:/agent-use:sha256=${digest.toLowerCase()}`,
    body,
    identity,
  );
  if (!response.ok) throw await requestFailure(server, response);
  return (await response.json()) as AgentUseStatus;
}
