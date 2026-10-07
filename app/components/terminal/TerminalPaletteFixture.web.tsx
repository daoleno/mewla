import React, { useEffect, useRef } from "react";
import { StyleSheet, View } from "react-native";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { Typography } from "../../constants/tokens";
import type { TerminalThemePalette } from "../../constants/terminalThemes";

const ESC = "\x1b[";
const sgr = (code: string, text: string) => `${ESC}${code}m${text}${ESC}0m`;

/** Typical TUI output: the 16 ANSI colours, git diff, ls, vim, htop. */
function paletteSample(): string {
  const names = ["black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"];
  const swatch = names
    .map((name, index) => `${sgr(String(30 + index), name.padEnd(8))}${sgr(String(90 + index), "bright")}`)
    .join("\r\n");
  const blocks = names.map((_, index) => sgr(String(40 + index), "  ")).join("") +
    "  " + names.map((_, index) => sgr(String(100 + index), "  ")).join("");
  return [
    `${sgr("1;32", "demo@studio")}:${sgr("1;34", "~/atlas-notes")}$ git diff --stat`,
    ` app/settings/strings.ts | 14 ${sgr("32", "+++++++")}${sgr("31", "-------")}`,
    `${sgr("1;32", "demo@studio")}:${sgr("1;34", "~/atlas-notes")}$ git diff`,
    sgr("1", "diff --git a/app/settings/strings.ts b/app/settings/strings.ts"),
    sgr("36", "@@ -12,4 +12,4 @@ export const strings = {"),
    sgr("31", '-  syncPaused: "Synchronisation is currently paused!",'),
    sgr("32", '+  syncPaused: "Sync paused",'),
    '   syncConflict: "Keep both copies",',
    `${sgr("1;32", "demo@studio")}:${sgr("1;34", "~/atlas-notes")}$ ls`,
    `${sgr("1;34", "app")}  ${sgr("1;34", "docs")}  ${sgr("32", "build.sh")}  ${sgr("36", "latest")}  README.md  ${sgr("1;31", "notes.tar.gz")}`,
    "",
    swatch,
    blocks,
    "",
    `  CPU[${sgr("32", "||||||||")}${sgr("31", "||")}${sgr("90", "            ")}  41.2%]   ${sgr("33", "Load average:")} 1.24 0.98 0.77`,
    `  Mem[${sgr("32", "|||||")}${sgr("34", "||")}${sgr("33", "|||")}${sgr("90", "            ")} 6.1G/16G]  ${sgr("1;36", "Tasks:")} 142, ${sgr("1;32", "3 running")}`,
    `  ${sgr("30;42", "  PID USER      PRI  CPU% MEM%  Command          ")}`,
    `  ${sgr("7", " 4127 demo       20   12.0  2.1  bun test --watch ")}`,
    `   4130 demo       20    3.1  0.8  ${sgr("1", "codex")} exec`,
    "",
    `${sgr("7", " NORMAL ")}${sgr("1", " strings.ts")}  ${sgr("90", "[+]")}                  ${sgr("33", "12,3")}  ${sgr("90", "Top")}`,
    `${sgr("31", "E492: Not an editor command: Wq")}`,
    `${sgr("1;32", "demo@studio")}:${sgr("1;34", "~/atlas-notes")}$ `,
  ].join("\r\n");
}

/** Screenshot fixture: the real xterm.js renderer with the app's palette. */
export function TerminalPaletteFixture({ theme }: { theme: TerminalThemePalette }) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    let term: Terminal | null = null;
    let cancelled = false;
    // xterm sizes cells from the first family at open, so wait for it.
    void document.fonts.load(`13px ${Typography.terminalFont}`).finally(() => {
      if (cancelled) return;
      term = open(host);
    });
    return () => {
      cancelled = true;
      term?.dispose();
    };
  }, [theme]);

  function open(host: HTMLDivElement): Terminal {
    const term = new Terminal({
      fontFamily: Typography.terminalFont,
      fontSize: 13,
      theme: { ...theme },
      cursorBlink: false,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(host);
    fit.fit();
    term.write(paletteSample());
    term.focus();
    return term;
  }
  return (
    <View style={[styles.container, { backgroundColor: theme.background }]}>
      <div ref={hostRef} style={{ flex: 1, minHeight: 0, minWidth: 0, height: "100%" }} />
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    paddingLeft: 6,
    paddingTop: 4,
    overflow: "hidden",
  },
});
