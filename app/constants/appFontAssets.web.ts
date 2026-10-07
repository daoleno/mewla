import {
  WEB_DISPLAY_FAMILY,
  WEB_DISPLAY_SEMIBOLD_FAMILY,
  WEB_MONO_BOLD_FAMILY,
  WEB_MONO_FAMILY,
  WEB_UI_FAMILY,
  WEB_UI_MEDIUM_FAMILY,
  WEB_UI_SEMIBOLD_FAMILY,
} from "./fontFamilies.web";

/** Latin faces only; CJK resolves to installed stacks, see fontFamilies.web.ts. */
export const appFontAssets = {
  [WEB_UI_FAMILY]: require("../assets/fonts/Inter-Regular.ttf"),
  [WEB_UI_MEDIUM_FAMILY]: require("../assets/fonts/Inter-Medium.ttf"),
  [WEB_UI_SEMIBOLD_FAMILY]: require("../assets/fonts/Inter-SemiBold.ttf"),
  [WEB_DISPLAY_FAMILY]: require("../assets/fonts/BricolageGrotesque-ExtraBold.ttf"),
  [WEB_DISPLAY_SEMIBOLD_FAMILY]: require("../assets/fonts/BricolageGrotesque-SemiBold.ttf"),
  [WEB_MONO_FAMILY]: require("../assets/fonts/web/MapleMono-Subset-Regular.ttf"),
  [WEB_MONO_BOLD_FAMILY]: require("../assets/fonts/web/MapleMono-Subset-SemiBold.ttf"),
};

// expo-font declares every web face at weight 400 and each weight here is its
// own family, so a "600" role on Inter SemiBold would be bolded twice. Real
// weights still come from the face; CJK fallbacks keep their own weights.
if (typeof document !== "undefined") {
  const style = document.createElement("style");
  style.textContent = "html{font-synthesis-weight:none}";
  document.head.appendChild(style);
}
