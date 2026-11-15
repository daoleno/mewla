/**
 * Ed25519 through the platform's WebCrypto: NSH runs it natively (ed25519-dalek, on Android and iOS alike), as bun and
 * browsers do. The vendored tweetnacl ran it in the app's interpreter: a signature took ~35 ms and a check ~67 ms on an
 * emulator, several times that on the owner's phone, and coming back to the app signs a connection and checks the
 * computer's answer on every direct address (owner 11-15: tweetnacl's field arithmetic among the 2 s cut-offs).
 * Natively each takes ~2 ms. The bytes are the same: Ed25519 signatures are deterministic. A device's key is still made
 * by tweetnacl (core/auth.ts), once per install, and a pairing link is still checked by it (core/protocolCrypto.ts).
 */
const ED25519 = { name: "Ed25519" } as const;
export const ED25519_SIGNATURE_BYTES = 64;
const KEY_BYTES = 32;
/** A PKCS#8 Ed25519 private key is this prefix and the 32-byte seed (RFC 8410). */
const PKCS8_SEED_PREFIX = [0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x04, 0x22, 0x04, 0x20];

/** The part of WebCrypto used here (NSH's types carry no DOM). */
type Key = object;
interface Subtle {
  importKey(format: "pkcs8" | "raw", data: Uint8Array, algorithm: typeof ED25519, extractable: boolean, usages: string[]): Promise<Key>;
  sign(algorithm: typeof ED25519, key: Key, data: Uint8Array): Promise<ArrayBuffer>;
  verify(algorithm: typeof ED25519, key: Key, signature: Uint8Array, data: Uint8Array): Promise<boolean>;
}
const subtle = (): Subtle => (globalThis.crypto as unknown as { subtle: Subtle }).subtle;
const source = (bytes: Uint8Array): Uint8Array => bytes;

/** The signing key of a seed, imported once: one device has one seed. */
let signing: { seed: string; key: Promise<Key> } | null = null;

function signingKey(seed: Uint8Array): Promise<Key> {
  const id = Array.from(seed, (byte) => byte.toString(16).padStart(2, "0")).join("");
  if (signing?.seed !== id) {
    const key = subtle().importKey("pkcs8", source(new Uint8Array([...PKCS8_SEED_PREFIX, ...seed])), ED25519, false, ["sign"]);
    signing = { seed: id, key };
    key.catch(() => {
      if (signing?.key === key) signing = null;
    });
  }
  return signing.key;
}

/** Signs `message` with the key of a 32-byte seed. */
export async function ed25519Sign(seed: Uint8Array, message: Uint8Array): Promise<Uint8Array> {
  if (seed.length !== KEY_BYTES) throw new Error("An Ed25519 seed is 32 bytes.");
  return new Uint8Array(await subtle().sign(ED25519, await signingKey(seed), source(message)));
}

/** Whether `signature` is `publicKey`'s over `message`; false for anything malformed. */
export async function ed25519Verify(message: Uint8Array, signature: Uint8Array, publicKey: Uint8Array): Promise<boolean> {
  if (signature.length !== ED25519_SIGNATURE_BYTES || publicKey.length !== KEY_BYTES) return false;
  try {
    const key = await subtle().importKey("raw", source(publicKey), ED25519, false, ["verify"]);
    return await subtle().verify(ED25519, key, source(signature), source(message));
  } catch {
    return false;
  }
}
