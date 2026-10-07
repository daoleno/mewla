import React, {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useRef,
} from "react";
import { StyleSheet, View } from "react-native";
import { Terminal, type ITheme } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { Typography } from "../../constants/tokens";
import type { TerminalThemePalette } from "../../constants/terminalThemes";
import { applyCtrlModifier } from "./terminalControl";
import type {
  TerminalSurfaceHandle,
  TerminalSurfaceProps,
} from "./TerminalSurface.types";
import { useTerminalSession } from "./useTerminalSession";

export type { TerminalSurfaceHandle, TerminalSurfaceProps } from "./TerminalSurface.types";

const WHEEL_PIXELS_PER_LINE = 40;
// The phone grid packs columns at 8px; desktop screens fit a readable size.
const WEB_TERMINAL_FONT_SIZE_PX = 13;

function xtermTheme(theme: TerminalThemePalette): ITheme {
  return { ...theme };
}

/**
 * Web terminal: the same daemon PTY session protocol as mobile, with xterm.js
 * as the VT parser and renderer in place of the native Ghostty core.
 */
export const TerminalSurface = forwardRef<
  TerminalSurfaceHandle,
  TerminalSurfaceProps
>(({
  serverId,
  targetId,
  backend = "tmux",
  theme,
  ctrlArmed = false,
  onCtrlArmedChange,
}, ref) => {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const atBottomRef = useRef(true);
  const ctrlArmedRef = useRef(ctrlArmed);
  ctrlArmedRef.current = ctrlArmed;
  const onCtrlArmedChangeRef = useRef(onCtrlArmedChange);
  onCtrlArmedChangeRef.current = onCtrlArmedChange;
  const initialThemeRef = useRef(theme);

  const session = useTerminalSession(serverId, targetId, backend, {
    onOpened: () => {
      atBottomRef.current = true;
      termRef.current?.reset();
      return true;
    },
    onOutput: ({ data }) => {
      termRef.current?.write(data);
    },
    onScrollState: ({ at_bottom }) => {
      atBottomRef.current = at_bottom;
    },
    onExit: ({ exit_code }) => {
      termRef.current?.write(`\r\n[Mewla] session exited with code ${exit_code}\r\n`);
    },
    onError: ({ session_id, message }) => {
      if (session_id) {
        termRef.current?.write(`\r\n[Mewla] ${message}\r\n`);
      }
    },
  });
  const sessionRef = useRef(session);
  sessionRef.current = session;

  const returnToLive = useCallback(() => {
    if (!atBottomRef.current) {
      sessionRef.current.cancelScroll();
      atBottomRef.current = true;
    }
    termRef.current?.scrollToBottom();
  }, []);

  const deliverInput = useCallback((data: string) => {
    returnToLive();
    return sessionRef.current.sendInput(data);
  }, [returnToLive]);

  const fit = useCallback(() => {
    const term = termRef.current;
    const fitAddon = fitRef.current;
    if (!term || !fitAddon || !hostRef.current?.isConnected) return;
    fitAddon.fit();
    sessionRef.current.resize(term.cols, term.rows);
  }, []);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    const term = new Terminal({
      allowProposedApi: false,
      cursorBlink: true,
      fontFamily: Typography.terminalFont,
      fontSize: WEB_TERMINAL_FONT_SIZE_PX,
      scrollback: 5000,
      theme: xtermTheme(initialThemeRef.current),
    });
    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.open(host);
    termRef.current = term;
    fitRef.current = fitAddon;

    const dataSubscription = term.onData((data) => {
      if (ctrlArmedRef.current) {
        onCtrlArmedChangeRef.current?.(false);
        deliverInput(applyCtrlModifier(data));
        return;
      }
      deliverInput(data);
    });
    if (backend === "tmux") {
      // tmux keeps scrollback in copy-mode on the daemon; the attached client
      // lives on the alternate screen, so wheel scrolling is a daemon command.
      let pendingPixels = 0;
      term.attachCustomWheelEventHandler((event) => {
        pendingPixels += event.deltaMode === WheelEvent.DOM_DELTA_LINE
          ? event.deltaY * WHEEL_PIXELS_PER_LINE
          : event.deltaY;
        const lines = Math.trunc(pendingPixels / WHEEL_PIXELS_PER_LINE);
        if (lines !== 0) {
          pendingPixels -= lines * WHEEL_PIXELS_PER_LINE;
          if (lines < 0) atBottomRef.current = false;
          sessionRef.current.scroll(lines);
        }
        event.preventDefault();
        return false;
      });
    }

    const observer = new ResizeObserver(() => fit());
    observer.observe(host);
    fit();

    return () => {
      observer.disconnect();
      dataSubscription.dispose();
      termRef.current = null;
      fitRef.current = null;
      term.dispose();
    };
  }, [backend, deliverInput, fit]);

  useEffect(() => {
    const term = termRef.current;
    if (term) term.options.theme = xtermTheme(theme);
  }, [theme]);

  useImperativeHandle(ref, () => ({
    sendInput(data, options) {
      const sent = deliverInput(data);
      if (sent && options?.focus !== false) termRef.current?.focus();
    },
    focus() {
      termRef.current?.focus();
    },
    blur() {
      termRef.current?.blur();
    },
    wakeRenderer() {
      fit();
      const term = termRef.current;
      if (term) term.refresh(0, term.rows - 1);
    },
    resumeInput() {
      returnToLive();
      termRef.current?.focus();
    },
    scrollToBottom() {
      returnToLive();
    },
  }), [deliverInput, fit, returnToLive]);

  return (
    <View style={[styles.container, { backgroundColor: theme.background }]}>
      <div ref={hostRef} style={webStyles.host} />
    </View>
  );
});

const styles = StyleSheet.create({
  container: {
    flex: 1,
    paddingLeft: 6,
    paddingTop: 4,
    overflow: "hidden",
  },
});

const webStyles = {
  host: {
    flex: 1,
    minHeight: 0,
    minWidth: 0,
  },
} as const;
