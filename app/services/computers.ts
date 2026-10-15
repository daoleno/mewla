import * as Crypto from "expo-crypto";
import { getOrCreateLocalDeviceIdentity, type LocalDeviceIdentity } from "./auth";
import { verifyDaemonAssertion } from "./deviceAuthContract";
import type { StoredServer } from "./storage";
import { otherComputerURLs } from "./computerAddresses";
import { signedFetch, requestFailure as failure } from "./signedRequest";

export interface ComputerPeer {
  name: string;
  daemon_id?: string;
  device_id?: string;
  direction: "outbound" | "inbound";
  online: boolean;
  last_seen?: string;
  version?: string;
  last_error?: string;
}

// listComputers returns the computers this server works with.
export async function listComputers(
  server: StoredServer,
  loadIdentity: () => Promise<LocalDeviceIdentity> = getOrCreateLocalDeviceIdentity,
): Promise<ComputerPeer[]> {
  const identity = await loadIdentity();
  const response = await signedFetch(server, "GET", "/computers", "mewla-computer-admin:list:GET:/computers", undefined, identity);
  if (!response.ok) throw await failure(server, response);
  const result = (await response.json()) as { computers?: ComputerPeer[] };
  return result.computers ?? [];
}

// connectComputers lets `current` hand work to `other`: the phone, trusted by
// both, asks `other` for a one-time invite and gives it to `current`, which
// pairs with `other` itself and pins the key this phone already pinned.
export async function connectComputers(
  current: StoredServer,
  other: StoredServer,
  loadIdentity: () => Promise<LocalDeviceIdentity> = getOrCreateLocalDeviceIdentity,
): Promise<void> {
  const identity = await loadIdentity();
  const inviteResponse = await signedFetch(
    other,
    "POST",
    "/computers/invite",
    "mewla-computer-admin:invite:POST:/computers/invite",
    undefined,
    identity,
  );
  if (!inviteResponse.ok) throw await failure(other, inviteResponse);
  const invite = (await inviteResponse.json()) as {
    invite?: string;
    assertion_timestamp?: string;
    assertion_nonce?: string;
    assertion_signature?: string;
  };
  const signed = verifyDaemonAssertion({
    purpose: "mewla-computer-invite",
    daemonId: other.daemonId,
    daemonPublicKey: other.daemonPublicKey,
    timestamp: invite.assertion_timestamp,
    nonceHex: invite.assertion_nonce,
    signatureHex: invite.assertion_signature,
  });
  if (!signed || !invite.invite) {
    throw new Error(`${other.name} did not answer with its own key`);
  }
  const body = JSON.stringify({
    invite: invite.invite,
    name: other.name,
    urls: otherComputerURLs(other),
    expected_daemon_id: other.daemonId,
  });
  const digest = await Crypto.digestStringAsync(Crypto.CryptoDigestAlgorithm.SHA256, body);
  const addResponse = await signedFetch(
    current,
    "POST",
    "/computers",
    `mewla-computer-admin:add:POST:/computers:sha256=${digest.toLowerCase()}`,
    body,
    identity,
  );
  if (!addResponse.ok) throw await failure(current, addResponse);
}
