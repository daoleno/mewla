import { effect, lifecycle, platform, signal, Storage, untracked } from "nsh";
import { failureText } from "../../../core/more/daemon";
import { platform as corePlatform } from "../../../core/platform";
import { appIdFrom, getUsage, osMajor, sendAppDaily, setUsage, usageDay, usagePlatform, validAppId } from "../../../core/settings/usage";
import manifest from "../../nsh.json";
import { daemon, daemonState } from "../daemon";

/**
 * Anonymous usage counts on the current computer: the one switch in Settings (the computer
 * holds it, for itself and its phones), and this app's once-a-day count, sent through that
 * computer. A switch of computer drops what was known and reads the next one.
 */
export interface UsageSwitchState {
  /** null until the computer answered (and while it runs a Mewla without counts). */
  enabled: boolean | null;
  locked: string;
  busy: boolean;
  error: string;
}

const EMPTY: UsageSwitchState = { enabled: null, locked: "", busy: false, error: "" };
export const usageState = signal<UsageSwitchState>(EMPTY);
let owner = "";

export function readUsage(): void {
  const server = daemon()?.server;
  if (!server) return;
  void getUsage(server)
    .then((answer) => daemon()?.server.id === server.id && usageState.set({ enabled: answer.enabled, locked: answer.locked, busy: false, error: "" }))
    // An older Mewla has no counts: the switch stays hidden.
    .catch(() => daemon()?.server.id === server.id && usageState.set(EMPTY));
}

export function chooseUsage(on: boolean): void {
  const server = daemon()?.server;
  if (!server || usageState().busy) return;
  usageState.set({ ...usageState(), busy: true, error: "" });
  void setUsage(server, on)
    .then((answer) => daemon()?.server.id === server.id && usageState.set({ enabled: answer.enabled, locked: answer.locked, busy: false, error: "" }))
    .catch((failure) => daemon()?.server.id === server.id && usageState.set({ ...usageState(), busy: false, error: failureText(failure, "That didn't finish") }));
}

/** Call in a component: reads the current computer's switch now and after every switch of computer. */
export function followUsage(): void {
  effect(() => {
    const id = daemon()?.server.id ?? "";
    untracked(() => {
      if (id !== owner) {
        owner = id;
        usageState.set(EMPTY);
      }
      readUsage();
    });
  });
}

/** Why counts are off whatever the switch says, in the person's words; "" when the switch decides. */
export function usageLockedText(locked: string): string {
  switch (locked) {
    case "":
      return "";
    case "environment":
      return "Off on this computer by MEWLA_TELEMETRY or DO_NOT_TRACK";
    case "development":
      return "Off: this computer runs a development build";
    default:
      return "Off on this computer";
  }
}

const APP_ID_KEY = "mewla:usage_app_id";
const dayKey = (serverId: string) => `mewla:usage_day:${serverId}`;

/** Sends this app's count to the current computer once a UTC day, when it is connected. */
async function sendAppDayOnce(): Promise<void> {
  const server = daemon()?.server;
  if (!server) return;
  const today = usageDay(new Date());
  if ((await Storage.getItem(dayKey(server.id))) === today) return;
  let app = await Storage.getItem(APP_ID_KEY);
  if (!validAppId(app)) {
    app = appIdFrom(corePlatform().randomBytes(16));
    await Storage.setItem(APP_ID_KEY, app);
  }
  await sendAppDaily(server, { app, platform: usagePlatform(platform.os), os_major: osMajor(platform.version ?? ""), version: manifest.version });
  await Storage.setItem(dayKey(server.id), today);
}

let following = false;

/** Once, at start: the app's day goes to whichever computer is current and connected. */
export function followAppDaily(): void {
  if (following) return;
  following = true;
  effect(() => {
    if (daemonState() !== "connected" || lifecycle() !== "active") return;
    // An older computer refuses it; the next day tries again.
    untracked(() => void sendAppDayOnce().catch(() => {}));
  });
}
