/**
 * What the QR scanner shows for the camera's permission. A scanner that has no
 * answer yet says so, one that can still ask asks (the first use), and one the
 * person turned off points to Settings: none of them is an empty dark window.
 */
export type CameraGate = "loading" | "request" | "denied" | "camera";

export function cameraGate(
  permission: { granted: boolean; canAskAgain: boolean } | null | undefined,
): CameraGate {
  if (!permission) return "loading";
  if (permission.granted) return "camera";
  return permission.canAskAgain ? "request" : "denied";
}

/**
 * The camera may run unless the app is in the background. iOS reports
 * "inactive" while its own permission dialog is up and while the app
 * switcher is open; treating that as "not in front" left the viewfinder
 * unmounted after the person tapped Allow.
 */
export function cameraMayRun(appState: string): boolean {
  return appState !== "background";
}
