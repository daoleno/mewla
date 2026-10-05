import { useCallback, useEffect, useRef } from "react";
import { BackHandler, Platform } from "react-native";
import { useNavigation, useRouter } from "expo-router";
import { resolveScreenBack, type ScreenBackParent } from "./screenBack";

/** The slice of a screen's navigation object Back needs. */
export interface ScreenBackNavigation {
  canGoBack(): boolean;
  goBack(): void;
  isFocused(): boolean;
}

interface UseScreenBackInput {
  parent: ScreenBackParent;
  /** Returns true when it took an in-screen step (e.g. left a sub-page). */
  onInScreenBack?: () => boolean;
  /**
   * The screen's own navigation. Required from header components: native
   * stack renders them outside the screen's navigation context.
   */
  navigation?: ScreenBackNavigation;
}

/**
 * The single Back action for a pushed screen. The Android hardware button
 * runs the same decision while the screen is focused; a plain pop is left to
 * the navigator so its animation and the iOS swipe stay native.
 */
export function useScreenBack({
  parent,
  onInScreenBack,
  navigation: screenNavigation,
}: UseScreenBackInput) {
  const contextNavigation = useNavigation();
  const navigation = screenNavigation ?? contextNavigation;
  const router = useRouter();
  const inScreenRef = useRef(onInScreenBack);
  useEffect(() => {
    inScreenRef.current = onInScreenBack;
  }, [onInScreenBack]);

  const run = useCallback(
    (fromHardware: boolean): boolean => {
      const decision = resolveScreenBack({
        handledInScreen: inScreenRef.current?.() ?? false,
        canGoBack: navigation.canGoBack(),
      });
      if (decision === "in-screen") return true;
      if (decision === "pop") {
        if (fromHardware) return false;
        navigation.goBack();
        return true;
      }
      router.replace(parent);
      return true;
    },
    [navigation, parent, router],
  );

  useEffect(() => {
    if (Platform.OS !== "android") return;
    // Lower stack screens stay mounted; only the focused one may answer.
    const subscription = BackHandler.addEventListener(
      "hardwareBackPress",
      () => (navigation.isFocused() ? run(true) : false),
    );
    return () => subscription.remove();
  }, [navigation, run]);

  return useCallback(() => {
    run(false);
  }, [run]);
}
