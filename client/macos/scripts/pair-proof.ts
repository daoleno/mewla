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
if (!confirmed) throw new Error("daemon did not recognize the paired device");
console.log("Original device paired and authenticated.");

const mode = process.argv[3];
if (mode === "--across-takeover" || mode === "--rollback" || mode === "--reopen") {
  const home = process.env.HOME!;
  const domain = `gui/${process.getuid!()}`;
  const listing = (label: string) => spawnSync("/bin/launchctl", ["print", `${domain}/${label}`], { encoding: "utf8", timeout: 2500 }).stdout ?? "";
  const auth = () => confirmPairedDevice({ serverUrl: paired.url, daemonId: paired.daemonId, daemonPublicKey: paired.daemonPublicKey }).catch(() => null);
  const wait = async (condition: () => boolean | Promise<boolean>, seconds: number, message: string) => {
    const deadline = Date.now() + seconds * 1000;
    while (!(await condition())) {
      if (Date.now() >= deadline) throw new Error(message);
      await Bun.sleep(500);
    }
  };
  const { existsSync, lstatSync, readlinkSync, writeFileSync } = await import("node:fs");
  const oldJob = home + "/Library/LaunchAgents/com.daoleno.mewla.daemon.plist";
  const priorBundle = home + "/Applications/Mewla.app";
  execFileSync("/usr/bin/open", ["-g", "/Applications/Mewla.app"]);
  const installed = () => listing("com.daoleno.mewla.installed-daemon");
  const errFile = process.env.RUNNER_TEMP + "/trial-helper.stderr";
  writeFileSync(errFile, "", { mode: 0o600 });
  const helper = "/Applications/Mewla.app/Contents/Helpers/mewla";
  // The job's program: the app's own executable, which runs the helper as its child (client/macos/src/host.rs).
  const host = "/Applications/Mewla.app/Contents/MacOS/Mewla";
  const version = JSON.parse(execFileSync(helper, ["version", "--json"], { encoding: "utf8" })).version;
  const health = async () => fetch("http://127.0.0.1:9876/health", { signal: AbortSignal.timeout(1000) })
    .then(r => r.json()).catch(() => null);
  const serves = async () => {
    const s = installed();
    const pid = s.match(/^\tpid = (\d+)$/m)?.[1];
    if (!pid || !s.includes("\n\tstate = running\n") || !s.includes("program = " + host)) return false;
    // The listener is the helper, the job's child.
    const listeners = spawnSync("/usr/sbin/lsof", ["-nP", "-iTCP:9876", "-sTCP:LISTEN", "-t"], { encoding: "utf8", timeout: 2500 }).stdout?.split(/\s+/).filter(Boolean) ?? [];
    const childOfJob = (listener: string) => spawnSync("/bin/ps", ["-o", "ppid=", "-p", listener], { encoding: "utf8", timeout: 2500 }).stdout?.trim() === pid;
    if (listeners.length !== 1 || !childOfJob(listeners[0])) return false;
    const h = await health();
    return h?.status === "ok" && h.version === version && h.daemon_id === paired.daemonId && h.daemon_public_key === paired.daemonPublicKey;
  };
  // The signed app is unchanged. Redirect only this job's next launch on the disposable CI Mac.
  // Print fixed error categories, never raw stderr or program arguments.
  let attached = false;
  let prior = "";
  let lastCapture = "";
  const startupTerms = [
    "Brain root", "already owned", "another Mewla daemon owns", "recover from an interrupted update",
    "open database", "database is locked", "database disk image is malformed", "unable to open database",
    "permission denied", "operation not permitted", "address already in use", "no such file or directory",
    "read-only file system", "no such column", "duplicate column", "migration", "missing", "bind", "listen",
    "initialize", "start model profiles owner", "load executors", "resolve home", "unknown command",
    "flag provided but not defined", "unexpected arguments", "control socket", "launchctl", "acquire",
    "server error", "auth", "brain store", "calendar store", "watcher", "state directory", "sqlite",
    "malformed", "outdated", "daemon", "worktree", "tmux", "http", "keychain", "failed",
  ];
  const probe = setInterval(async () => {
    const s = installed();
    if (s && !attached) {
      const task = Bun.spawn(["/usr/bin/sudo", "-n", "/bin/launchctl", "debug", domain + "/com.daoleno.mewla.installed-daemon", "--stderr", errFile], {stdin:"ignore",stdout:"ignore",stderr:"ignore"});
      attached = true;
      const timer = setTimeout(() => task.kill(), 8000);
      void task.exited.then(code => { clearTimeout(timer); console.log("Trial stderr redirect status", code); });
    }
    if (existsSync(errFile)) {
      const lines = (await Bun.file(errFile).text()).trim().split("\n");
      const tail = lines.at(-1)?.toLowerCase() ?? "";
      const capture = JSON.stringify({ lines: lines.length, lastLineCategories: startupTerms.filter(t => tail.includes(t.toLowerCase())) });
      if (capture !== lastCapture) { console.log("Helper startup diagnostics", capture); lastCapture = capture; }
    }
    const h = await health();
    const argv = s.match(/^\s*arguments = \{\n([\s\S]*?)\n\s*\}/m)?.[1]?.trim().split(/\n\s*/);
    const next = JSON.stringify({
      state: s.match(/^\tstate = (.+)$/m)?.[1] ?? "absent",
      lastExit: s.match(/^\tlast exit code = (.*)$/m)?.[1],
      argv: argv?.map(a => a === helper ? "helper" : a === host ? "host" : a === "--daemon" ? "--daemon" : a === "serve" ? "serve" : "other"),
      version: h?.version, identity: h?.daemon_id === paired.daemonId,
    });
    if (next !== prior) { console.log("Trial observation", next); prior = next; }
  }, 1000);
  probe.unref();
  await wait(serves, 95, "signed helper never owned the healthy listener with its version and original identity");
  if (!existsSync(oldJob) || !existsSync(priorBundle)) throw new Error("previous installation removed before the original phone authenticated");
  if (mode === "--rollback" || mode === "--reopen") {
    if (mode === "--reopen") {
      // Interrupt the trial coordinator on this disposable Mac, leaving both
      // launchd services alone. A normal Quit Apple event may request consent.
      // The app itself, not its executable running the daemon (`Mewla --daemon serve`).
      const app = "^/Applications/Mewla[.]app/Contents/MacOS/Mewla$";
      const pid = execFileSync("/usr/bin/pgrep", ["-f", app], { encoding: "utf8" }).trim();
      if (!/^\d+$/.test(pid)) throw new Error("cannot identify a single installed app process");
      process.kill(Number(pid), "SIGKILL");
      await wait(() => spawnSync("/usr/bin/pgrep", ["-f", app]).status === 1, 30, "app still running after interruption");
      if (!existsSync(home + "/.mewla/mac-app-takeover.pending")) throw new Error("trial was no longer pending");
      execFileSync("/usr/bin/open", ["-g", "/Applications/Mewla.app"]);
    }
    // Keep this original phone offline throughout the trial. Its key stays in memory.
    await wait(() => !existsSync(home + "/.mewla/mac-app-takeover.pending") && !installed()
      && listing("com.daoleno.mewla.daemon").includes("\n\tstate = running\n"), 220, "previous daemon was not restored");
    if (!existsSync(oldJob) || !existsSync(priorBundle) || !(await auth())) throw new Error("original phone could not use the restored installation");
    console.log("Trial withheld phone authentication; previous login job and original phone recovered.");
  } else {
    await wait(async () => await serves() && !!(await auth()) && await serves(), 60, "original phone failed to authenticate to the installed helper");
    await wait(() => !existsSync(oldJob) && !existsSync(priorBundle)
      && existsSync(home + "/.mewla/mac-app-takeover.complete")
      && lstatSync(home + "/.local/bin/mewla", { throwIfNoEntry: false })?.isSymbolicLink()
      && readlinkSync(home + "/.local/bin/mewla") === "/Applications/Mewla.app/Contents/Helpers/mewla", 30, "takeover did not commit after the original phone authenticated");
    console.log("Original phone authenticated before retiring the previous login service and CLI bundle.");
  }
}

if (mode === "--external") {
  const { lstatSync } = await import("node:fs");
  const job = `gui/${process.getuid!()}`;
  const show = (label: string) => spawnSync("/bin/launchctl", ["print", job + "/" + label], { encoding: "utf8" });
  const originalPid = () => show("org.example.mewla").stdout?.match(/^\s*pid = (\d+)$/m)?.[1];
  const old = originalPid();
  if (!old) throw new Error("external daemon missing before launch");
  execFileSync("/usr/bin/open", ["-g", "/Applications/Mewla.app"]);
  for (let i = 0; i < 60; i++) {
    await Bun.sleep(1000);
    const linked = lstatSync(process.env.HOME + "/.local/bin/mewla").isSymbolicLink();
    const registered = ["com.daoleno.mewla.daemon", "com.daoleno.mewla.installed-daemon"].some(s => show(s).status === 0);
    const sameProcess = old === originalPid();
    if (linked || registered || !sameProcess) {
      throw new Error(JSON.stringify({linked, registered, sameProcess, elapsedSeconds: i + 1}));
    }
  }
  if (!(await confirmPairedDevice({serverUrl: paired.url, daemonId: paired.daemonId, daemonPublicKey: paired.daemonPublicKey}))) throw new Error("external phone lost access");
  console.log("Original external process, CLI and paired phone retained.");
}
