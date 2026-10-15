import * as Crypto from "expo-crypto";
import { getOrCreateLocalDeviceIdentity, type LocalDeviceIdentity } from "./auth";
import { signDeviceAuthorization } from "./deviceAuthContract";
import { buildHTTPURL } from "./pairing";
import { bytesToHex } from "./protocolCrypto";
import { resolveStoredServerURL } from "./pinnedTransport";
import type { StoredServer } from "./storage";

const REVOKE_TIMEOUT_MS = 3_000;

// revokeThisDevice asks the computer to forget this phone before the app
// removes it, so a removed computer stops trusting the phone. It is best
// effort and bounded: an offline computer never blocks the removal, and the
// owner can still revoke with `mewla devices revoke` there.
export async function revokeThisDevice(
  server: StoredServer,
  loadIdentity: () => Promise<LocalDeviceIdentity> = getOrCreateLocalDeviceIdentity,
): Promise<boolean> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), REVOKE_TIMEOUT_MS);
  try {
    const work = (async () => {
      const identity = await loadIdentity();
      const target = await Crypto.digestStringAsync(
        Crypto.CryptoDigestAlgorithm.SHA256,
        identity.deviceId,
      );
      const purpose = `mewla-device-admin:revoke:DELETE:/devices:sha256=${target.toLowerCase()}`;
      const devicesURL = buildHTTPURL(await resolveStoredServerURL(server), "/devices");
      if (!devicesURL) return false;
      const response = await fetch(devicesURL, {
        method: "DELETE",
        signal: controller.signal,
        headers: {
          "Content-Type": "application/json",
          Authorization: signDeviceAuthorization({
            purpose,
            daemonId: server.daemonId || "",
            deviceId: identity.deviceId,
            seedHex: identity.seedHex,
            timestamp: Date.now().toString(),
            nonceHex: bytesToHex(Crypto.getRandomBytes(16)),
          }),
        },
        body: JSON.stringify({ device_id: identity.deviceId }),
      });
      return response.ok;
    })();
    const timeout = new Promise<boolean>((resolve) =>
      controller.signal.addEventListener("abort", () => resolve(false)),
    );
    return await Promise.race([work.catch(() => false), timeout]);
  } finally {
    clearTimeout(timer);
  }
}
