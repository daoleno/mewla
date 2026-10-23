import type { ComponentType } from "react";
import { Redirect } from "expo-router";

// Development builds only: Metro folds __DEV__ to false in a release build and
// drops this require, so the demo screen and its fixtures are never bundled.
const ScreenshotDemoScreen: ComponentType | null = __DEV__
  ? require("../components/screenshotDemo/ScreenshotDemoScreen").default
  : null;

export default function ScreenshotDemoRoute() {
  return ScreenshotDemoScreen ? <ScreenshotDemoScreen /> : <Redirect href="/" />;
}
