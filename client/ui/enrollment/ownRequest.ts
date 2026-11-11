import { signal, Storage } from "nsh";
import type { BrowserEnrollmentPrompt } from "../../core/enrollment";

/** This browser's own request to be let in (Expo: sessionStorage). */
export const OWN_ENROLLMENT_KEY = "mewla:enrollment:v1";

/**
 * The id of the request this browser made while it waited to be let in. Once a
 * pairing link pairs it instead, that request is still pending on the computer:
 * Approval must not ask this browser (or anyone) to approve it.
 */
export const ownEnrollmentRequest = signal<string | null>(null);

void Storage.getItem(OWN_ENROLLMENT_KEY)
  .then((raw) => {
    const saved = raw ? (JSON.parse(raw) as Partial<BrowserEnrollmentPrompt>) : null;
    if (saved?.requestId && ownEnrollmentRequest() === null) ownEnrollmentRequest.set(saved.requestId);
  })
  .catch(() => {});

export function rememberOwnEnrollment(request: BrowserEnrollmentPrompt): void {
  ownEnrollmentRequest.set(request.requestId);
  void Storage.setItem(OWN_ENROLLMENT_KEY, JSON.stringify(request)).catch(() => {});
}

export function forgetOwnEnrollment(): void {
  ownEnrollmentRequest.set(null);
  void Storage.removeItem(OWN_ENROLLMENT_KEY).catch(() => {});
}
