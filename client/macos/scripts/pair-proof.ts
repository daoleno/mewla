// Pairs a fresh device with a pairing link the way the app does (client/core pairFlow: readPairingLink, pairWith),
// then proves the daemon knows it (a signed /auth-check). The phone QR of Mewla for Mac encodes such a link.
//   bun client/macos/scripts/pair-proof.ts 'mewla://settings?p=…'
import { confirmPairedDevice } from "../../core/pairing";
import { pairWith, readPairingLink } from "../../core/pairFlow";
import { installMemoryPlatform } from "../../core/testing/memoryPlatform";

const link = process.argv[2] ?? "";
installMemoryPlatform({ deviceName: () => "Pair proof", fetch: (input, init) => fetch(input, init) });
const payload = readPairingLink(link);
if (!payload) throw new Error("not a pairing link");
const paired = await pairWith(payload);
const confirmed = await confirmPairedDevice({ serverUrl: paired.url, daemonId: paired.daemonId, daemonPublicKey: paired.daemonPublicKey });
if (!confirmed) throw new Error(`paired with ${paired.url}, but the daemon does not know the device`);
console.log(JSON.stringify({ paired: true, url: paired.url, daemonId: paired.daemonId, confirmed: true }));
