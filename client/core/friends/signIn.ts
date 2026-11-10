import type { LoginStart } from "./requests";

/** What the sign-in loop needs: the computer's sign-in, a clock and a way to wait. */
export interface SignInDeps {
  start(): Promise<LoginStart>;
  poll(loginId: string): Promise<boolean>;
  now(): number;
  wait(ms: number): Promise<void>;
  /** The code to show, or null once it is no longer wanted. */
  show(start: LoginStart | null): void;
  /** False once a newer turn-on, a cancel or a computer switch made this one stale. */
  current(): boolean;
}

/** How long between polls: what the service asks, never under 2 s. */
export function pollInterval(start: Pick<LoginStart, "interval_seconds">): number {
  return Math.max(2, start.interval_seconds || 5) * 1000;
}

/**
 * Signs the computer in to its Mewla account with a code the person enters on the
 * sign-in page: shows the code, polls until it is done or expired. Resolves true once
 * signed in, false when it was cancelled; rejects when the code expired.
 */
export async function signInComputer(deps: SignInDeps): Promise<boolean> {
  const start = await deps.start();
  if (!deps.current()) return false;
  deps.show(start);
  try {
    const interval = pollInterval(start);
    for (;;) {
      await deps.wait(interval);
      if (!deps.current()) return false;
      if (start.expires_at_ms && deps.now() > start.expires_at_ms) throw new Error("The sign-in code expired; try again");
      if (await deps.poll(start.login_id)) return deps.current();
      if (!deps.current()) return false;
    }
  } finally {
    if (deps.current()) deps.show(null);
  }
}
