/**
 * First run on a phone: what Mewla is, the one command for the computer, the code it prints,
 * and the moment the phone is connected. Plain TS: the screen (ui/screens/Onboarding.tsx) only draws it.
 */
export type OnboardingStep = "welcome" | "install" | "scan" | "connected";

// Copy follows the homepage (site/index.html); onboarding.test.ts keeps them in step.
export const WELCOME_TITLE = "Naps all day. Ships all night.";
export const WELCOME_LEDE = "Tell Brain the goal. It leads a team of agents on your own computer, and calls you only when it matters.";
export const INSTALL_COMMAND = "curl -fsSL https://mewla.ai/install.sh | sh";
export const INSTALL_NOTE = "Checksum-verified · no sudo · no telemetry";
export const START_COMMAND = "mewla";
export const START_NOTE = "Pick 1 and sign in with GitHub.";
export const OTHER_WAYS_URL = "https://mewla.ai/docs/connect-and-pair/#choose-an-address";

/** The step Back returns to, or null when Back should leave the screen. */
export function previousOnboardingStep(step: OnboardingStep, cameFrom: OnboardingStep | null): OnboardingStep | null {
  if (step === "install") return "welcome";
  if (step === "scan") return cameFrom === "welcome" ? "welcome" : "install";
  return null;
}

/** Brain's cat on each step (DESIGN.md, the cat's state map). */
export type CatClip = "attention" | "working" | "delegating" | "offline";

export function catClipFor(step: OnboardingStep, pairing: boolean, connection: string | undefined): CatClip {
  if (step === "welcome") return "attention";
  if (step === "install") return "working";
  if (step === "scan") return pairing ? "attention" : "delegating";
  if (connection === "connected") return "attention";
  return connection === "connecting" || connection === "idle" ? "delegating" : "offline";
}

/** Whether the connected step's seal stamps in behind the cat: on Welcome, and once Brain is home. */
export function sealShown(step: OnboardingStep, connection: string | undefined): boolean {
  return step === "welcome" || (step === "connected" && connection === "connected");
}

// A computer added by address is named by it ("10.0.0.5"); sentences need a name a person would use.
const ADDRESS = /^(localhost|\d{1,3}(\.\d{1,3}){3}|\[?[0-9a-f]*:[0-9a-f:.]*\]?)(:\d+)?$/i;

export function computerName(name: string | null | undefined, fallback = "your computer"): string {
  const trimmed = name?.trim();
  return trimmed && !ADDRESS.test(trimmed) ? trimmed : fallback;
}

/** The connected step's words for the new computer's connection. */
export function connectedCopy(connection: string, name: string, problem: string): { title: string; lede: string } {
  if (connection === "connected") return { title: "Brain is home.", lede: `Connected to ${name}.` };
  if (connection === "connecting" || connection === "idle") return { title: "Connecting…", lede: name };
  return { title: "Can't reach your computer", lede: personProblem(problem) || `Check that mewla is running on ${name}.` };
}

/**
 * The transport's own words for an end ("error", "silent", "closed (1006)", "direct-available")
 * are for logs; only a reason the computer gave when it closed is shown to the person.
 */
function personProblem(problem: string): string {
  const text = problem.trim();
  return /^(error|silent|direct-available|closed \(\d+\))$/.test(text) ? "" : text;
}

// ---- Layout -------------------------------------------------------------------------------

export interface Frame {
  width: number;
  height: number;
  top: number;
  bottom: number;
}

export interface CatBox {
  cx: number;
  cy: number;
  size: number;
}

const HERO_SIZE = 164;
const CORNER_SIZE = 88;
/** Below this usable height (a phone on its side, a foldable's cover screen) the steps sit in two columns. */
const COMPACT_HEIGHT = 520;
const COMPACT_ZONE_MAX = 260;

/** True when the window is too short for the cat above the text. */
export function isCompactFrame({ height, top, bottom }: Frame): boolean {
  return height - top - bottom < COMPACT_HEIGHT;
}

/** Width of the cat's column in a compact welcome or connected step; 0 otherwise. */
export function heroZoneWidth(frame: Frame): number {
  return isCompactFrame(frame) ? Math.round(Math.min(frame.width * 0.4, COMPACT_ZONE_MAX)) : 0;
}

/** Where the cat stands on the welcome and connected steps; short windows give it a column of its own. */
export function heroCatBox(frame: Frame): CatBox {
  const { width, height, top, bottom } = frame;
  const usable = height - top - bottom;
  if (isCompactFrame(frame)) {
    return { cx: heroZoneWidth(frame) / 2, cy: top + usable / 2, size: Math.min(HERO_SIZE, usable - 32) };
  }
  return { cx: width / 2, cy: top + Math.max(132, Math.round(usable * 0.27)), size: HERO_SIZE };
}

/** The cat keeping watch beside the title on the install and scan steps. */
export function cornerCatBox({ width, top }: Frame): CatBox {
  return { cx: width - 20 - CORNER_SIZE / 2, cy: top + 14 + CORNER_SIZE / 2, size: CORNER_SIZE };
}

export function catBoxForStep(step: OnboardingStep, frame: Frame): CatBox {
  return step === "install" || step === "scan" ? cornerCatBox(frame) : heroCatBox(frame);
}

/** Where the hero steps' text starts: below the cat, or at the top beside it when compact. */
export function belowHero(frame: Frame): number {
  if (isCompactFrame(frame)) return 0;
  const hero = heroCatBox(frame);
  return hero.cy + hero.size / 2 + 28 - frame.top;
}

/** How tall the install and scan headers are, so the title clears the corner cat. */
export function headerHeight(frame: Frame): number {
  if (isCompactFrame(frame)) return 0;
  const corner = cornerCatBox(frame);
  return corner.cy + corner.size / 2 - frame.top;
}

// ---- The camera ---------------------------------------------------------------------------

/** `scan.permission` / `scan.request`: what the system says about the camera. */
export type CameraPermission = "granted" | "undetermined" | "denied";

export function readCameraPermission(value: unknown): CameraPermission | null {
  return value === "granted" || value === "undetermined" || value === "denied" ? value : null;
}

/**
 * What the viewfinder shows. One that has no answer yet says so, one that can still ask asks,
 * one the person turned off points to Settings, one that failed says so: none is a dark window
 * that promises a camera.
 */
export type FinderState = "loading" | "request" | "denied" | "failed" | "camera";

export function finderState(permission: CameraPermission | null, failed: boolean, asking: boolean): FinderState {
  if (permission === null || asking) return "loading";
  if (permission === "denied") return "denied";
  if (permission === "undetermined") return "request";
  return failed ? "failed" : "camera";
}

/** "Looking for a Mewla code…" only while the camera really looks: allowed, running, the app in front. */
export function cameraSearching(state: FinderState, foreground: boolean, pairing: boolean): boolean {
  return state === "camera" && foreground && !pairing;
}

// ---- One pairing at a time ----------------------------------------------------------------

/**
 * Camera frames, Paste and opened links share one latch: one import at a time. A code that was
 * refused stays remembered, so the same code still in view is reported once, not every frame.
 */
export class PairLatch {
  private held = false;
  private refused = "";

  /** True when this value may start a pairing now. */
  claim(value: string, fromCamera: boolean): boolean {
    if (this.held) return false;
    if (fromCamera && value === this.refused) return false;
    this.held = true;
    return true;
  }

  release(refused?: string): void {
    this.held = false;
    if (refused !== undefined) this.refused = refused;
  }

  get busy(): boolean {
    return this.held;
  }
}
