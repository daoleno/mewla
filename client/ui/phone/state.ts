import { effect, host, platform, signal, untracked, lifecycle } from "nsh";
import { failureText } from "../../core/more/daemon";
import { changePhone } from "../../core/phone/api";
import { phoneLinkTarget, sameTarget, type PhoneLinkTarget } from "../../core/phone/link";
import { phoneAgentWorking, phoneLabsValue } from "../../core/phone/setup";
import { parsePhoneStatus, type PhoneStatus } from "../../core/phone/status";
import { serverURLNow } from "../../core/link/pinnedTransport";
import { current, ready, transportPath } from "../session";

/**
 * Phone use: an agent on the current computer drives this phone. Android's GitHub APK only: the
 * host's `phone.*` (client/android/phone behind src/github's PhoneHost). Its link to the computer
 * lives in the host's foreground service, so it outlives this JavaScript; the app tells it which
 * computer to follow (the one current computer, rebound on every switch), turns the computer's own
 * switch on, and shows the setup and the agent's screen.
 */

/** The host's live checks and state; null until read, and on a build without Phone use. */
export const phoneStatus = signal<PhoneStatus | null>(null);
/** This build and this Android can do Phone use: the GitHub APK on Android 11 or later. */
export const phoneAvailable = signal(false);
/** The computer did not take its Phone use switch (its words). */
export const phoneError = signal("");
/** The guided setup (pairing) is running. */
export const phonePairing = signal(false);
/** The helper is being started because the person asked. */
export const phoneStarting = signal(false);

/** Settings → Labs → Phone use: Off, Set up, Ready or You are in control. */
export function phoneLabsWord(): string {
  return phoneLabsValue(phoneStatus());
}

let shown = "";
function apply(value: unknown): void {
  const next = parsePhoneStatus(value);
  if (!next) return;
  // The poll answers the same status most of the time: the pages redraw only on a change.
  const key = JSON.stringify(next);
  if (key !== shown) {
    shown = key;
    phoneStatus.set(next);
  }
  phoneAvailable.set(next.supported);
}

const hosted = () => platform.os === "android" && host.available();

/** Asks the host for the live checks; a build without Phone use answers nothing. */
export async function readPhone(): Promise<void> {
  if (!hosted()) return;
  try {
    apply(await host.call("phone.status"));
  } catch {
    phoneAvailable.set(false);
  }
}

// The checks change when the person comes back from Settings; poll fast only while setup is on screen.
let watchers = 0;
let poll: ReturnType<typeof setInterval> | null = null;
function schedulePoll(): void {
  if (poll) clearInterval(poll);
  poll = null;
  if (!phoneAvailable() || lifecycle() !== "active") return;
  poll = setInterval(() => void readPhone(), watchers > 0 ? 1_000 : 10_000);
}

/** Polls the live checks every second while a screen shows them; returns the release. */
export function watchPhoneSetup(): () => void {
  watchers += 1;
  untracked(schedulePoll);
  void readPhone();
  let released = false;
  return () => {
    if (released) return;
    released = true;
    watchers = Math.max(0, watchers - 1);
    untracked(schedulePoll);
  };
}

let started = false;

/**
 * Follows the current computer and the host's status, once for the app's life (called when this
 * module loads, outside any page, so the effects live as long as the app).
 */
export function startPhoneUse(): void {
  if (started || !hosted()) return;
  started = true;
  try {
    host.on("phone.status", (data) => apply(data));
  } catch {
    // A build that does not grant phone.* has no Phone use.
    return;
  }
  void readPhone();

  effect(() => {
    phoneAvailable();
    lifecycle();
    untracked(schedulePoll);
  });
  // Back in front (from Settings, a notification): the checks again.
  effect(() => {
    if (lifecycle() === "active") untracked(() => void readPhone());
  });

  // The link's one computer: the current one, or none. Until the saved computers are read there is
  // nothing to decide, and a link the host brought up after a reboot must not be dropped meanwhile.
  let told: PhoneLinkTarget | null | undefined;
  effect(() => {
    if (!phoneAvailable() || !ready()) return;
    // On Mewla Relay the link rides the tunnel the app chose; a new path tells it again.
    transportPath();
    const server = current();
    const target = phoneLinkTarget(server ? { ...server, url: serverURLNow(server) } : null);
    untracked(() => {
      if (told !== undefined && sameTarget(told, target)) return;
      told = target;
      void host.call("phone.follow", target).catch((error) => console.warn("phone.follow:", error instanceof Error ? error.message : error));
    });
  });

  // The computer's own switch for its Workers, whenever Phone use is on for a computer.
  let switchedFor = "";
  effect(() => {
    const enabled = phoneStatus()?.enabled === true;
    const server = current();
    const key = enabled && server ? server.id : "";
    untracked(() => {
      if (key === switchedFor) return;
      switchedFor = key;
      phoneError.set("");
      if (!key || !server) return;
      void changePhone(server, { enabled: true }).catch((cause) => {
        if (current()?.id === server.id) phoneError.set(failureText(cause, "Not reachable"));
      });
    });
  });

  // While an agent works the phone, leaving the app from any page floats its screen.
  let pip = "";
  effect(() => {
    const status = phoneStatus();
    const on = phoneAgentWorking(status);
    const w = status?.w ?? 0;
    const h = status?.h ?? 0;
    const key = on ? `${w}x${h}` : "";
    untracked(() => {
      if (key === pip) return;
      const was = pip;
      pip = key;
      if (!on && !was) return;
      void host.call("phone.setPipAuto", { on, w, h }).catch(() => {});
    });
  });
}

/** The Phone use switch: asks for notifications (the status and handoff ones), then turns it on. */
export async function turnPhoneOn(): Promise<void> {
  phoneError.set("");
  await host.call("push.requestPermission", undefined, { timeout: 0 }).catch(() => undefined);
  apply(await host.call("phone.setEnabled", { enabled: true }));
}

export async function turnPhoneOff(): Promise<void> {
  apply(await host.call("phone.setEnabled", { enabled: false }));
  const server = current();
  if (server) await changePhone(server, { enabled: false }).catch(() => undefined);
}

/** The guided setup: Mewla walks Settings and reads the pairing code. Resolves whether it paired. */
export async function pairPhone(): Promise<boolean> {
  phonePairing.set(true);
  try {
    return (await host.call<boolean>("phone.pair", {}, { timeout: 0 })) === true;
  } catch {
    return false;
  } finally {
    phonePairing.set(false);
    void readPhone();
  }
}

/** Starts the helper; the status says why when it does not come up. */
export async function startPhoneHelper(): Promise<void> {
  if (phoneStarting()) return;
  phoneStarting.set(true);
  try {
    await host.call("phone.ensureHelper", undefined, { timeout: 0 });
  } catch {
    // The status's problem says why.
  } finally {
    phoneStarting.set(false);
    void readPhone();
  }
}

/** The system screens setup sends the person to. */
export function openPhoneScreen(screen: "developer" | "wireless" | "accessibility" | "app"): void {
  void host.call("phone.open", { screen }).catch(() => {});
}

/** Silent when the secure-settings grant exists; otherwise the system's page for Mewla's service. */
export async function allowPhoneAccessibility(): Promise<void> {
  const enabled = await host.call<boolean>("phone.enableAccessibility").catch(() => false);
  if (!enabled) openPhoneScreen("accessibility");
  void readPhone();
}

export type PhoneControl = "takeOver" | "giveBack" | "stop" | "handoffDone";

/** Take over, Give back (also Resume), Stop, Done. */
export function phoneControl(control: PhoneControl): void {
  void host.call(`phone.${control}`).catch(() => {});
}

/** Floats the agent's screen in a small window over other apps (picture-in-picture). */
export function floatPhone(): void {
  const status = phoneStatus();
  void host.call("phone.enterPip", { w: status?.w ?? 0, h: status?.h ?? 0 }).catch(() => {});
}

startPhoneUse();
