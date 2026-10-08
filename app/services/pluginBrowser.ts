import { requireOptionalNativeModule } from "expo-modules-core";
import { Linking } from "react-native";
import type { WebBrowserAuthSessionResult } from "expo-web-browser";
import { PLUGIN_CALLBACK } from "./pluginOnboarding";

export type PluginBrowserResult = WebBrowserAuthSessionResult | { type: "external" };

// Existing development clients may predate ExpoWebBrowser. Do not evaluate
// that package's requireNativeModule at route import: Linking can open the
// same official authorization page, and the existing flow owns return/status.
export async function openPluginAuthorization(url: string, deviceFlow: boolean, web?: "same-tab" | "new-tab"): Promise<PluginBrowserResult> {
  // A web UI sign-in returns this tab to Plugins through the daemon callback,
  // which restores the pending connection. A popup would outlive its opener.
  if (web === "same-tab") {
    globalThis.location.assign(url);
    return { type: "external" };
  }
  // A web sign-in that can't return here (a device code, or a service that
  // returns to the app) runs in its own tab while this one polls for it.
  if (web === "new-tab") {
    await Linking.openURL(url);
    return { type: "external" };
  }
  if (!requireOptionalNativeModule("ExpoWebBrowser")) {
    await Linking.openURL(url);
    // External browsers cannot report dismissal. Keep the expiring flow and
    // explicit Cancel action; AppState/Linking check or finish on return.
    return { type: "external" };
  }

  const browser = require("expo-web-browser") as typeof import("expo-web-browser");
  if (deviceFlow) {
    await browser.openBrowserAsync(url);
    return { type: "external" };
  }
  return browser.openAuthSessionAsync(url, PLUGIN_CALLBACK);
}
