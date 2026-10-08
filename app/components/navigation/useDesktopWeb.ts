import { createContext, useContext } from "react";
import { Platform, useWindowDimensions } from "react-native";
import { isDesktopWeb } from "./desktopWeb";

/** Desktop web (see `isDesktopWeb`), following the window as it resizes. */
export function useDesktopWeb(): boolean {
  const { width } = useWindowDimensions();
  return isDesktopWeb(Platform.OS, width);
}

/**
 * True under the desktop web shell, which hosts the sidebar for every page.
 * The primary shell then leaves its own docked sidebar out, so the two can
 * never disagree about who draws it.
 */
export const DesktopWebShellContext = createContext(false);

export function useDesktopWebShellHosted(): boolean {
  return useContext(DesktopWebShellContext);
}
