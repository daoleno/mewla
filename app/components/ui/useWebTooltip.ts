import { useEffect, type RefObject } from "react";
import { Platform } from "react-native";

/**
 * Gives an icon-only control a native hover tooltip on web. React Native Web
 * drops `title`, so it is set on the rendered element; native has no hover.
 */
export function useWebTooltip(ref: RefObject<unknown>, text: string | undefined) {
  useEffect(() => {
    if (Platform.OS !== "web") return;
    const node = ref.current as { setAttribute?(name: string, value: string): void; removeAttribute?(name: string): void } | null;
    if (!node?.setAttribute || !node.removeAttribute) return;
    if (text) node.setAttribute("title", text);
    else node.removeAttribute("title");
  }, [ref, text]);
}
