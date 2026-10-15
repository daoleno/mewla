import type { CatBox } from "../mewla/launchTiming";

/**
 * First run on a phone: what Mewla is, the one command for the computer, the
 * code it prints, and the moment the phone is connected.
 */
export type OnboardingStep = "welcome" | "install" | "scan" | "connected";

// Copy follows the homepage (site/index.html); onboardingCopy.test.ts keeps them in step.
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

interface Frame {
  width: number;
  height: number;
  top: number;
  bottom: number;
}

const HERO_SIZE = 164;
const CORNER_SIZE = 88;

/**
 * Where the cat stands on the welcome and connected steps. The launch veil
 * flies the waking cat here, so both must use this one function with the
 * same frame: the full screen and its safe-area insets.
 */
export function heroCatBox({ width, height, top, bottom }: Frame): CatBox {
  const usable = height - top - bottom;
  return { cx: width / 2, cy: top + Math.max(132, Math.round(usable * 0.27)), size: HERO_SIZE };
}

/** The cat keeping watch beside the title on the install and scan steps. */
export function cornerCatBox({ width, top }: Frame): CatBox {
  return { cx: width - 20 - CORNER_SIZE / 2, cy: top + 14 + CORNER_SIZE / 2, size: CORNER_SIZE };
}

export function catBoxForStep(step: OnboardingStep, frame: Frame): CatBox {
  return step === "install" || step === "scan" ? cornerCatBox(frame) : heroCatBox(frame);
}
