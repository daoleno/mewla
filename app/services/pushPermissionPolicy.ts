export interface PushPermissionState {
  granted: boolean;
  canAskAgain: boolean;
}

/**
 * Whether to show the system prompt now. Android reports a never-asked
 * permission as "denied" with canAskAgain, and lets the same denial be asked
 * again, so Mewla keeps its own record and asks exactly once. A person who
 * declined re-enables notifications in system settings.
 */
export function shouldAskForPushPermission(
  state: PushPermissionState,
  alreadyAsked: boolean,
): boolean {
  return !state.granted && state.canAskAgain && !alreadyAsked;
}
