import * as Crypto from "expo-crypto";
import { getOrCreateLocalDeviceIdentity, type LocalDeviceIdentity } from "./auth";
import type { StoredServer } from "./storage";
import { requestFailure, signedFetch } from "./signedRequest";
import type { IPhoneDaemonStatus } from "./iphoneStatus";

export type { IPhoneDaemonStatus } from "./iphoneStatus";
export { iphoneOffered, iphoneStateWord } from "./iphoneStatus";

export type IPhoneChange = { enabled: boolean } | { stop_agent: true } | { handoff_done: true };

export async function getIPhoneStatus(
  server: StoredServer,
  loadIdentity: () => Promise<LocalDeviceIdentity> = getOrCreateLocalDeviceIdentity,
): Promise<IPhoneDaemonStatus> {
  const identity = await loadIdentity();
  const response = await signedFetch(server, "GET", "/iphone", "mewla-iphone:GET:/iphone", undefined, identity);
  if (!response.ok) throw await requestFailure(server, response);
  return (await response.json()) as IPhoneDaemonStatus;
}

/** Applies one change; the signature covers the body. */
export async function changeIPhone(
  server: StoredServer,
  change: IPhoneChange,
  loadIdentity: () => Promise<LocalDeviceIdentity> = getOrCreateLocalDeviceIdentity,
): Promise<IPhoneDaemonStatus> {
  const identity = await loadIdentity();
  const body = JSON.stringify(change);
  const digest = await Crypto.digestStringAsync(Crypto.CryptoDigestAlgorithm.SHA256, body);
  const response = await signedFetch(
    server,
    "POST",
    "/iphone",
    `mewla-iphone:POST:/iphone:sha256=${digest.toLowerCase()}`,
    body,
    identity,
  );
  if (!response.ok) throw await requestFailure(server, response);
  return (await response.json()) as IPhoneDaemonStatus;
}
