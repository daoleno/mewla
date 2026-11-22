// Runs on the clean CI Mac, with the signed app's pairing dialog already open.
import { execFileSync, spawnSync } from "node:child_process";
import { copyFileSync, existsSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { networkInterfaces } from "node:os";
import { join } from "node:path";
import { buildAuthorizationHeader } from "../../core/auth";
import { confirmPairedDevice } from "../../core/pairing";
import { pairWith, readPairingLink } from "../../core/pairFlow";
import { installMemoryPlatform } from "../../core/testing/memoryPlatform";
import { authSubprotocol } from "../../core/webSocketAuth";

const [vision, out] = process.argv.slice(2);
const privateDir = mkdtempSync(join(process.env.RUNNER_TEMP!, "private-phone-"));
const screen = join(privateDir, "display.png");
const cli = "/Applications/Mewla.app/Contents/Helpers/mewla";
const ips = Object.values(networkInterfaces()).flat().filter((entry) => entry?.family === "IPv4" && !entry.internal).map((entry) => entry!.address);
if (!ips.length) throw new Error("CI Mac has no LAN interface");
const wait = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));
async function until(label: string, condition: () => boolean | Promise<boolean>, seconds = 60) {
  const end = Date.now() + seconds * 1000;
  while (Date.now() < end) {
    if (await condition()) return;
    await wait(500);
  }
  throw new Error(label);
}
function snap() {
  execFileSync("screencapture", ["-x", "-D", "1", screen], { timeout: 10_000 });
}
function inspect(mode: string, arg?: string) {
  snap();
  return spawnSync(vision, [screen, mode, ...(arg ? [arg] : [])], { encoding: "utf8", timeout: 12_000 });
}
function osa(script: string) {
  execFileSync("osascript", ["-e", script], { timeout: 20_000, stdio: "ignore" });
}
const esc = () => osa('tell application "System Events" to key code 53');
const fromFile = () => osa('tell application "System Events" to tell process "Mewla" to click menu item "Pair a Phone…" of menu "File" of menu bar item "File" of menu bar 1');
async function httpLAN() {
  for (const ip of ips) {
    try {
      const res = await fetch(`http://${ip}:9876/health`, { signal: AbortSignal.timeout(1500) });
      if (res.ok && (await res.json()).daemon_id) return true;
    } catch { /* The default TLS-only listener refuses plain HTTP. */ }
  }
  return false;
}
async function proveOff(label: string) {
  const response = await fetch("http://127.0.0.1:9876/health", { signal: AbortSignal.timeout(3000) });
  if (!response.ok || await httpLAN()) throw new Error(label);
  console.log(label);
}
async function phoneSocket(url: string, daemonId: string) {
  const auth = await buildAuthorizationHeader({ daemonId, purpose: "mewla-connect" });
  const socket = new WebSocket(url, [authSubprotocol(auth)]);
  await until("phone WebSocket did not open", () => socket.readyState === WebSocket.OPEN, 10);
  return socket;
}
async function roundTrip(socket: WebSocket, id: string) {
  let received = false;
  socket.onmessage = ({ data }) => {
    if (typeof data !== "string") return;
    const frame = JSON.parse(data);
    if (frame.type === "link_pong" && frame.request_id === id) received = true;
  };
  socket.send(JSON.stringify({ type: "link_ping", request_id: id }));
  await until("no authenticated phone pong: " + id, () => received, 10);
}

try {
  await proveOff("Fresh install answers locally with LAN off.");
  await until("no trusted LAN consent from cat menu", () => inspect("consent").status === 0);
  copyFileSync(screen, join(out, "06-pair-consent.png"));
  await until("Cancel unavailable", () => inspect("click", "Cancel").status === 0);
  await until("Cancel left the sheet open", () => inspect("closed").status === 0);
  await proveOff("Cancel kept LAN off.");
  fromFile();
  await until("File did not open trusted LAN consent", () => inspect("consent").status === 0);
  await until("Allow trusted LAN unavailable", () => inspect("click", "Allow trusted LAN").status === 0);
  await until("LAN listener did not open", httpLAN, 120);
  const decodedFile = join(privateDir, "decoded");
  await until("no short-lived scannable code after consent", () => {
    const result = inspect("scan", decodedFile);
    return result.status === 0 && existsSync(decodedFile);
  });
  const scanned = readFileSync(decodedFile, "utf8");
  execFileSync(vision, [screen, "redact", join(out, "06-pair-redacted.png")]);
  installMemoryPlatform({ deviceName: () => "Simulated LAN phone", fetch: (input, init) => fetch(input, init) });
  const target = readPairingLink(scanned);
  if (!target) {
    const url = URL.canParse(scanned) ? new URL(scanned) : null;
    const payload = url?.searchParams.get("p") ?? "";
    const bytes = Buffer.from(payload, "base64url");
    const addressLength = bytes.length >= 3 ? bytes.readUInt16BE(1) : -1;
    console.log(`Decoded QR: url=${!!url}, characters=${scanned.length}, bytes=${bytes.length}, declaredAddressBytes=${addressLength}, version=${bytes[0]}`);
    await until("Copy link unavailable", () => inspect("click", "Copy link").status === 0);
    await wait(500);
    const copied = execFileSync("pbpaste", { encoding: "utf8" }).trim();
    execFileSync("pbcopy", { input: "" });
    throw new Error(`QR rejected: url=${!!url}, bytes=${bytes.length}, declaredAddressBytes=${addressLength}, version=${bytes[0]}, matchesClipboard=${copied === scanned}, clipboardParses=${!!readPairingLink(copied)}`);
  }
  const host = new URL(target.url).hostname;
  if (!ips.includes(host)) throw new Error("Displayed QR hostname " + host + " differs from LAN interfaces " + ips.join(", "));
  const phone = await pairWith(target);
  const socket = await phoneSocket(phone.url, phone.daemonId);
  await roundTrip(socket, "before-dismiss");
  esc();
  await until("pairing sheet still open", () => inspect("closed").status === 0);
  await roundTrip(socket, "after-dismiss");
  console.log("Bun phone client paired through the displayed LAN QR. The same authenticated WebSocket answered after dismissal.");
  fromFile();
  await until("existing phone route did not show its QR directly", () => inspect("qr").status === 0);
  esc();
  socket.close();
  const daemonJob = `gui/${process.getuid!()}/com.daoleno.mewla.daemon`;
  execFileSync("launchctl", ["kickstart", "-k", daemonJob], { timeout: 60_000 });
  const known = { serverUrl: phone.url, daemonId: phone.daemonId, daemonPublicKey: phone.daemonPublicKey };
  await until("old phone did not authenticate after restart", async () => !!(await confirmPairedDevice(known).catch(() => null)), 90);
  const resumed = await phoneSocket(phone.url, phone.daemonId);
  await roundTrip(resumed, "after-restart");
  console.log("Restart retained LAN and the original paired phone authenticated again.");
  osa('tell application "System Events" to keystroke "," using command down');
  try {
    await until("Connections settings did not open", () => {
      if (inspect("connections").status === 0) return true;
      inspect("click", "Connections");
      return false;
    });
    console.log("Settings showed the phone routes on its Connections page.");
    await until("Turn off LAN missing on the Connections page", () => inspect("click", "Turn off LAN").status === 0);
  } catch (error) {
    if (inspect("no-code").status === 0) copyFileSync(screen, join(out, "settings-at-failure.png"));
    throw error;
  }
  await until("LAN stayed open", async () => !await httpLAN() && (await fetch("http://127.0.0.1:9876/health").catch(() => null))?.ok === true, 90);
  await proveOff("Turn off LAN closed the plain HTTP route.");
  execFileSync("launchctl", ["kickstart", "-k", daemonJob], { timeout: 60_000 });
  await until("local daemon did not restart", async () => (await fetch("http://127.0.0.1:9876/health").catch(() => null))?.ok === true);
  await proveOff("LAN remained off after restart.");
  if (!execFileSync(cli, ["devices", "list"], { encoding: "utf8" }).includes("Simulated LAN phone")) throw new Error("turning off LAN lost phone pairing");
} finally {
  rmSync(privateDir, { recursive: true, force: true });
}
