import * as Crypto from "expo-crypto";
import { getOrCreateLocalDeviceIdentity, type LocalDeviceIdentity } from "./auth";
import type { StoredServer } from "./storage";
import { requestFailure, signedFetch } from "./signedRequest";

/** What the current computer says about Phone use. */
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

export async function getPhoneStatus(
  server: StoredServer,
  loadIdentity: () => Promise<LocalDeviceIdentity> = getOrCreateLocalDeviceIdentity,
): Promise<PhoneDaemonStatus> {
  const identity = await loadIdentity();
  const response = await signedFetch(server, "GET", "/phone", "mewla-phone:GET:/phone", undefined, identity);
  if (!response.ok) throw await requestFailure(server, response);
  return (await response.json()) as PhoneDaemonStatus;
}

/** Applies one change; the signature covers the body. */
export async function changePhone(
  server: StoredServer,
  change: PhoneChange,
  loadIdentity: () => Promise<LocalDeviceIdentity> = getOrCreateLocalDeviceIdentity,
): Promise<PhoneDaemonStatus> {
  const identity = await loadIdentity();
  const body = JSON.stringify(change);
  const digest = await Crypto.digestStringAsync(Crypto.CryptoDigestAlgorithm.SHA256, body);
  const response = await signedFetch(
    server,
    "POST",
    "/phone",
    `mewla-phone:POST:/phone:sha256=${digest.toLowerCase()}`,
    body,
    identity,
  );
  if (!response.ok) throw await requestFailure(server, response);
  return (await response.json()) as PhoneDaemonStatus;
}
