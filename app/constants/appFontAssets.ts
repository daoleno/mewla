import { FontFamilies } from "./fontFamilies";

export const appFontAssets = {
  [FontFamilies.ui]: require("../assets/fonts/Inter-Regular.ttf"),
  [FontFamilies.uiMedium]: require("../assets/fonts/Inter-Medium.ttf"),
  [FontFamilies.uiSemibold]: require("../assets/fonts/Inter-SemiBold.ttf"),
  [FontFamilies.display]: require("../assets/fonts/BricolageGrotesque-ExtraBold.ttf"),
  [FontFamilies.displaySemibold]: require("../assets/fonts/BricolageGrotesque-SemiBold.ttf"),
  [FontFamilies.cjk]: require("../assets/fonts/SourceHanSansSC-Regular.otf"),
  [FontFamilies.cjkMedium]: require("../assets/fonts/SourceHanSansSC-Medium.otf"),
  [FontFamilies.mono]: require("../assets/fonts/MapleMono-CN-Regular.ttf"),
  [FontFamilies.monoBold]: require("../assets/fonts/MapleMono-CN-SemiBold.ttf"),
};
