import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import * as Clipboard from 'expo-clipboard';
import { WebView, WebViewMessageEvent } from 'react-native-webview';
import { dispatchRenderer } from '../../modules/terminal-vt/src';
import { createTerminalRendererTransport } from './terminalRendererTransport';
import type {
  MouseAction,
  MouseButton,
  RenderSnapshot,
} from '../../modules/terminal-vt/src';
import type { TerminalThemePalette } from '../../constants/terminalThemes';
import { createTerminalRenderScheduler } from './terminalRenderScheduler';
import { useGhosttyCoreTerminal } from './useGhosttyCoreTerminal';
import { useTerminalSession } from './useTerminalSession';
import type { TerminalInputHandleRef } from './TerminalInputHandler';
import { TerminalLiveGridOwner } from './terminalLiveGrid';
import type { TerminalScrollCancelReason } from './terminalScrollGesture';
import { useTerminalHistory } from './useTerminalHistory';
import { encodeTerminalWheel, selectTerminalScrollRoute } from './terminalWheel';
import type { TerminalHistoryRender } from './terminalHistory';
import { isCurrentTerminalRendererGeneration } from './terminalSurfaceBootstrap';
import {
  notifyTmuxClientFocus,
  TerminalScrollCorrelation,
} from './terminalSessionCorrelation';

type BridgeMessage = { rendererGeneration: number } & (
  | { type: 'ready' }
  | { type: 'bootstrapError'; message: string }
  | { type: 'bootstrapWarning'; message: string }
  | { type: 'runtimeError'; message: string }
  | {
      type: 'resize';
      cols: number;
      rows: number;
      cellWidth: number;
      cellHeight: number;
    }
  | { type: 'focusInput'; sessionId: string | null }
  | { type: 'selectionActive'; active: boolean }
  | { type: 'historyInteraction'; sessionId: string | null; active: boolean }
  | { type: 'historyRetry'; sessionId: string | null }
  | { type: 'viewportScroll'; sessionId: string | null; atBottom: boolean }
  | { type: 'copyText'; text: string }
  | { type: 'wheel'; sessionId: string | null; token: string | null; ticks: number; x: number; y: number }
  | {
      type: 'mouse';
      action: MouseAction;
      button: MouseButton;
      x: number;
      y: number;
      shift?: boolean;
      ctrl?: boolean;
      alt?: boolean;
      meta?: boolean;
      anyButtonPressed?: boolean;
      sessionId: string | null;
    }
);

type RendererCommand =
  | 'blur'
  | 'wakeRenderer'
  | 'resumeInput'
  | 'scrollToBottom';

type RendererStateMessage =
  | { type: 'renderSnapshot'; snapshot: RenderSnapshot }
  | { type: 'theme'; theme: TerminalThemePalette }
  | { type: 'history'; history: TerminalHistoryRender };

interface UseGhosttyTerminalControllerArgs {
  serverId: string;
  targetId: string;
  backend: string;
  theme: TerminalThemePalette;
  rendererGeneration: number;
  onCtrlArmedChange?: (next: boolean) => void;
  onRendererBootstrapFailure?: (message: string, generation: number) => void;
}

/**
 * One native terminal path: PTY bytes update Ghostty, and Ghostty updates one
 * live grid. Styled pane history uses the same Ghostty formatter; the WebView
 * owns scrolling locally, without PTY redraws or per-frame bridge messages.
 */
export function useGhosttyTerminalController({
  serverId,
  targetId,
  backend,
  theme,
  rendererGeneration,
  onCtrlArmedChange,
  onRendererBootstrapFailure,
}: UseGhosttyTerminalControllerArgs) {
  const webviewRef = useRef<WebView>(null);
  const inputRef = useRef<TerminalInputHandleRef>(null);
  const webReadyRef = useRef(false);
  const pendingRef = useRef<RendererStateMessage[]>([]);
  const pendingRendererCommandRef = useRef<RendererCommand | null>(null);
  const gridOwnerRef = useRef<TerminalLiveGridOwner | null>(null);
  const scrollCorrelationRef = useRef(new TerminalScrollCorrelation());
  const paneModesRef = useRef<TerminalHistoryRender['panes']>([]);
  const rendererGenerationRef = useRef(rendererGeneration);
  rendererGenerationRef.current = rendererGeneration;
  const [readyGeneration, setReadyGeneration] = useState<number | null>(null);
  const [scrolledUp, setScrolledUp] = useState(false);
  const ready = readyGeneration === rendererGeneration;

  const ghostty = useGhosttyCoreTerminal();

  const rendererTransport = useMemo(() => createTerminalRendererTransport(
    dispatchRenderer,
    (message, generation) => {
      webReadyRef.current = false;
      setReadyGeneration(null);
      onRendererBootstrapFailure?.(message, generation);
    },
  ), [onRendererBootstrapFailure]);
  useEffect(() => () => rendererTransport.clear(), [rendererTransport]);

  const injectRendererState = useCallback((payload: RendererStateMessage) => {
    const script = payload.type === 'renderSnapshot'
      ? `window.__mewlaRenderSnapshot && window.__mewlaRenderSnapshot(${JSON.stringify(payload.snapshot)}); true;`
      : payload.type === 'history'
        ? `window.__mewlaHistory && window.__mewlaHistory(${JSON.stringify(payload.history)}); true;`
      : `window.__mewlaTheme && window.__mewlaTheme(${JSON.stringify(payload.theme)}); true;`;
    rendererTransport.send(script);
  }, [rendererTransport]);

  const postToRenderer = useCallback((payload: RendererStateMessage) => {
    if (!webReadyRef.current) {
      pendingRef.current = pendingRef.current.filter(
        (pending) => pending.type !== payload.type,
      );
      pendingRef.current.push(payload);
      return;
    }
    injectRendererState(payload);
  }, [injectRendererState]);

  const publishHistory = useCallback((history: TerminalHistoryRender) => {
    paneModesRef.current = history.panes || [];
    postToRenderer({ type: 'history', history });
  }, [postToRenderer]);
  const history = useTerminalHistory(serverId, theme, publishHistory);

  const injectRendererCommand = useCallback((command: RendererCommand) => {
    const scripts: Record<RendererCommand, string> = {
      blur: 'window.__mewlaBlur && window.__mewlaBlur(); true;',
      wakeRenderer: 'window.__mewlaWakeRenderer && window.__mewlaWakeRenderer(); true;',
      resumeInput: 'window.__mewlaResumeInput && window.__mewlaResumeInput(); true;',
      scrollToBottom: 'window.__mewlaScrollToBottom && window.__mewlaScrollToBottom(); true;',
    };
    rendererTransport.send(scripts[command]);
  }, [rendererTransport]);

  const runRendererCommand = useCallback((command: RendererCommand) => {
    if (!webReadyRef.current) {
      pendingRendererCommandRef.current = command;
      return;
    }
    injectRendererCommand(command);
  }, [injectRendererCommand]);

  const replaceScrollContext = useCallback((
    sessionId: string | null,
    reason: TerminalScrollCancelReason,
  ) => {
    const context = scrollCorrelationRef.current.replace(sessionId);
    if (webReadyRef.current) {
      rendererTransport.send(
        `window.__mewlaSetScrollContext && window.__mewlaSetScrollContext(${JSON.stringify(context.sessionId)}, ${JSON.stringify(context.token)}, ${JSON.stringify(reason)}); true;`,
      );
    }
  }, [rendererTransport]);

  const cancelLocalScroll = useCallback((reason: TerminalScrollCancelReason) => {
    replaceScrollContext(scrollCorrelationRef.current.context.sessionId, reason);
  }, [replaceScrollContext]);

  const flushRenderState = useCallback(() => {
    // Keep native dirty rows until this renderer can receive their full base.
    if (!webReadyRef.current || !webviewRef.current) return;
    const frame = ghostty.consumeRenderSnapshot();
    if (frame) {
      postToRenderer({ type: 'renderSnapshot', snapshot: frame.snapshot });
    }
  }, [ghostty, postToRenderer]);

  const renderScheduler = useMemo(
    () => createTerminalRenderScheduler(flushRenderState),
    [flushRenderState],
  );
  const scheduleRenderState = renderScheduler.schedule;

  useEffect(() => () => renderScheduler.cancel(), [renderScheduler]);

  useEffect(() => {
    ghostty.setTheme(theme);
    postToRenderer({ type: 'theme', theme });
    scheduleRenderState();
  }, [ghostty, postToRenderer, scheduleRenderState, theme]);

  const session = useTerminalSession(serverId, targetId, backend, {
    onOpened: ({ sessionId }) => {
      replaceScrollContext(sessionId, 'session-change');
      history.attach(sessionId);
      setScrolledUp(false);
      const attached = gridOwnerRef.current?.attach(sessionId) ?? false;
      if (attached) {
        scheduleRenderState();
      }
      return attached;
    },
    onOutput: ({ session_id, data }) => {
      history.output();
      if (ghostty.writeOutput(session_id, data)) {
        scheduleRenderState();
      }
    },
    onSessionInvalidated: (sessionId, reason) => {
      history.attach(null);
      replaceScrollContext(
        null,
        reason === 'disconnect' ? 'disconnect' : 'session-change',
      );
      gridOwnerRef.current?.detach(sessionId ?? undefined);
      setScrolledUp(false);
      inputRef.current?.clear();
      inputRef.current?.blur();
    },
    onExit: ({ session_id, exit_code }) => {
      const message = `\r\n[Mewla] session exited with code ${exit_code}\r\n`;
      if (ghostty.writeOutput(session_id, message)) {
        scheduleRenderState();
      }
    },
    onError: ({ session_id, code, message }) => {
      if (!session_id) {
        return;
      }
      if (ghostty.writeOutput(session_id, `\r\n[Mewla] ${message}\r\n`)) {
        scheduleRenderState();
      }
    },
  });

  const gridOwner = useMemo(() => new TerminalLiveGridOwner({
    requestPtyGrid(cols, rows) {
      session.resize(cols, rows);
    },
    resetGhostty(sessionId, grid) {
      return ghostty.resetTerminal(sessionId, grid);
    },
    resizeGhostty(sessionId, grid) {
      const resized = ghostty.resizeGrid(sessionId, grid);
      if (resized) {
        scheduleRenderState();
      }
      return resized;
    },
  }), [ghostty, scheduleRenderState, session]);
  gridOwnerRef.current = gridOwner;

  const canDeliverInput = useCallback(() => session.canSendInput(), [session]);

  const notifyClientFocus = useCallback(() => {
    return notifyTmuxClientFocus(backend, session.sendInput);
  }, [backend, session]);

  const deliverInput = useCallback((data: string) => {
    cancelLocalScroll('input');
    runRendererCommand('scrollToBottom');
    return session.sendInput(data);
  }, [cancelLocalScroll, runRendererCommand, session]);

  const focusPaneAtPoint = useCallback((x: number, y: number) => {
    if (backend !== 'tmux') {
      return;
    }
    const grid = ghostty.currentGrid();
    if (!grid) {
      return;
    }
    const col = Math.max(0, Math.min(grid.cols - 1, Math.floor(x / grid.cellWidth)));
    const row = Math.max(0, Math.min(grid.rows - 1, Math.floor(y / grid.cellHeight)));
    session.focusPane(col, row);
    history.output();
  }, [backend, ghostty, history, session]);

  const focus = useCallback(() => {
    if (canDeliverInput()) {
      cancelLocalScroll('input');
      runRendererCommand('scrollToBottom');
      notifyClientFocus();
      inputRef.current?.focus();
    }
  }, [canDeliverInput, cancelLocalScroll, notifyClientFocus, runRendererCommand]);

  const blur = useCallback(() => {
    cancelLocalScroll('route-blur');
    inputRef.current?.blur();
    runRendererCommand('blur');
  }, [cancelLocalScroll, runRendererCommand]);

  const wakeRenderer = useCallback(() => {
    runRendererCommand('wakeRenderer');
    scheduleRenderState();
  }, [runRendererCommand, scheduleRenderState]);

  const enterLiveMode = useCallback((command: 'resumeInput' | 'scrollToBottom') => {
    cancelLocalScroll('jump-live');
    if (!notifyClientFocus()) {
      session.cancelScroll();
    }
    setScrolledUp(false);
    runRendererCommand(command);
    if (canDeliverInput()) {
      inputRef.current?.focus();
    }
  }, [canDeliverInput, cancelLocalScroll, notifyClientFocus, runRendererCommand, session]);

  const resumeInput = useCallback(() => {
    wakeRenderer();
    enterLiveMode('resumeInput');
  }, [enterLiveMode, wakeRenderer]);

  const scrollToBottom = useCallback(() => {
    wakeRenderer();
    enterLiveMode('scrollToBottom');
  }, [enterLiveMode, wakeRenderer]);

  const onInput = useCallback((data: string) => {
    deliverInput(data);
  }, [deliverInput]);

  const clearInputMirror = useCallback(() => {
    inputRef.current?.clear();
  }, []);

  const onRendererLoadStart = useCallback((generation: number) => {
    if (!isCurrentTerminalRendererGeneration(
      rendererGenerationRef.current,
      generation,
    )) {
      return;
    }
    webReadyRef.current = false;
    rendererTransport.clear();
    renderScheduler.cancel();
    pendingRef.current = [];
    setReadyGeneration(null);
    // A WebView retry/remount loses its DOM but not the sole Ghostty model.
    // Reapplying the same theme marks the native render state fully dirty so
    // the replacement renderer receives a complete current snapshot on ready.
    ghostty.setTheme(theme);
  }, [ghostty, renderScheduler, rendererTransport, theme]);

  const onRendererMessage = useCallback((event: WebViewMessageEvent) => {
    try {
      const payload = JSON.parse(event.nativeEvent.data) as BridgeMessage;
      if (
        !isCurrentTerminalRendererGeneration(
          rendererGenerationRef.current,
          payload.rendererGeneration,
        )
      ) {
        return;
      }

      if (payload.type === 'ready') {
        const nativeEvent = event.nativeEvent as typeof event.nativeEvent & { target: number };
        rendererTransport.bind(nativeEvent.target, payload.rendererGeneration);
        webReadyRef.current = true;
        setReadyGeneration(payload.rendererGeneration);
        postToRenderer({ type: 'theme', theme });
        const queued = pendingRef.current;
        pendingRef.current = [];
        queued.forEach(injectRendererState);
        const pendingCommand = pendingRendererCommandRef.current;
        pendingRendererCommandRef.current = null;
        if (pendingCommand) {
          injectRendererCommand(pendingCommand);
        }
        const scrollContext = scrollCorrelationRef.current.context;
        rendererTransport.send(
          `window.__mewlaSetScrollContext && window.__mewlaSetScrollContext(${JSON.stringify(scrollContext.sessionId)}, ${JSON.stringify(scrollContext.token)}, "session-change"); true;`,
        );
        scheduleRenderState();
        history.replay();
        injectRendererCommand('wakeRenderer');
        return;
      }

      if (payload.type === 'bootstrapWarning') {
        console.warn('[Terminal WebView] ' + payload.message);
        return;
      }

      if (payload.type === 'runtimeError') {
        console.error('[Terminal WebView] runtime error: ' + payload.message);
        return;
      }

      if (payload.type === 'bootstrapError') {
        webReadyRef.current = false;
        setReadyGeneration(null);
        const message = payload.message || 'Terminal WebView bootstrap failed.';
        console.error('[Terminal WebView] bootstrap error: ' + message);
        onRendererBootstrapFailure?.(message, payload.rendererGeneration);
        return;
      }

      if (payload.type === 'resize') {
        history.output();
        gridOwner.update({
          cols: payload.cols,
          rows: payload.rows,
          cellWidth: payload.cellWidth,
          cellHeight: payload.cellHeight,
        });
        return;
      }

      if (payload.type === 'focusInput') {
        if (!session.acceptInteractionSession(payload.sessionId)) {
          return;
        }
        cancelLocalScroll('input');
        if (!notifyClientFocus()) {
          session.cancelScroll();
        }
        setScrolledUp(false);
        if (canDeliverInput()) {
          inputRef.current?.focus();
        }
        return;
      }

      if (payload.type === 'selectionActive') {
        if (payload.active) {
          cancelLocalScroll('selection');
          clearInputMirror();
          onCtrlArmedChange?.(false);
        }
        return;
      }

      if (payload.type === 'copyText') {
        void Clipboard.setStringAsync(payload.text);
        return;
      }

      if (payload.type === 'historyInteraction') {
        if (session.acceptInteractionSession(payload.sessionId)) {
          history.interaction(payload.active);
        }
        return;
      }
      if (payload.type === 'historyRetry') {
        if (session.acceptInteractionSession(payload.sessionId)) history.output();
        return;
      }
      if (payload.type === 'viewportScroll') {
        if (session.acceptInteractionSession(payload.sessionId)) setScrolledUp(!payload.atBottom);
        return;
      }

      if (payload.type === 'mouse') {
        if (
          !session.acceptInteractionSession(payload.sessionId) ||
          !canDeliverInput()
        ) {
          return;
        }

        const isLeftPress = payload.action === 'press' && payload.button === 'left';
        if (isLeftPress) {
          cancelLocalScroll('input');
          session.cancelScroll();
          setScrolledUp(false);
          clearInputMirror();
          focusPaneAtPoint(payload.x, payload.y);
        }

        const encoded = ghostty.encodePointer({
          action: payload.action,
          button: payload.button,
          x: payload.x,
          y: payload.y,
          shift: payload.shift,
          ctrl: payload.ctrl,
          alt: payload.alt,
          meta: payload.meta,
          anyButtonPressed: payload.anyButtonPressed,
        });
        if (encoded) {
          if (!isLeftPress) {
            clearInputMirror();
          }
          deliverInput(encoded);
        }
      }

      if (payload.type === 'wheel') {
        if (!session.acceptInteractionSession(payload.sessionId) ||
            !scrollCorrelationRef.current.accept(payload.sessionId, payload.token) ||
            !canDeliverInput() || !Number.isFinite(payload.x) || !Number.isFinite(payload.y)) return;
        const grid = ghostty.currentGrid();
        if (!grid || selectTerminalScrollRoute(paneModesRef.current || [],
          Math.floor(payload.x / grid.cellWidth), Math.floor(payload.y / grid.cellHeight)) !== 'wheel') return;
        const encoded = encodeTerminalWheel(payload.ticks, (button) => ghostty.encodePointer({
          action: 'press', button, x: payload.x, y: payload.y, anyButtonPressed: false,
        }));
        // Wheel input must not focus the IME, clear the composer, jump the
        // native viewport or invalidate the gesture that produced this batch.
        if (encoded) {
          session.sendInput(encoded);
        }
      }
    } catch {
      // Ignore malformed bridge messages.
    }
  }, [
    canDeliverInput,
    cancelLocalScroll,
    clearInputMirror,
    deliverInput,
    focusPaneAtPoint,
    ghostty,
    history,
    gridOwner,
    injectRendererCommand,
    injectRendererState,
    notifyClientFocus,
    onCtrlArmedChange,
    onRendererBootstrapFailure,
    postToRenderer,
    replaceScrollContext,
    rendererTransport,
    scheduleRenderState,
    session,
    theme,
  ]);

  return {
    webviewRef,
    inputRef,
    ready,
    readyGeneration,
    scrolledUp,
    onInput,
    onCtrlConsumed() {
      onCtrlArmedChange?.(false);
    },
    onRendererLoadStart,
    onRendererMessage,
    sendInput(data: string, options?: { focus?: boolean }) {
      clearInputMirror();
      const sent = deliverInput(data);
      if (sent && options?.focus !== false) {
        inputRef.current?.focus();
      }
    },
    focus,
    blur,
    wakeRenderer,
    resumeInput,
    scrollToBottom,
  };
}
