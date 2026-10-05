import * as Crypto from "expo-crypto";
import * as Device from "expo-device";
import { Platform } from "react-native";
import nacl from "tweetnacl";
import { secureStorage } from "./secureStorage";
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

const DEVICE_ID_KEY = "zen.device.v3.id";
const DEVICE_NAME_KEY = "zen.device.v3.name";
const DEVICE_SEED_KEY = "zen.device.v3.seed";
const DEVICE_PUBLIC_KEY_KEY = "zen.device.v3.public-key";

export interface LocalDeviceIdentity {
  deviceId: string;
  deviceName: string;
  publicKeyHex: string;
  seedHex: string;
}

export type AuthPurpose =
  | "zen-connect"
  | "zen-browser"
  | `zen-browser-view:${string}`
  | "zen-upload"
  | "zen-enrollment:decision:POST:/enrollment/decision"
  | "zen-probe"
  | "zen-session-file";

export function normalizePairingToken(
  rawValue: string | null | undefined,
): string {
  return normalizeFixedHex(rawValue, 64);
}

export async function getOrCreateLocalDeviceIdentity(): Promise<LocalDeviceIdentity> {
  const [storedDeviceId, storedName, storedSeedHex, storedPublicKeyHex] =
    await Promise.all([
      secureStorage.getItemAsync(DEVICE_ID_KEY),
      secureStorage.getItemAsync(DEVICE_NAME_KEY),
      secureStorage.getItemAsync(DEVICE_SEED_KEY),
      secureStorage.getItemAsync(DEVICE_PUBLIC_KEY_KEY),
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

  const seed = Crypto.getRandomBytes(32);
  const keyPair = nacl.sign.keyPair.fromSeed(seed);
  const nextIdentity: LocalDeviceIdentity = {
    deviceId: Crypto.randomUUID(),
    deviceName: defaultDeviceName(),
    seedHex: bytesToHex(seed),
    publicKeyHex: bytesToHex(keyPair.publicKey),
  };

  await Promise.all([
    secureStorage.setItemAsync(DEVICE_ID_KEY, nextIdentity.deviceId),
    secureStorage.setItemAsync(DEVICE_NAME_KEY, nextIdentity.deviceName),
    secureStorage.setItemAsync(DEVICE_SEED_KEY, nextIdentity.seedHex),
    secureStorage.setItemAsync(DEVICE_PUBLIC_KEY_KEY, nextIdentity.publicKeyHex),
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
  const nonceHex = bytesToHex(Crypto.getRandomBytes(16));
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
  if (Platform.OS === "web") {
    const os = Device.osName?.trim();
    return os ? `Zen Web (${os})` : "Zen Web";
  }
  return Device.deviceName?.trim() || Device.modelName?.trim() || "Zen mobile";
}
