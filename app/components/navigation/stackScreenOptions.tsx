import React, { useCallback } from "react";
import { Platform } from "react-native";
import { Typography, useAppTheme } from "../../constants/tokens";
import { HeaderBackButton } from "./HeaderBackButton";
import { screenBackParent, type ScreenBackParent } from "./screenBack";
import { useScreenBack, type ScreenBackNavigation } from "./useScreenBack";

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
  return useCallback(
    ({
      navigation,
      route,
    }: {
      navigation: ScreenBackNavigation;
      route: { name: string };
    }) => {
      const backParent = screenBackParent(route.name);
      return {
        headerStyle: { backgroundColor: colors.bgPrimary },
        headerTintColor: colors.textPrimary,
        headerShadowVisible: false,
        headerTitleAlign: "center" as const,
        headerTitleStyle: {
          fontFamily: Typography.uiFontMedium,
          fontSize: 17,
          color: colors.textPrimary,
        },
        // Headerless routes get none: native-stack renders headerLeft even
        // for a hidden header, which would register a hardware-back listener.
        headerLeft: backParent
          ? () => <StackBackButton navigation={navigation} parent={backParent} />
          : undefined,
        contentStyle: { backgroundColor: colors.bgPrimary },
        animation: "slide_from_right" as const,
        fullScreenGestureEnabled: true,
        ...webHeaderInsets,
      };
    },
    [colors],
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
