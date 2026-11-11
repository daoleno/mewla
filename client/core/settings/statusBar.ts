/**
 * The status bar's content color on a page, from the app's own appearance: dark icons on the light
 * paper, light ones on the dark. Every page states it: left to NSH's "auto", Android kept the light
 * icons of a Dark page after the app went back to Light (FAILURES Round 4 #7: clock, Wi-Fi and
 * battery invisible on the light header until a restart).
 */
export function statusBarForeground(scheme: "light" | "dark"): "light" | "dark" {
  return scheme === "dark" ? "light" : "dark";
}
