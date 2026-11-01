import { PairingCancelledError } from "./pairingScope";

export interface ImportToast {
  title: string;
  detail?: string;
  tone: "info" | "error";
}

/**
 * What a pairing link opened from outside the app (the system Camera, a tapped
 * link) tells the person. The in-app scanner shows its own progress and errors;
 * a link that arrives as a URL used to say nothing at all while it paired, which
 * over a slow route is a minute, and nothing when it failed, so it looked like
 * nothing had happened.
 */
export function importProgressToast(message: string): ImportToast {
  return { title: message, tone: "info" };
}

/** Null when the person cancelled; a failure otherwise, with its reason. */
export function importFailureToast(error: unknown): ImportToast | null {
  if (error instanceof PairingCancelledError) return null;
  const detail = error instanceof Error ? error.message.trim() : String(error ?? "").trim();
  return { title: "Couldn't pair this computer", detail: detail || undefined, tone: "error" };
}
