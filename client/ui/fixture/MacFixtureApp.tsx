// Mewla for Mac on the soft-final mock's scenario (fixture/scenario.ts): a busy desk for the Mac's fidelity captures
// (macos-fidelity.yml), with the same content every run. Built only by `client/macos/package.sh --dev --fixture`, as
// Mewla Dev; the app never imports it.
//
// The scripted computer (workbench) is this Mac: `app.pairing` answers with its key, so the app joins it as its own
// daemon (mac/local.ts `joinLocalDaemon`) instead of pairing the Dev daemon the host started.
import "./seed";
import { host } from "nsh";
import "../App";
import { installPlatform, platform } from "../../core/platform";
import { FakeDaemon } from "../testing/fakeDaemon";
import { scenario, storage } from "./scenario";

const workbench = (JSON.parse(storage["mewla:v3:servers"] ?? "[]") as { daemonId: string; daemonPublicKey: string }[])[0];
const qr = { size: 21, rows: Array.from({ length: 21 }, (_, y) => Array.from({ length: 21 }, (_, x) => ((x * 7 + y * 3) % 5 < 2 ? "1" : "0")).join("")) };
const pairing = {
  expires_at: new Date(Date.now() + 15 * 60_000).toISOString(),
  daemon_id: workbench?.daemonId ?? "",
  daemon_public_key: workbench?.daemonPublicKey ?? "",
  phone_address: "http://192.168.1.20:9876",
  phone_link: "mewla://settings?p=phone",
  phone_qr: qr,
  wifi_address: "http://192.168.1.30:9876",
  wifi_link: "mewla://settings?p=wifi",
  wifi_qr: qr,
};
const call = host.call.bind(host);
(host as { call: typeof host.call }).call = ((name: string, ...rest: unknown[]) =>
  name === "app.pairing" ? Promise.resolve(pairing) : (call as (...args: unknown[]) => Promise<unknown>)(name, ...rest)) as typeof host.call;

const daemon = new FakeDaemon({ ...scenario(), admitsInput: { reply: "On it. I will tell you when the pricing page is ready." } });
installPlatform({ ...platform(), openSocket: () => daemon.open() });
host.on<Record<string, unknown>>("app.daemonPush", (frame) => daemon.push(frame as { type: string }));
