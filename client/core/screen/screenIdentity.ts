// Binds Screen's video to who is at each end. The computer's offer carries its
// signature over the offer's DTLS certificate fingerprints, checked here with
// the computer's key from pairing; this device signs the fingerprints of its
// answer, which the computer checks against the device the socket signed in
// as. The DTLS handshake then holds each side to the certificate it signed
// for, so the media is end to end between the two: nothing between them (the
// relay, a network) can read it or stand in for either side.
// Mirrors daemon/screencast/identity.go and auth.ScreenDTLSPurpose.

import { verifyDaemonAssertion } from "../deviceAuthContract";
import { deviceAuthorization, sha256Hex } from "../more/daemon";
import type { StoredServer } from "../storedServerContract";

/** The DTLS fingerprints an SDP names, as "<hash> <HEX:HEX:…>", sorted. */
export function sdpFingerprints(sdp: string): string[] {
  const out = new Set<string>();
  for (const line of sdp.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed.startsWith("a=fingerprint:")) continue;
    const value = trimmed.slice("a=fingerprint:".length).trim();
    const space = value.indexOf(" ");
    if (space < 0) continue;
    out.add(`${value.slice(0, space).trim().toLowerCase()} ${value.slice(space + 1).trim().toUpperCase()}`);
  }
  return [...out].sort();
}

/** What both sides sign for these fingerprints. */
export async function screenDtlsPurpose(fingerprints: string[]): Promise<string> {
  return `mewla-screen-dtls:sha256=${await sha256Hex(fingerprints.join("\n"))}`;
}

export interface OfferAssertion {
  timestamp?: unknown;
  nonce_hex?: unknown;
  signature_hex?: unknown;
}

/** Whether the computer signed this offer's certificates. */
export async function offerIsTheComputers(server: Pick<StoredServer, "daemonId" | "daemonPublicKey">, sdp: string, assertion: OfferAssertion | undefined): Promise<boolean> {
  const fingerprints = sdpFingerprints(sdp);
  if (!fingerprints.length || !assertion) return false;
  const text = (value: unknown) => (typeof value === "string" ? value : null);
  return verifyDaemonAssertion({
    purpose: await screenDtlsPurpose(fingerprints),
    daemonId: server.daemonId,
    daemonPublicKey: server.daemonPublicKey,
    timestamp: text(assertion.timestamp),
    nonceHex: text(assertion.nonce_hex),
    signatureHex: text(assertion.signature_hex),
  });
}

/** This device's signature over its answer's certificates. */
export async function signAnswer(server: StoredServer, sdp: string): Promise<string> {
  return deviceAuthorization(server, await screenDtlsPurpose(sdpFingerprints(sdp)));
}
