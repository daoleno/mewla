import React, { useCallback } from "react";
import { Platform } from "react-native";
import { ContentWidth, Typography, useAppTheme } from "../../constants/tokens";
import { DesktopPageHeader } from "./DesktopPageHeader";
import { HeaderBackButton } from "./HeaderBackButton";
import { screenBackParent, type ScreenBackParent } from "./screenBack";
import { useScreenBack, type ScreenBackNavigation } from "./useScreenBack";
import { desktopFramedRoute, desktopPageColumn, isDesktopTopLevelRoute } from "./desktopWeb";
import { useDesktopWeb } from "./useDesktopWeb";

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
      const framed = desktopWeb && desktopFramedRoute(route.name);
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
        // Desktop web: one header for every page, its row on the page's own
        // centered column (DesktopPageHeader, desktopFramedRoute).
        ...(desktopWeb
          ? {
              header: (props: StackHeaderProps) => (
                <DesktopPageHeader
                  column={desktopPageColumn(route.name)}
                  title={desktopHeaderTitle(props)}
                  leading={backParent ? <StackBackButton navigation={navigation} parent={backParent} /> : null}
                  trailing={props.options.headerRight?.({ tintColor: colors.textPrimary, canGoBack: Boolean(props.back) })}
                />
              ),
            }
          : null),
        contentStyle: framed
          ? {
              backgroundColor: colors.bgPrimary,
              width: "100%" as const,
              maxWidth: ContentWidth.page,
              alignSelf: "center" as const,
            }
          : { backgroundColor: colors.bgPrimary },
        animation: desktopWeb ? ("none" as const) : ("slide_from_right" as const),
        fullScreenGestureEnabled: true,
        ...webHeaderInsets,
      };
    },
    [colors, desktopWeb],
  );
}

/** The part of the Stack's header props DesktopPageHeader reads. */
interface StackHeaderProps {
  route: { name: string };
  back?: unknown;
  options: {
    title?: string;
    headerTitle?: string | ((props: { children: string; tintColor?: string }) => React.ReactNode);
    headerRight?: (props: { tintColor?: string; canGoBack: boolean }) => React.ReactNode;
  };
}

/** A page's title as plain text, or the page's own title element. */
function desktopHeaderTitle({ options, route }: StackHeaderProps): React.ReactNode {
  if (typeof options.headerTitle === "string" && options.headerTitle) return options.headerTitle;
  if (options.title) return options.title;
  if (typeof options.headerTitle === "function") {
    return options.headerTitle({ children: route.name, tintColor: undefined });
  }
  return "";
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
