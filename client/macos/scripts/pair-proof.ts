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
  const errFile = process.env.RUNNER_TEMP + "/trial-helper.stderr";
  let attached = false;
  const snapshot = async () => {
    const s = installed();
    const jobState = s.match(/^\tstate = (.+)$/m)?.[1] ?? "absent";
    const lastExit = s.match(/^\tlast exit code = (.*)$/m)?.[1] ?? "none";
    const jobProgram = s.match(/^\tprogram(?: identifier)? = (.*)$/m)?.[1] ?? "none";
    if (s && !attached) {
      const result = spawnSync("/bin/launchctl", ["debug", domain + "/com.daoleno.mewla.installed-daemon", "--stderr", errFile]);
      attached = result.status === 0;
    }
    if (existsSync(errFile)) {
      const stderr = await Bun.file(errFile).text();
      const errors = stderr.split("\n").filter(l => /^(?:\d{4}\/\d{2}\/\d{2} [\d:]+ )?(?:unknown command|flag provided but not defined|resolve auth storage directory|open |listen |bind:|another daemon|recover from an interrupted update|acquire |cannot |lock |create |Mewla is|mewla is)/i.test(l));
      for (const line of errors.slice(-1)) {
        if (!/token|key|signature|nonce|:\/\//i.test(line)) console.log("Helper startup error", line.slice(0,220));
      }
    }
    const h = await fetch("http://127.0.0.1:9876/health", {signal: AbortSignal.timeout(1000)}).then(r => r.json()).catch(() => null);
    return JSON.stringify({jobState, jobProgram, lastExit, version:h?.version, identity:h?.daemon_id === paired.daemonId});
  };
  let prior = "";
  const probe = setInterval(async () => {
    const next = await snapshot();
    if (next !== prior) { console.log("Trial observation", next); prior = next; }
  }, 1000);
  probe.unref();
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
