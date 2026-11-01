import { useCallback, useEffect, useRef, useState } from "react";
import {
  getKeyboardForegroundSnapshot,
  keyboardDiagEnabled,
  keyboardDiagLog,
} from "keyboard-lifecycle";
import {
  Keyboard,
  Platform,
  useWindowDimensions,
  type KeyboardEvent,
  type LayoutChangeEvent,
} from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import {
  accessoryBottomOffset as resolveAccessoryBottomOffset,
  androidKeyboardInset,
  nextAndroidBaseWindow,
  keyboardIsUp,
  type KeyboardFrame,
} from "./terminalKeyboardInset";

interface UseTerminalAccessoryLayoutInput {
  accessoryVisible: boolean;
  ctrlResetKey?: string | null;
  ctrlDisabled?: boolean;
}

export function useTerminalAccessoryLayout({
  accessoryVisible,
  ctrlResetKey,
  ctrlDisabled = false,
}: UseTerminalAccessoryLayoutInput) {
  const insets = useSafeAreaInsets();
  const { width: windowWidth, height: windowHeight } = useWindowDimensions();
  const [shownByEvent, setShownByEvent] = useState(false);
  const [keyboardFrame, setKeyboardFrame] = useState<KeyboardFrame | null>(null);
  const keyboardVisible = keyboardIsUp(
    Platform.OS,
    shownByEvent,
    windowHeight,
    keyboardFrame,
  );
  const [androidInset, setAndroidInset] = useState(0);
  const [accessoryHeight, setAccessoryHeight] = useState(68);
  const [ctrlArmed, setCtrlArmed] = useState(false);
  const baseWindowRef = useRef<{ width: number; height: number } | null>({
    width: windowWidth,
    height: windowHeight,
  });

  const handleLog = (name: string) => (event: KeyboardEvent) => {
    const end = event?.endCoordinates;
    keyboardDiagLog(
      JSON.stringify({ at: "event", name, screenY: end?.screenY, height: end?.height }),
    );
  };

  useEffect(() => {
    const handleFrame = (name: string) => (event: KeyboardEvent) => {
      const end = event?.endCoordinates;
      keyboardDiagLog(
        JSON.stringify({
          at: "event",
          name,
          screenY: end?.screenY,
          height: end?.height,
          width: end?.width,
          metrics: Keyboard.metrics() ?? null,
        }),
      );
      setKeyboardFrame({ screenY: end?.screenY ?? 0, height: end?.height ?? 0 });
    };
    const handleShow = (event: KeyboardEvent) => {
      handleFrame("keyboardDidShow")(event);
      setShownByEvent(true);
    };
    const handleHide = () => {
      keyboardDiagLog(JSON.stringify({ at: "event", name: "keyboardDidHide" }));
      setKeyboardFrame(null);
      setAndroidInset(0);
      setShownByEvent(false);
    };

    const subs = [
      Keyboard.addListener("keyboardDidShow", handleShow),
      Keyboard.addListener("keyboardDidHide", handleHide),
    ];
    // A rotation or a fold moves the keyboard without showing it again; only
    // iOS reports that. keyboardWillShow is logged to show the ordering.
    if (Platform.OS === "ios") {
      subs.push(
        Keyboard.addListener("keyboardDidChangeFrame", handleFrame("keyboardDidChangeFrame")),
      );
      if (keyboardDiagEnabled) {
        subs.push(
          Keyboard.addListener("keyboardWillShow", handleLog("keyboardWillShow")),
          Keyboard.addListener("keyboardWillChangeFrame", handleLog("keyboardWillChangeFrame")),
          Keyboard.addListener("keyboardWillHide", () =>
            keyboardDiagLog(JSON.stringify({ at: "event", name: "keyboardWillHide" })),
          ),
        );
      }
    }
    return () => {
      for (const sub of subs) sub.remove();
    };
  }, []);

  useEffect(() => {
    baseWindowRef.current = nextAndroidBaseWindow(
      baseWindowRef.current,
      { width: windowWidth, height: windowHeight },
      keyboardVisible,
    );
  }, [keyboardVisible, windowWidth, windowHeight]);

  useEffect(() => {
    if (!keyboardVisible || Platform.OS !== "android") return;

    const next = androidKeyboardInset(
      keyboardFrame?.height ?? 0,
      baseWindowRef.current?.height ?? null,
      windowHeight,
    );
    setAndroidInset((previous) =>
      Math.abs(previous - next) <= 1 ? previous : next,
    );
  }, [keyboardVisible, keyboardFrame, windowWidth, windowHeight]);

  useEffect(() => {
    if (!keyboardVisible) {
      setCtrlArmed(false);
    }
  }, [keyboardVisible]);

  useEffect(() => {
    setCtrlArmed(false);
  }, [ctrlResetKey]);

  useEffect(() => {
    if (ctrlDisabled) {
      setCtrlArmed(false);
    }
  }, [ctrlDisabled]);

  const handleCtrlArmedChange = useCallback((next: boolean) => {
    setCtrlArmed(next);
  }, []);

  const handleAccessoryLayout = useCallback((event: LayoutChangeEvent) => {
    const nextHeight = Math.ceil(event.nativeEvent.layout.height);
    setAccessoryHeight((previous) =>
      Math.abs(previous - nextHeight) <= 1 ? previous : nextHeight,
    );
  }, []);

  // Both platforms use an absolute dock. iOS lifts it by the part of the window
  // the keyboard covers now; Android by the part window resize did not handle.
  const accessoryBottomOffset = resolveAccessoryBottomOffset({
    platform: Platform.OS,
    keyboardVisible,
    safeAreaBottom: insets.bottom,
    windowHeight,
    keyboardFrame,
    androidInset,
  });
  const outputBottomInset = accessoryVisible
    ? accessoryHeight + accessoryBottomOffset
    : 0;

  // Opt-in (see keyboard-lifecycle): one line per change of what the layout
  // computed, and what the platform itself says once it settles.
  useEffect(() => {
    if (!keyboardDiagEnabled) return;
    keyboardDiagLog(
      JSON.stringify({
        at: "layout",
        platform: Platform.OS,
        window: { width: windowWidth, height: windowHeight },
        safeAreaBottom: insets.bottom,
        shownByEvent,
        frame: keyboardFrame,
        keyboardVisible,
        accessoryBottomOffset,
        accessoryHeight,
        outputBottomInset,
      }),
    );
    let live = true;
    const timer = setTimeout(() => {
      getKeyboardForegroundSnapshot("diag", 0)
        .then((snapshot) => {
          if (live) keyboardDiagLog(JSON.stringify({ at: "native", ...snapshot }));
        })
        .catch(() => {});
    }, 350);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [
    windowWidth,
    windowHeight,
    insets.bottom,
    shownByEvent,
    keyboardFrame,
    keyboardVisible,
    accessoryBottomOffset,
    accessoryHeight,
    outputBottomInset,
  ]);

  return {
    keyboardVisible,
    ctrlArmed,
    accessoryBottomOffset,
    outputBottomInset,
    handleCtrlArmedChange,
    handleAccessoryLayout,
  };
}
