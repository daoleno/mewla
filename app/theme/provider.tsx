import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import {
  DefaultTheme,
  ThemeProvider as NavigationThemeProvider,
} from "expo-router";
import * as SystemUI from "expo-system-ui";
import { useColorScheme } from "react-native";
import {
  getAccentPreference,
  getThemePreference,
  setAccentPreference,
  setThemePreference,
} from "../services/storage";
import { navigationThemeFromTheme } from "./navigation";
import { DEFAULT_ACCENT_ID, getAccentById, type ThemeAccentId } from "./primitives";
import { resolveTheme } from "./resolve";
import { syncSystemRootBackground } from "./syncSystemRootBackground";
import type { ResolvedTheme, ThemePreference } from "./types";

type ThemeContextValue = {
  theme: ResolvedTheme;
  preference: ThemePreference;
  setPreference: (next: ThemePreference) => Promise<void>;
  accentId: ThemeAccentId;
  setAccentId: (next: ThemeAccentId) => Promise<void>;
};

const ThemeContext = createContext<ThemeContextValue | null>(null);

export function ThemeProvider({ children }: { children: ReactNode }) {
  const systemScheme = useColorScheme();
  const [preference, setPreferenceState] = useState<ThemePreference>("system");
  const [accentId, setAccentIdState] = useState<ThemeAccentId>(DEFAULT_ACCENT_ID);
  const [hydrated, setHydrated] = useState(false);

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      const [stored, storedAccent] = await Promise.all([
        getThemePreference().catch(() => null),
        getAccentPreference().catch(() => null),
      ]);
      if (cancelled) return;
      if (stored) {
        setPreferenceState(stored);
      }
      const accent = getAccentById(storedAccent);
      if (accent) {
        setAccentIdState(accent.id);
      }
      setHydrated(true);
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const colorScheme = systemScheme === "light" ? "light" : "dark";
  const themeId = preference === "system" ? null : preference;

  const theme = useMemo(
    () =>
      resolveTheme({
        colorScheme,
        themeId,
        accentId,
      }),
    [accentId, colorScheme, themeId],
  );
  const navigationTheme = useMemo(
    () => navigationThemeFromTheme(theme, DefaultTheme.fonts),
    [theme],
  );

  const setPreference = useCallback(async (next: ThemePreference) => {
    setPreferenceState(next);
    await setThemePreference(next);
  }, []);

  const setAccentId = useCallback(async (next: ThemeAccentId) => {
    setAccentIdState(next);
    await setAccentPreference(next);
  }, []);

  useEffect(() => {
    if (!hydrated) {
      return;
    }
    // Expo system root follows resolved Mewla canvas on Android and iOS.
    // Best-effort; ThemeProvider remains the sole theme state owner.
    void syncSystemRootBackground(theme.colors.bgPrimary, {
      setBackgroundColorAsync: (color) =>
        SystemUI.setBackgroundColorAsync(color),
    });
  }, [hydrated, theme.colors.bgPrimary]);

  const value = useMemo(
    () => ({
      theme,
      preference,
      setPreference,
      accentId,
      setAccentId,
    }),
    [accentId, preference, setAccentId, setPreference, theme],
  );

  if (!hydrated) {
    return null;
  }

  return (
    <ThemeContext.Provider value={value}>
      <NavigationThemeProvider value={navigationTheme}>
        {children}
      </NavigationThemeProvider>
    </ThemeContext.Provider>
  );
}

export function useThemeContext(): ThemeContextValue {
  const context = useContext(ThemeContext);
  if (!context) {
    throw new Error("useThemeContext must be used within ThemeProvider");
  }
  return context;
}
