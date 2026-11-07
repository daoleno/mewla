import type { TerminalThemeChrome, TerminalThemePalette } from "../../../constants/terminalThemes";
import { withAlpha } from "../colorWithAlpha";
import type { DiffViewerVars } from "./diffViewerProtocol";
import type { TreeViewerVars } from "./treeViewerProtocol";

/**
 * The reader's palette: syntax ink and change colours come from the terminal
 * theme (so a diff matches the terminal beside it), the page and gutters from
 * the app's review chrome (so it matches the sheet around it).
 */
export function buildDiffViewerVars(theme: TerminalThemePalette, chrome: TerminalThemeChrome): DiffViewerVars {
  const quiet = withAlpha(theme.foreground, 0.62);
  return {
    "--diffs-foreground": theme.foreground,
    "--diffs-background": chrome.surface,
    "--diffs-ansi-black": theme.black,
    "--diffs-ansi-red": theme.red,
    "--diffs-ansi-green": theme.green,
    "--diffs-ansi-yellow": theme.yellow,
    "--diffs-ansi-blue": theme.blue,
    "--diffs-ansi-magenta": theme.magenta,
    "--diffs-ansi-cyan": theme.cyan,
    "--diffs-ansi-white": theme.white,
    "--diffs-ansi-bright-black": theme.brightBlack,
    "--diffs-ansi-bright-red": theme.brightRed,
    "--diffs-ansi-bright-green": theme.brightGreen,
    "--diffs-ansi-bright-yellow": theme.brightYellow,
    "--diffs-ansi-bright-blue": theme.brightBlue,
    "--diffs-ansi-bright-magenta": theme.brightMagenta,
    "--diffs-ansi-bright-cyan": theme.brightCyan,
    "--diffs-ansi-bright-white": theme.brightWhite,
    "--diffs-token-comment": theme.brightBlack,
    "--diffs-token-keyword": theme.magenta,
    "--diffs-token-string": theme.green,
    "--diffs-token-string-expression": theme.green,
    "--diffs-token-constant": theme.yellow,
    "--diffs-token-function": theme.blue,
    "--diffs-token-parameter": theme.cyan,
    "--diffs-token-punctuation": quiet,
    "--diffs-token-link": theme.cyan,
    "--diffs-token-inserted": theme.green,
    "--diffs-token-deleted": theme.red,
    "--diffs-addition-color-override": theme.green,
    "--diffs-deletion-color-override": theme.red,
    "--diffs-modified-color-override": theme.blue,
    "--diffs-bg-addition-override": withAlpha(theme.green, 0.13),
    "--diffs-bg-deletion-override": withAlpha(theme.red, 0.13),
    "--diffs-bg-addition-emphasis-override": withAlpha(theme.green, 0.32),
    "--diffs-bg-deletion-emphasis-override": withAlpha(theme.red, 0.32),
    "--diffs-bg-addition-number-override": withAlpha(theme.green, 0.2),
    "--diffs-bg-deletion-number-override": withAlpha(theme.red, 0.2),
    "--diffs-bg-context-override": chrome.surface,
    "--diffs-bg-context-gutter-override": chrome.surface,
    "--diffs-bg-buffer-override": chrome.surfaceMuted,
    "--diffs-bg-separator-override": chrome.surfaceMuted,
    "--diffs-bg-selection-override": withAlpha(chrome.accent, 0.18),
    "--diffs-bg-selection-number-override": withAlpha(chrome.accent, 0.28),
    "--diffs-bg-hover-override": chrome.surfaceMuted,
    "--diffs-fg-number-override": chrome.textSubtle,
    "--diffs-fg-number-addition-override": theme.green,
    "--diffs-fg-number-deletion-override": theme.red,
  };
}

/** The tree's palette: the list sits on the sheet's grouped canvas. */
export function buildTreeViewerVars(theme: TerminalThemePalette, chrome: TerminalThemeChrome): TreeViewerVars {
  return {
    "--trees-bg-override": chrome.appBackground,
    "--trees-bg-muted-override": chrome.surfaceMuted,
    "--trees-fg-override": chrome.text,
    "--trees-fg-muted-override": chrome.textSubtle,
    "--trees-search-bg-override": chrome.surfaceMuted,
    "--trees-search-fg-override": chrome.text,
    "--trees-input-bg-override": chrome.surfaceMuted,
    "--trees-accent-override": chrome.accent,
    "--trees-selected-bg-override": chrome.accentSoft,
    "--trees-border-color-override": chrome.border,
    "--trees-focus-ring-color-override": chrome.focus,
    "--trees-git-added-color-override": theme.green,
    "--trees-git-deleted-color-override": theme.red,
    "--trees-git-modified-color-override": theme.yellow,
    "--trees-git-renamed-color-override": theme.cyan,
    "--trees-git-untracked-color-override": theme.green,
  };
}
