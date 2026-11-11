// A computer that answers `/pair` the way the daemon does, for `nsh test` only: its pairing link and a
// signed answer (the identity proof enrollWithDaemon checks).
import nacl from "../../core/vendor/nacl";
import { bytesToHex } from "../../core/protocolCrypto";
import { buildServerAssertionPayload } from "../../core/deviceAuthContract";

export function testComputer(address = "http://10.0.0.5:9876", seed = 7) {
  const keys = nacl.sign.keyPair.fromSeed(new Uint8Array(32).fill(seed));
  const publicKey = bytesToHex(keys.publicKey);
  const daemonId = "a1".repeat(32);
  return {
    publicKey,
    pairURL: `POST ${address}/pair`,
    link: (name = "Desk") => `mewla://settings?u=${encodeURIComponent(address)}&k=${publicKey}&t=${"c".repeat(64)}&n=${name}`,
    /** The daemon's answer to a good code. */
    answer() {
      const timestamp = new Date().toISOString();
      const nonce = "5".repeat(32);
      const signature = nacl.sign.detached(buildServerAssertionPayload("mewla-pair", daemonId, timestamp, nonce), keys.secretKey);
      return {
        status: 200,
        body: { daemon_id: daemonId, daemon_public_key: publicKey, assertion_timestamp: timestamp, assertion_nonce: nonce, assertion_signature: bytesToHex(signature) },
      };
    },
  };
}
