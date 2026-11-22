// Pairs a fresh device with a pairing link the way the app does (client/core pairFlow: readPairingLink, pairWith),
// then proves the daemon knows it (a signed /auth-check). The phone QR of Mewla for Mac encodes such a link.
//   bun client/macos/scripts/pair-proof.ts 'mewla://settings?p=…'
import { confirmPairedDevice } from "../../core/pairing";
import { pairWith, readPairingLink } from "../../core/pairFlow";
import { installMemoryPlatform } from "../../core/testing/memoryPlatform";
import { execFileSync, spawnSync } from "node:child_process";

const link = process.argv[2] ?? "";
installMemoryPlatform({ deviceName: () => "Pair proof", fetch: (input, init) => fetch(input, init) });
const payload = readPairingLink(link);
if (!payload) throw new Error("not a pairing link");
const paired = await pairWith(payload);
const confirmed = await confirmPairedDevice({ serverUrl: paired.url, daemonId: paired.daemonId, daemonPublicKey: paired.daemonPublicKey });
if (!confirmed) throw new Error(`paired with ${paired.url}, but the daemon does not know the device`);
console.log(JSON.stringify({ paired: true, url: paired.url, daemonId: paired.daemonId, confirmed: true }));

if (process.argv[3] === "--across-takeover") {
  // Keep the simulated phone's original key in memory while the installed app takes over.
  execFileSync("/usr/bin/open", ["-g", "/Applications/Mewla.app"]);
  const job = `gui/${process.getuid!()}/com.daoleno.mewla.daemon`;
  const deadline = Date.now() + 200_000;
  while (true) {
    const listing = spawnSync("/bin/launchctl", ["print", job], { encoding: "utf8" }).stdout;
    if (listing.includes("program identifier = Contents/Helpers/mewla") && listing.includes("\n\tstate = running\n")) {
      const retained = await confirmPairedDevice({ serverUrl: paired.url, daemonId: paired.daemonId, daemonPublicKey: paired.daemonPublicKey }).catch(() => null);
      if (retained) break;
    }
    if (Date.now() >= deadline) throw new Error("the original phone could not authenticate to the app's daemon after takeover");
    await Bun.sleep(1000);
  }
  console.log("The original phone authenticated to the app's running daemon after takeover.");
}
