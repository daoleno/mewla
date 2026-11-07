import { useEffect, useMemo, type RefObject } from "react";
import type { TerminalThemeChrome, TerminalThemePalette } from "../../../constants/terminalThemes";
import { Typography, useAppTheme } from "../../../constants/tokens";
import type { DiffViewerHostHandle } from "./diffViewerProtocol";
import { buildDiffViewerVars, buildTreeViewerVars } from "./pierreTheme";
import { PIERRE_FONT_FAMILY } from "./pierreViewerHtml";
import type { TreeViewerHostHandle } from "./treeViewerProtocol";
import { Platform } from "react-native";

/**
 * Native WebViews get the terminal font through a document-local family;
 * the web page already has the app's font loaded under its own name.
 */
export function pierreFontFamily(): string {
  return Platform.OS === "web" ? Typography.chatMonoFont : `'${PIERRE_FONT_FAMILY}', monospace`;
}

export interface DiffViewerLayout {
  split: boolean;
  wrap: boolean;
  headers: boolean;
  inset: number;
}

/** Keeps a diff reader's palette, font and layout in step with the app. */
export function useDiffViewerStyle(
  host: RefObject<DiffViewerHostHandle | null>,
  theme: TerminalThemePalette,
  chrome: TerminalThemeChrome,
  fontSize: number,
  layout: DiffViewerLayout,
) {
  const { isLight } = useAppTheme();
  const vars = useMemo(() => buildDiffViewerVars(theme, chrome), [theme, chrome]);
  useEffect(() => {
    host.current?.post([
      { type: "style", vars, dark: !isLight, fontFamily: pierreFontFamily(), fontSize },
      { type: "layout", ...layout },
    ]);
    // `layout` is a fresh object per render; its fields are the dependencies.
  }, [host, vars, isLight, fontSize, layout.split, layout.wrap, layout.headers, layout.inset]); // eslint-disable-line react-hooks/exhaustive-deps
}

/** Keeps a file tree's palette and font in step with the app. */
export function useTreeViewerStyle(
  host: RefObject<TreeViewerHostHandle | null>,
  theme: TerminalThemePalette,
  chrome: TerminalThemeChrome,
) {
  const { isLight } = useAppTheme();
  const vars = useMemo(() => buildTreeViewerVars(theme, chrome), [theme, chrome]);
  useEffect(() => {
    host.current?.post([
      { type: "style", vars, dark: !isLight, fontFamily: pierreFontFamily(), fontSize: 14, itemHeight: 44 },
    ]);
  }, [host, vars, isLight]);
}
