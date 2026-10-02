import { requireOptionalNativeModule } from "expo-modules-core";
import { Linking } from "react-native";
import type { WebBrowserAuthSessionResult } from "expo-web-browser";
import { PLUGIN_CALLBACK } from "./pluginOnboarding";

export type PluginBrowserResult = WebBrowserAuthSessionResult | { type: "external" };

// Existing development clients may predate ExpoWebBrowser. Do not evaluate
// that package's requireNativeModule at route import: Linking can open the
// same official authorization page, and the existing flow owns return/status.
export async function openPluginAuthorization(url: string, deviceFlow: boolean): Promise<PluginBrowserResult> {
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
