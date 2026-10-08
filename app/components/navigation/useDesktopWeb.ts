import { createContext, useContext, useEffect, useSyncExternalStore } from "react";
import { Platform, useWindowDimensions } from "react-native";
import { isDesktopWeb } from "./desktopWeb";

/** Desktop web (see `isDesktopWeb`), following the window as it resizes. */
export function useDesktopWeb(): boolean {
  const { width } = useWindowDimensions();
  return isDesktopWeb(Platform.OS, width);
}

const TOUCH_POINTER_QUERY = "(any-pointer: coarse)";

function touchPointerQuery(): MediaQueryList | null {
  if (Platform.OS !== "web" || typeof window === "undefined") return null;
  if (typeof window.matchMedia !== "function") return null;
  return window.matchMedia(TOUCH_POINTER_QUERY);
}

function subscribeTouchPointer(onChange: () => void): () => void {
  const query = touchPointerQuery();
  if (!query) return () => {};
  query.addEventListener("change", onChange);
  return () => query.removeEventListener("change", onChange);
}

function readKeyboardOnlyWeb(): boolean {
  const query = touchPointerQuery();
  return query != null && !query.matches;
}

/**
 * Web with no touch pointer at all, so typing comes from a physical keyboard
 * and phone keyboard aids (the terminal key bar) stay out. Phones, tablets
 * and touch laptops report a coarse pointer and keep them.
 */
export function useKeyboardOnlyWeb(): boolean {
  return useSyncExternalStore(subscribeTouchPointer, readKeyboardOnlyWeb, () => false);
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

/** The shell's setter for the tab title a page gives itself. */
export const DesktopPageTitleContext = createContext<((title: string | null) => void) | null>(null);

/**
 * A page names itself in the browser tab ("atlas-notes · Mewla") while it is
 * the focused page; without one the tab names the page's place.
 */
export function useDesktopPageTitle(title: string | null | undefined, active = true) {
  const setTitle = useContext(DesktopPageTitleContext);
  useEffect(() => {
    if (!setTitle || !active) return;
    setTitle(title?.trim() || null);
    return () => setTitle(null);
  }, [active, setTitle, title]);
}
