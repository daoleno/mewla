/** Where a screen's Back lands when it has no history below it. */
export type ScreenBackParent = "/" | "/list" | "/settings";

export type ScreenBackDecision = "pop" | "parent";

/**
 * One Back rule for the header button, the Android hardware button and the
 * iOS edge swipe: the stack pops, and a deep-linked screen with nothing below
 * it lands on its logical parent. Sub-pages are routes, so they pop too.
 */
export function resolveScreenBack(input: {
  canGoBack: boolean;
}): ScreenBackDecision {
  return input.canGoBack ? "pop" : "parent";
}

/**
 * Logical parent for each root Stack route that shows the Stack header.
 * Headerless routes return null: native-stack still renders `headerLeft` for a
 * hidden header, so they get no default Back (and no hardware-back listener).
 * The Session screen draws its own Back with "/list" as the parent; Extensions
 * draws its header in its own nested Stack.
 */
export function screenBackParent(routeName: string): ScreenBackParent | null {
  switch (routeName) {
    case "(primary)":
    case "plugins":
    case "terminal/[id]":
    case "onboarding":
    case "screenshot-demo":
      return null;
    case "settings/model-providers":
      return "/settings";
    default:
      return "/";
  }
}
