import * as Crypto from "expo-crypto";
import {
  buildAuthorizationHeader,
  getOrCreateLocalDeviceIdentity,
  normalizeDaemonId,
  normalizePairingToken,
  normalizePublicKeyHex,
  verifyDaemonAssertion,
} from "./auth";

export interface PairingInput {
  serverUrl: string;
  daemonId?: string;
  daemonPublicKey: string;
  enrollmentToken: string;
  // "link" requests run through the app's own pinned Mewla Link tunnel, so
  // their failures never describe loopback ports or cleartext HTTP.
  transport?: "link" | "direct";
}

// The pairing request never got an HTTP answer. Over Mewla Link the daemon
// may still have trusted this phone, so callers confirm before giving up.
export class PairingUnreachableError extends Error {}

// The daemon answered and refused the one-time code (used or expired).
export class PairingCodeRejectedError extends Error {}

export async function enrollWithDaemon(input: PairingInput): Promise<{
  daemonId: string;
  daemonPublicKey: string;
}> {
  const daemonId = normalizeDaemonId(input.daemonId);
  const daemonPublicKey = normalizePublicKeyHex(input.daemonPublicKey);
  const enrollmentToken = normalizePairingToken(input.enrollmentToken);
  const pairURL = buildHTTPURL(input.serverUrl, "/pair");

  if (!daemonPublicKey || !enrollmentToken || !pairURL) {
    throw new Error("Invalid pairing link.");
  }

  const identity = await getOrCreateLocalDeviceIdentity();
  let response: Response;
  try {
    response = await fetch(pairURL, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        enrollment_token: enrollmentToken,
        expected_daemon_id: daemonId || undefined,
        expected_daemon_public_key: daemonPublicKey,
        device_id: identity.deviceId,
        device_name: identity.deviceName,
        device_public_key: identity.publicKeyHex,
      }),
    });
  } catch (error) {
    throw input.transport === "link"
      ? new PairingUnreachableError(LINK_UNREACHABLE_MESSAGE)
      : buildPairingNetworkError(pairURL, error);
  }

  if (!response.ok) {
    const detail = (await response.text()).trim();
    if (response.status === 401) {
      throw new PairingCodeRejectedError(
        input.transport === "link"
          ? PAIRING_CODE_USED_MESSAGE
          : detail || PAIRING_CODE_USED_MESSAGE,
      );
    }
    throw new Error(detail || `Pairing failed (${response.status})`);
  }

  const payload = (await response.json()) as {
    daemon_id?: string;
    daemon_public_key?: string;
    assertion_timestamp?: string;
    assertion_nonce?: string;
    assertion_signature?: string;
  };
  const pairedDaemonId = normalizeDaemonId(payload.daemon_id);
  const pairedDaemonPublicKey = normalizePublicKeyHex(
    payload.daemon_public_key,
  );
  if (!pairedDaemonId || !pairedDaemonPublicKey) {
    throw new Error("Daemon returned an invalid identity.");
  }
  if (
    pairedDaemonPublicKey !== daemonPublicKey ||
    (daemonId && pairedDaemonId !== daemonId)
  ) {
    throw new Error("Pairing target identity did not match the scanned link.");
  }
  if (
    !verifyDaemonAssertion({
      purpose: "mewla-pair",
      daemonId: pairedDaemonId,
      daemonPublicKey: pairedDaemonPublicKey,
      timestamp: payload.assertion_timestamp,
      nonceHex: payload.assertion_nonce,
      signatureHex: payload.assertion_signature,
    })
  ) {
    throw new Error("Pairing target failed daemon identity proof.");
  }

  return {
    daemonId: pairedDaemonId,
    daemonPublicKey: pairedDaemonPublicKey,
  };
}

export const LINK_UNREACHABLE_MESSAGE =
  "Mewla couldn't reach this computer. Make sure it's awake and mewla is running there with remote access connected (mewla doctor shows it), then try again. If it is, this phone's network may be blocking Mewla; try Wi-Fi or mobile data instead.";
export const PAIRING_CODE_USED_MESSAGE =
  "This pairing code was already used or has expired. Run mewla pair on the computer and scan the new code.";

// confirmPairedDevice asks the daemon, with this phone's device key, whether
// it already trusts this phone. A /pair response lost on the way back leaves
// the daemon paired while the app saw only a network error; the signed probe
// tells the two apart without spending another one-time code.
async function daemonIdFromPublicKey(publicKeyHex: string): Promise<string> {
  const key = Uint8Array.from(
    publicKeyHex.match(/../g) ?? [],
    (byte) => parseInt(byte, 16),
  );
  const digest = await Crypto.digest(Crypto.CryptoDigestAlgorithm.SHA256, key);
  return Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
}

export async function confirmPairedDevice(input: {
  serverUrl: string;
  daemonId: string;
  daemonPublicKey: string;
}): Promise<{ daemonId: string; daemonPublicKey: string } | null> {
  const daemonPublicKey = normalizePublicKeyHex(input.daemonPublicKey);
  // The Link pairing link leaves the daemon ID out: it is the SHA-256 of the
  // daemon's public key.
  const daemonId =
    normalizeDaemonId(input.daemonId) ||
    (daemonPublicKey ? await daemonIdFromPublicKey(daemonPublicKey) : "");
  const authCheckURL = buildHTTPURL(input.serverUrl, "/auth-check");
  if (!daemonId || !daemonPublicKey || !authCheckURL) {
    return null;
  }

  const identity = await getOrCreateLocalDeviceIdentity();
  let response: Response;
  try {
    response = await fetch(authCheckURL, {
      method: "GET",
      headers: {
        Authorization: await buildAuthorizationHeader({
          daemonId,
          purpose: "mewla-probe",
        }),
      },
    });
  } catch {
    throw new PairingUnreachableError(LINK_UNREACHABLE_MESSAGE);
  }
  if (response.status === 401) {
    return null;
  }
  if (!response.ok) {
    throw new PairingUnreachableError(LINK_UNREACHABLE_MESSAGE);
  }
  const payload = (await response.json().catch(() => null)) as {
    device_id?: string;
    daemon_id?: string;
    daemon_public_key?: string;
    assertion_timestamp?: string;
    assertion_nonce?: string;
    assertion_signature?: string;
  } | null;
  if (
    !payload ||
    payload.device_id !== identity.deviceId ||
    normalizeDaemonId(payload.daemon_id) !== daemonId ||
    normalizePublicKeyHex(payload.daemon_public_key) !== daemonPublicKey ||
    !verifyDaemonAssertion({
      purpose: "mewla-probe",
      daemonId,
      daemonPublicKey,
      timestamp: payload.assertion_timestamp,
      nonceHex: payload.assertion_nonce,
      signatureHex: payload.assertion_signature,
    })
  ) {
    throw new Error("Pairing target failed daemon identity proof.");
  }
  return { daemonId, daemonPublicKey };
}

export async function buildSignedRequestHeaders(input: {
  serverUrl: string;
  daemonId: string;
  purpose: "mewla-probe" | "mewla-upload";
}): Promise<Record<string, string>> {
  return {
    Authorization: await buildAuthorizationHeader({
      daemonId: input.daemonId,
      purpose: input.purpose,
    }),
  };
}

export function buildHTTPURL(
  serverUrl: string,
  pathname: string,
): string | null {
  const trimmed = serverUrl.trim();
  if (!trimmed) return null;

  try {
    const parsed = new URL(trimmed);
    if (parsed.protocol === "ws:") {
      parsed.protocol = "http:";
    } else if (parsed.protocol === "wss:") {
      parsed.protocol = "https:";
    } else if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      return null;
    }
    parsed.pathname = pathname;
    parsed.search = "";
    parsed.hash = "";
    return parsed.toString();
  } catch {
    return null;
  }
}

export const LOOPBACK_PAIRING_MESSAGE =
  "This pairing code points at 127.0.0.1, which on a phone is the phone itself, not your computer. Run mewla pair on the computer again; with remote access connected it shows a code that works from anywhere.";

function buildPairingNetworkError(pairURL: string, error: unknown): Error {
  const rawMessage = error instanceof Error ? error.message.trim() : "";
  const healthURL = buildHTTPURL(pairURL, "/health");

  try {
    const parsed = new URL(pairURL);
    // A loopback code reaches a phone only through a developer port forward
    // (adb reverse); when that fails, the code itself is the problem.
    if (["127.0.0.1", "localhost", "[::1]"].includes(parsed.hostname)) {
      return new Error(LOOPBACK_PAIRING_MESSAGE);
    }
    if (parsed.protocol === "http:") {
      const detail = rawMessage || "Network request failed.";
      return new Error(
        `${detail} The app could not reach ${pairURL}. Confirm that ${healthURL || "the /health endpoint"} opens from the phone browser. If browser access works but the app still fails on Android, the app build may still be blocking cleartext HTTP and needs to be rebuilt after enabling it.`,
      );
    }
  } catch {
    // Fall through to the generic message below.
  }

  if (healthURL) {
    return new Error(
      `${rawMessage || "Network request failed."} The app could not reach ${pairURL}. Confirm that ${healthURL} opens from the phone browser.`,
    );
  }

  return new Error(rawMessage || "Network request failed.");
}
