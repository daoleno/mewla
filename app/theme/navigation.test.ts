import { describe, expect, test } from "bun:test";
import { resolveTheme } from "./resolve";
import {
  navigationThemeFromTheme,
  type NavigationThemeFonts,
} from "./navigation";

const fonts: NavigationThemeFonts = {
  regular: { fontFamily: "System", fontWeight: "400" },
  medium: { fontFamily: "System", fontWeight: "500" },
  bold: { fontFamily: "System", fontWeight: "600" },
  heavy: { fontFamily: "System", fontWeight: "700" },
};

describe("Session exit navigation theme continuity", () => {
  test("uses the resolved dark canvas for the native stack transition owner", () => {
    const appTheme = resolveTheme({ colorScheme: "dark" });
    const navigationTheme = navigationThemeFromTheme(appTheme, fonts);

    expect(navigationTheme).toEqual({
      dark: true,
      colors: {
        primary: appTheme.colors.accent,
        background: appTheme.colors.bgPrimary,
        card: appTheme.colors.bgSurface,
        text: appTheme.colors.textPrimary,
        border: appTheme.colors.border,
        notification: appTheme.colors.statusFailed,
      },
      fonts,
    });
    expect(navigationTheme.colors.background).toBe("#141210");
    expect(Object.values(navigationTheme.colors)).not.toContain("transparent");
  });

  test("re-resolves the complete navigation theme for live Light/Dark changes", () => {
    const darkTheme = resolveTheme({ colorScheme: "dark" });
    const lightTheme = resolveTheme({ colorScheme: "light" });
    const darkNavigationTheme = navigationThemeFromTheme(
      darkTheme,
      fonts,
    );
    const lightNavigationTheme = navigationThemeFromTheme(
      lightTheme,
      fonts,
    );

    expect(darkNavigationTheme.dark).toBe(true);
    expect(lightNavigationTheme.dark).toBe(false);
    expect(darkNavigationTheme.colors.background).toBe(
      darkTheme.colors.bgPrimary,
    );
    expect(lightNavigationTheme.colors.background).toBe(
      lightTheme.colors.bgPrimary,
    );
    expect(lightNavigationTheme.colors.card).toBe(
      lightTheme.colors.bgSurface,
    );
    expect(lightNavigationTheme.colors.text).toBe(
      lightTheme.colors.textPrimary,
    );
    expect(darkNavigationTheme.fonts).toBe(fonts);
    expect(lightNavigationTheme.fonts).toBe(fonts);
  });

  test("follows the resolved theme instead of maintaining a second scheme", () => {
    const explicitlyDarkTheme = resolveTheme({
      colorScheme: "light",
      themeId: "classic-dark",
    });
    const navigationTheme = navigationThemeFromTheme(
      explicitlyDarkTheme,
      fonts,
    );

    expect(explicitlyDarkTheme.colorScheme).toBe("dark");
    expect(navigationTheme.dark).toBe(true);
    expect(navigationTheme.colors.background).toBe("#141210");
  });
});
