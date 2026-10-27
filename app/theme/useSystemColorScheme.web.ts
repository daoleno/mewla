import { useEffect, useState } from "react";

const DARK_QUERY = "(prefers-color-scheme: dark)";

function currentScheme(): "light" | "dark" | null {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") return null;
  return window.matchMedia(DARK_QUERY).matches ? "dark" : "light";
}

/**
 * The browser's appearance, read from the media query itself so Auto follows
 * a system switch at once (react-native-web's useColorScheme kept the scheme
 * the page loaded with).
 */
export function useSystemColorScheme(): "light" | "dark" | null {
  const [scheme, setScheme] = useState(currentScheme);
  useEffect(() => {
    if (typeof window === "undefined" || typeof window.matchMedia !== "function") return;
    const query = window.matchMedia(DARK_QUERY);
    const update = () => setScheme(query.matches ? "dark" : "light");
    update();
    query.addEventListener("change", update);
    return () => query.removeEventListener("change", update);
  }, []);
  return scheme;
}
