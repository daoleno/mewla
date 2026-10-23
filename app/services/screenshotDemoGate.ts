// The gate that decides whether the dev-only screenshot demo runs. It stays
// free of fixtures so the app root can ask without bundling them; the demo
// itself loads only in development builds (app/app/screenshot-demo.tsx).

type ScreenshotDemoEnvironment = {
  dev: boolean;
  enabled: string | undefined;
};

export function screenshotDemoEnabled(
  environment: ScreenshotDemoEnvironment = {
    dev: typeof __DEV__ !== "undefined" && __DEV__,
    enabled: process.env.EXPO_PUBLIC_MEWLA_SCREENSHOT_DEMO,
  },
): boolean {
  return environment.dev && environment.enabled === "1";
}

export function shouldUseScreenshotDemoRuntime({
  demo,
  enabled,
  rootSegment,
}: {
  demo: string | string[] | undefined;
  enabled: boolean;
  rootSegment: string | undefined;
}): boolean {
  return (
    enabled &&
    rootSegment === "screenshot-demo" &&
    screenshotDemoRouteOptedIn(demo)
  );
}

export function screenshotDemoRouteOptedIn(
  value: string | string[] | undefined,
): boolean {
  const candidate = Array.isArray(value) ? value[0] : value;
  return candidate === "1";
}
