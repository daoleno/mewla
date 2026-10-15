// Settings → About pages on the website.

import { requireOptionalNativeModule } from "expo-modules-core";
import { Linking } from "react-native";

export const PRIVACY_POLICY_URL = "https://mewla.ai/privacy/";
export const SUPPORT_URL = "https://mewla.ai/support/";

/**
 * Opens a page in the in-app browser, or the system browser when this build
 * has no ExpoWebBrowser (web, or an older development client).
 */
export async function openAboutPage(url: string): Promise<void> {
  if (!requireOptionalNativeModule("ExpoWebBrowser")) {
    await Linking.openURL(url);
    return;
  }
  const browser = require("expo-web-browser") as typeof import("expo-web-browser");
  await browser.openBrowserAsync(url);
}
