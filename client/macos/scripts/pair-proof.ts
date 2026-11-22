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

const mode = process.argv[3];
if (mode === "--across-takeover" || mode === "--rollback") {
  const home = process.env.HOME!;
  const domain = `gui/${process.getuid!()}`;
  const listing = (label: string) => spawnSync("/bin/launchctl", ["print", `${domain}/${label}`], { encoding: "utf8" }).stdout;
  const auth = () => confirmPairedDevice({ serverUrl: paired.url, daemonId: paired.daemonId, daemonPublicKey: paired.daemonPublicKey }).catch(() => null);
  const wait = async (condition: () => boolean | Promise<boolean>, seconds: number, message: string) => {
    const deadline = Date.now() + seconds * 1000;
    while (!(await condition())) {
      if (Date.now() >= deadline) throw new Error(message);
      await Bun.sleep(500);
    }
  };
  const { existsSync, readlinkSync } = await import("node:fs");
  const oldJob = home + "/Library/LaunchAgents/com.daoleno.mewla.daemon.plist";
  const priorBundle = home + "/Applications/Mewla.app";
  execFileSync("/usr/bin/open", ["-g", "/Applications/Mewla.app"]);
  const installed = () => listing("com.daoleno.mewla.installed-daemon");
  await wait(() => {
    const s = installed();
    return s.includes("\n\tstate = running\n") && s.includes("program = /Applications/Mewla.app/Contents/Helpers/mewla");
  }, 180, "signed installed helper did not run at its absolute path");
  if (!existsSync(oldJob) || !existsSync(priorBundle)) throw new Error("previous installation removed before the original phone authenticated");
  if (mode === "--rollback") {
    // Keep this original phone offline throughout the trial. Its key stays in memory.
    await wait(() => !existsSync(home + "/.mewla/mac-app-takeover.pending") && !installed()
      && listing("com.daoleno.mewla.daemon").includes("\n\tstate = running\n"), 220, "previous daemon was not restored");
    if (!existsSync(oldJob) || !existsSync(priorBundle) || !(await auth())) throw new Error("original phone could not use the restored installation");
    console.log("Trial withheld phone authentication; previous login job and original phone recovered.");
  } else {
    await wait(async () => !!(await auth()), 60, "original phone failed to authenticate to the installed helper");
    await wait(() => !existsSync(oldJob) && !existsSync(priorBundle)
      && existsSync(home + "/.mewla/mac-app-takeover.complete")
      && readlinkSync(home + "/.local/bin/mewla") === "/Applications/Mewla.app/Contents/Helpers/mewla", 30, "takeover did not commit after the original phone authenticated");
    console.log("Original phone authenticated before retiring the previous login service and CLI bundle.");
  }
}
