import nacl from "./vendor/nacl";
import { platform } from "./platform";
import { bytesToHex, hexToBytes, normalizeFixedHex } from "./protocolCrypto";
import { normalizeDaemonId, signDeviceAuthorization } from "./deviceAuthContract";

export {
  bytesToHex,
  hexToBytes,
  verifyLinkPairingSignature,
} from "./protocolCrypto";
export {
  buildServerAssertionPayload,
  buildSignaturePayload,
  normalizeDaemonId,
  normalizePublicKeyHex,
  verifyDaemonAssertion,
} from "./deviceAuthContract";
export type { DaemonAssertionInput } from "./deviceAuthContract";

const DEVICE_ID_KEY = "mewla.device.v3.id";
const DEVICE_NAME_KEY = "mewla.device.v3.name";
const DEVICE_SEED_KEY = "mewla.device.v3.seed";
const DEVICE_PUBLIC_KEY_KEY = "mewla.device.v3.public-key";

export interface LocalDeviceIdentity {
  deviceId: string;
  deviceName: string;
  publicKeyHex: string;
  seedHex: string;
}

export type AuthPurpose =
  | "mewla-connect"
  | "mewla-browser"
  | "mewla-screen"
  | "mewla-screen-view"
  | "mewla-display"
  | "mewla-display-view"
  | "mewla-phone-device"
  | "mewla-upload"
  | "mewla-enrollment:decision:POST:/enrollment/decision"
  | "mewla-probe"
  | "mewla-session-file";

export function normalizePairingToken(
  rawValue: string | null | undefined,
): string {
  return normalizeFixedHex(rawValue, 64);
}

export async function getOrCreateLocalDeviceIdentity(): Promise<LocalDeviceIdentity> {
  const [storedDeviceId, storedName, storedSeedHex, storedPublicKeyHex] =
    await Promise.all([
      platform().secure.getItem(DEVICE_ID_KEY),
      platform().secure.getItem(DEVICE_NAME_KEY),
      platform().secure.getItem(DEVICE_SEED_KEY),
      platform().secure.getItem(DEVICE_PUBLIC_KEY_KEY),
    ]);

  const normalizedSeedHex = normalizeFixedHex(storedSeedHex, 64);
  const normalizedPublicKeyHex = normalizeFixedHex(storedPublicKeyHex, 64);
  if (storedDeviceId?.trim() && normalizedSeedHex && normalizedPublicKeyHex) {
    return {
      deviceId: storedDeviceId.trim(),
      deviceName: storedName?.trim() || defaultDeviceName(),
      seedHex: normalizedSeedHex,
      publicKeyHex: normalizedPublicKeyHex,
    };
  }

  // Mint once per process: racing first calls, or a secure store that does
  // not hand the key back, must not pair one key and sign with another.
  mintedIdentity ??= mintLocalDeviceIdentity().catch((error) => {
    mintedIdentity = null;
    throw error;
  });
  return mintedIdentity;
}

let mintedIdentity: Promise<LocalDeviceIdentity> | null = null;

async function mintLocalDeviceIdentity(): Promise<LocalDeviceIdentity> {
  const seed = platform().randomBytes(32);
  const keyPair = nacl.sign.keyPair.fromSeed(seed);
  const nextIdentity: LocalDeviceIdentity = {
    deviceId: platform().randomUUID(),
    deviceName: defaultDeviceName(),
    seedHex: bytesToHex(seed),
    publicKeyHex: bytesToHex(keyPair.publicKey),
  };

  await Promise.all([
    platform().secure.setItem(DEVICE_ID_KEY, nextIdentity.deviceId),
    platform().secure.setItem(DEVICE_NAME_KEY, nextIdentity.deviceName),
    platform().secure.setItem(DEVICE_SEED_KEY, nextIdentity.seedHex),
    platform().secure.setItem(DEVICE_PUBLIC_KEY_KEY, nextIdentity.publicKeyHex),
  ]);

  return nextIdentity;
}

export async function buildAuthorizationHeader(input: {
  daemonId: string;
  purpose: AuthPurpose;
}): Promise<string> {
  const daemonId = normalizeDaemonId(input.daemonId);
  if (!daemonId) {
    throw new Error("Missing daemon identity.");
  }

  const identity = await getOrCreateLocalDeviceIdentity();
  const timestamp = Date.now().toString();
  const nonceHex = bytesToHex(platform().randomBytes(16));
  return signDeviceAuthorization({
    purpose: input.purpose,
    daemonId,
    deviceId: identity.deviceId,
    seedHex: identity.seedHex,
    timestamp,
    nonceHex,
  });
}

function defaultDeviceName(): string {
  return platform().deviceName().trim() || "Mewla mobile";
}
