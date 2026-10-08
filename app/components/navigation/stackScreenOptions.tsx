import React, { useCallback } from "react";
import { Platform } from "react-native";
import { Typography, useAppTheme } from "../../constants/tokens";
import { HeaderBackButton } from "./HeaderBackButton";
import { screenBackParent, type ScreenBackParent } from "./screenBack";
import { useScreenBack, type ScreenBackNavigation } from "./useScreenBack";
import { desktopReadableRoute, isDesktopTopLevelRoute } from "./desktopWeb";
import { useDesktopWeb } from "./useDesktopWeb";

/** Desktop web pages keep a readable width beside the sidebar. */
const DESKTOP_PAGE_STYLE = {
  width: "100%" as const,
  maxWidth: 1080,
  alignSelf: "center" as const,
};

// Native headers inset their leading and trailing items by 16pt. The web
// Stack header (JS Header) reads these container styles instead; they are not
// part of the native-stack option type.
const webHeaderInsets: object =
  Platform.OS === "web"
    ? {
        headerLeftContainerStyle: { paddingStart: 16 },
        headerRightContainerStyle: { paddingEnd: 16 },
      }
    : {};

/**
 * Header options for every Stack in the app (root and nested), so each
 * pushed screen gets the same header and the one shared Back.
 */
export function useStackScreenOptions() {
  const { colors } = useAppTheme();
  // Desktop web: menu destinations sit beside the sidebar, which is their
  // way out, so they draw no Back and pages switch without a slide.
  const desktopWeb = useDesktopWeb();
  return useCallback(
    ({
      navigation,
      route,
    }: {
      navigation: ScreenBackNavigation;
      route: { name: string };
    }) => {
      const topLevel = desktopWeb && isDesktopTopLevelRoute(route.name);
      const backParent = topLevel ? null : screenBackParent(route.name);
      return {
        headerStyle: { backgroundColor: colors.bgPrimary },
        headerTintColor: colors.textPrimary,
        headerShadowVisible: false,
        // Desktop web titles read like Brain's and Sessions' page titles.
        headerTitleAlign: desktopWeb ? ("left" as const) : ("center" as const),
        headerTitleStyle: topLevel
          ? {
              fontFamily: Typography.displayFont,
              fontSize: 24,
              letterSpacing: -0.6,
              color: colors.textPrimary,
            }
          : {
              fontFamily: Typography.uiFontMedium,
              fontSize: 17,
              color: colors.textPrimary,
            },
        // Headerless routes get none: native-stack renders headerLeft even
        // for a hidden header, which would register a hardware-back listener.
        // Desktop web menu destinations draw an empty slot, or the web
        // header's own back arrow would stand in for the missing one.
        headerLeft: backParent
          ? () => <StackBackButton navigation={navigation} parent={backParent} />
          : topLevel
            ? () => null
            : undefined,
        contentStyle: desktopWeb && desktopReadableRoute(route.name)
          ? [{ backgroundColor: colors.bgPrimary }, DESKTOP_PAGE_STYLE]
          : { backgroundColor: colors.bgPrimary },
        animation: desktopWeb ? ("none" as const) : ("slide_from_right" as const),
        fullScreenGestureEnabled: true,
        ...webHeaderInsets,
      };
    },
    [colors, desktopWeb],
  );
}

/** Default header Back for every pushed screen; see useScreenBack. */
function StackBackButton({
  navigation,
  parent,
}: {
  navigation: ScreenBackNavigation;
  parent: ScreenBackParent;
}) {
  const goBack = useScreenBack({ parent, navigation });
  return <HeaderBackButton onPress={goBack} />;
}
