import { WEB_MONO_BOLD_FAMILY, WEB_MONO_FAMILY } from "./fontFamilies.web";

/** UI text uses installed font stacks; see fontFamilies.web.ts. */
export const appFontAssets = {
  [WEB_MONO_FAMILY]: require("../assets/fonts/web/MapleMono-Subset-Regular.ttf"),
  [WEB_MONO_BOLD_FAMILY]: require("../assets/fonts/web/MapleMono-Subset-SemiBold.ttf"),
};
