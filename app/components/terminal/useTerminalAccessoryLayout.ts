import { useCallback, useEffect, useRef, useState } from "react";
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
  const [keyboardVisible, setKeyboardVisible] = useState(false);
  const [androidInset, setAndroidInset] = useState(0);
  const [accessoryHeight, setAccessoryHeight] = useState(68);
  const [ctrlArmed, setCtrlArmed] = useState(false);
  const [keyboardFrame, setKeyboardFrame] = useState<KeyboardFrame | null>(null);
  const baseWindowRef = useRef<{ width: number; height: number } | null>({
    width: windowWidth,
    height: windowHeight,
  });

  useEffect(() => {
    const handleFrame = (event: KeyboardEvent) => {
      const end = event?.endCoordinates;
      setKeyboardFrame({ screenY: end?.screenY ?? 0, height: end?.height ?? 0 });
    };
    const handleShow = (event: KeyboardEvent) => {
      handleFrame(event);
      setKeyboardVisible(true);
    };
    const handleHide = () => {
      setKeyboardFrame(null);
      setAndroidInset(0);
      setKeyboardVisible(false);
    };

    const subs = [
      Keyboard.addListener("keyboardDidShow", handleShow),
      Keyboard.addListener("keyboardDidHide", handleHide),
    ];
    // A rotation or a fold moves the keyboard without showing it again; only
    // iOS reports that.
    if (Platform.OS === "ios") {
      subs.push(Keyboard.addListener("keyboardDidChangeFrame", handleFrame));
    }
    return () => {
      for (const sub of subs) sub.remove();
    };
  }, []);

  // The window changed under a keyboard that is up (rotation, fold): take the
  // keyboard's frame as it is now, not as it was when it showed.
  useEffect(() => {
    if (Platform.OS !== "ios" || !keyboardVisible) return;
    const metrics = Keyboard.metrics();
    if (metrics) {
      setKeyboardFrame({ screenY: metrics.screenY, height: metrics.height });
    }
  }, [keyboardVisible, windowWidth, windowHeight]);

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

  return {
    keyboardVisible,
    ctrlArmed,
    accessoryBottomOffset,
    outputBottomInset,
    handleCtrlArmedChange,
    handleAccessoryLayout,
  };
}
