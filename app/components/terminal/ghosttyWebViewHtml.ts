import type { TerminalThemePalette } from '../../constants/terminalThemes';
import {
  TERMINAL_GRID_CELL_WIDTH_FALLBACK_EM,
  TERMINAL_GRID_FONT_SIZE_CSS_PX,
  TERMINAL_GRID_LINE_HEIGHT_RATIO,
  TERMINAL_GRID_SIZE_SOURCE,
  TERMINAL_TEXT_SIZE_ADJUST_CSS,
  terminalGridLineHeightCssPx,
} from './terminalFontDensity';

export function buildGhosttyTerminalHtml(
  theme: TerminalThemePalette,
  fontUri: string | null,
  fontSize: number,
  rendererGeneration: number,
) {
  const terminalFontSize = Number.isFinite(fontSize) && fontSize > 0
    ? fontSize
    : TERMINAL_GRID_FONT_SIZE_CSS_PX;
  const lineHeight = terminalGridLineHeightCssPx(terminalFontSize);
  const safeRendererGeneration = Number.isSafeInteger(rendererGeneration) &&
    rendererGeneration >= 0
    ? rendererGeneration
    : 0;
  const escapedFontUri = fontUri
    ?.replace(/\\/g, '\\\\')
    .replace(/'/g, "\\'") ?? null;
  const fontFace = escapedFontUri ? `
    @font-face {
      font-family: 'ZenTerm';
      src: url('${escapedFontUri}') format('truetype');
      font-display: swap;
    }
  ` : '';
  const terminalFontFamily = escapedFontUri ? "'ZenTerm', monospace" : 'monospace';

  return String.raw`<!DOCTYPE html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta
      name="viewport"
      content="width=device-width, initial-scale=1, maximum-scale=1, user-scalable=no"
    />
    <style>
      ${fontFace}
      html, body {
        margin: 0;
        padding: 0;
        width: 100%;
        height: 100%;
        overflow: hidden;
        background: ${theme.background};
        overscroll-behavior: contain;
        ${TERMINAL_TEXT_SIZE_ADJUST_CSS}
      }
      body {
        user-select: none;
        -webkit-user-select: none;
      }
      #root {
        position: relative;
        width: 100%;
        height: 100%;
        overflow-x: hidden;
        overflow-y: auto;
        overflow-anchor: none;
        overscroll-behavior-y: contain;
        -webkit-overflow-scrolling: touch;
        touch-action: pan-y;
        background: ${theme.background};
      }
      #terminal-live { position: relative; overflow: hidden; }
      #terminal-history { position: relative; overflow: hidden; }
      #history-rows { position: absolute; left: 0; right: 0; }
      #history-notice {
        position: fixed; top: 6px; right: 8px; z-index: 20;
        max-width: 85%; padding: 5px 8px; border-radius: 6px;
        font: 11px system-ui; color: ${theme.foreground};
        background: ${theme.background}; opacity: .9;
      }
      #history-notice:empty { display: none; }
      #terminal-html, #history-rows {
        position: relative;
        overflow: hidden;
        display: block;
        box-sizing: border-box;
        background: ${theme.background};
        color: ${theme.foreground};
        font-family: ${terminalFontFamily};
        font-size: ${terminalFontSize}px;
        line-height: ${lineHeight}px;
        white-space: normal;
        tab-size: 8;
        pointer-events: auto;
        user-select: text;
        -webkit-user-select: text;
        -webkit-touch-callout: default;
        touch-action: pan-y;
        -webkit-tap-highlight-color: transparent;

      }
      #terminal-html { transform: translate3d(0, 0, 0); }
      #history-rows { position: absolute; overflow: visible; }
      #terminal-html *, #history-rows * {
        font-family: inherit;
        user-select: text;
        -webkit-user-select: text;
      }
      .terminal-row {
        display: block;
        height: ${lineHeight}px;
        line-height: ${lineHeight}px;
        white-space: pre;
      }
      .terminal-row * {
        white-space: pre;
      }
      #terminal-html pre {
        margin: 0;
        white-space: pre;
      }
      #terminal-html::selection,
      #terminal-html *::selection, #history-rows *::selection {
        background: ${theme.selectionBackground};
      }
      #terminal-cursor {
        position: absolute;
        top: 0;
        left: 0;
        z-index: 10;
        display: none;
        width: 2px;
        height: ${lineHeight}px;
        background: ${theme.cursor};
        pointer-events: none;
      }
      #cell-measure {
        position: absolute;
        top: 0;
        left: 0;
        visibility: hidden;
        white-space: pre;
        font-family: ${terminalFontFamily};
        font-size: ${terminalFontSize}px;
        line-height: ${lineHeight}px;
      }
    </style>
  </head>
  <body>
    <div id="root">
      <div id="terminal-history"><div id="history-rows"></div></div>
      <div id="terminal-live">
        <div id="terminal-html"></div>
        <div id="terminal-cursor"></div>
      </div>
      <span id="cell-measure">M</span>
    </div>
    <div id="history-notice"></div>
    <script>
      const FONT_SIZE = ${terminalFontSize};
      const LINE_HEIGHT_RATIO = ${TERMINAL_GRID_LINE_HEIGHT_RATIO};
      const CELL_WIDTH_FALLBACK = ${TERMINAL_GRID_CELL_WIDTH_FALLBACK_EM};
      const LINE_HEIGHT_PX = Math.ceil(FONT_SIZE * LINE_HEIGHT_RATIO);
      const HAS_BUNDLED_FONT = ${escapedFontUri !== null};
      const FONT_READY_TIMEOUT_MS = 1200;
      const RENDERER_GENERATION = ${safeRendererGeneration};

      ${TERMINAL_GRID_SIZE_SOURCE}

      let rendererReady = false;
      const send = (payload) => {
        try {
          payload.rendererGeneration = RENDERER_GENERATION;
          window.ReactNativeWebView.postMessage(JSON.stringify(payload));
        } catch (_) {}
      };

      const errorMessage = (value) => {
        if (value && typeof value.message === 'string' && value.message) {
          return value.message;
        }
        if (typeof value === 'string' && value) {
          return value;
        }
        try {
          return String(value || 'Unknown WebView script failure');
        } catch (_) {
          return 'Unknown WebView script failure';
        }
      };

      const reportScriptIssue = (type, value) => {
        send({ type, message: errorMessage(value) });
      };

      window.addEventListener('error', (event) => {
        reportScriptIssue(
          rendererReady ? 'runtimeError' : 'bootstrapError',
          event.error || event.message,
        );
      });
      window.addEventListener('unhandledrejection', (event) => {
        reportScriptIssue(
          rendererReady ? 'runtimeError' : 'bootstrapError',
          event.reason,
        );
      });

      const waitForBundledFont = async () => {
        if (!HAS_BUNDLED_FONT) {
          return;
        }
        if (!document.fonts || typeof document.fonts.load !== 'function') {
          send({
            type: 'bootstrapWarning',
            message: 'WebView FontFaceSet is unavailable; using monospace fallback.',
          });
          return;
        }

        let timeoutId = null;
        const fontAttempt = Promise.resolve()
          .then(() => document.fonts.load(FONT_SIZE + 'px "ZenTerm"'))
          .then(() => document.fonts.ready)
          .then(
            () => ({ status: 'ready' }),
            (error) => ({ status: 'failed', error }),
          );
        const timeout = new Promise((resolve) => {
          timeoutId = setTimeout(
            () => resolve({ status: 'timeout' }),
            FONT_READY_TIMEOUT_MS,
          );
        });
        const result = await Promise.race([fontAttempt, timeout]);
        if (timeoutId != null) {
          clearTimeout(timeoutId);
        }
        if (result.status === 'failed') {
          send({
            type: 'bootstrapWarning',
            message: 'Bundled terminal font failed inside WebView; using monospace fallback: ' +
              errorMessage(result.error),
          });
        } else if (result.status === 'timeout') {
          send({
            type: 'bootstrapWarning',
            message: 'Bundled terminal font timed out; continuing with monospace fallback.',
          });
        }
      };

      (async () => {
        await waitForBundledFont();

        const root = document.getElementById('root');
        const terminalHtml = document.getElementById('terminal-html');
        const cursor = document.getElementById('terminal-cursor');
        const measure = document.getElementById('cell-measure');
        const live = document.getElementById('terminal-live');
        const history = document.getElementById('terminal-history');
        const historyRows = document.getElementById('history-rows');
        const historyNotice = document.getElementById('history-notice');
        if (!root || !terminalHtml || !cursor || !measure) {
          throw new Error('Terminal WebView bootstrap elements are missing.');
        }

        let activeTheme = ${JSON.stringify(theme)};
        let renderSnapshot = {
          rows: 0,
          cols: 0,
          html: '',
          cursorCol: 0,
          cursorRow: 0,
          cursorVisible: false,
        };
        let viewportWidth = 1;
        let viewportHeight = 1;
        let cellWidth = Math.max(1, FONT_SIZE * CELL_WIDTH_FALLBACK);
        let cellHeight = LINE_HEIGHT_PX;
        let lastRenderedHtml = '';
        let lastReportedCols = 0;
        let lastReportedRows = 0;
        let lastReportedCellWidth = 0;
        let lastReportedCellHeight = 0;
        let nativeSelectionActive = false;
        let pendingViewportSyncAfterSelection = false;
        let cursorBlinkVisible = true;
        let drawRAF = null;
        let historyLines = [];
        let historyStart = -1;
        let historyEnd = -1;
        let followLive = true;
        let touching = false;
        let touchRow = null;
        const mountedHistoryRows = new Map();
        let moved = false;
        let tapInTerminal = false;
        let touchX = 0;
        let touchY = 0;
        let interactionActive = false;
        let idleTimer = null;
        let pendingHistory = null;
        let scrollSessionId = null;
        let scrollToken = null;

        const scheduleDraw = () => {
          if (drawRAF == null) {
            drawRAF = requestAnimationFrame(draw);
          }
        };

        const focusInput = () => {
          if (!nativeSelectionActive) {
            send({ type: 'focusInput', sessionId: scrollSessionId });
          }
        };

        const sendMouse = (action, button, x, y, anyButtonPressed) => {
          send({
            type: 'mouse',
            sessionId: scrollSessionId,
            action,
            button,
            x,
            y,
            anyButtonPressed,
          });
        };

        const emitTap = (x, y) => {
          if (!followLive || nativeSelectionActive || hasTerminalSelection()) {
            return;
          }
          y -= live.getBoundingClientRect().top;
          x -= live.getBoundingClientRect().left;
          sendMouse('press', 'left', x, y, true);
          sendMouse('release', 'left', x, y, false);
          focusInput();
        };

        const measureCellWidth = () => {
          const width = measure.getBoundingClientRect().width;
          return Math.max(1, width || FONT_SIZE * CELL_WIDTH_FALLBACK);
        };

        const getViewportSize = () => {
          const rect = root.getBoundingClientRect();
          const width = rect.width || root.clientWidth ||
            document.documentElement.clientWidth || window.innerWidth;
          const height = rect.height || root.clientHeight ||
            document.documentElement.clientHeight || window.innerHeight;
          return {
            width: Math.max(1, Math.floor(width || 1)),
            height: Math.max(1, Math.floor(height || 1)),
          };
        };

        const applyTheme = () => {
          document.body.style.background = activeTheme.background;
          document.documentElement.style.background = activeTheme.background;
          root.style.background = activeTheme.background;
          terminalHtml.style.background = activeTheme.background;
          terminalHtml.style.color = activeTheme.foreground;
          historyRows.style.color = activeTheme.foreground;
          historyRows.style.background = activeTheme.background;
          historyNotice.style.color = activeTheme.foreground;
          historyNotice.style.background = activeTheme.background;
          cursor.style.background = activeTheme.cursor;
        };

        const updateCursor = () => {
          if (
            nativeSelectionActive ||
            !cursorBlinkVisible ||
            !renderSnapshot.cursorVisible
          ) {
            cursor.style.display = 'none';
            return;
          }

          const x = renderSnapshot.cursorCol * cellWidth;
          const y = renderSnapshot.cursorRow * cellHeight;
          if (x >= viewportWidth || y >= viewportHeight || x < 0 || y < 0) {
            cursor.style.display = 'none';
            return;
          }
          cursor.style.display = 'block';
          cursor.style.width = Math.max(2, Math.round(cellWidth * 0.14)) + 'px';
          cursor.style.height = cellHeight + 'px';
          cursor.style.transform = 'translate(' + x + 'px,' + y + 'px)';
        };

        const drawHistory = () => {
          if (nativeSelectionActive) return;
          const visibleRows = Math.ceil(viewportHeight / cellHeight);
          const start = Math.max(0, Math.floor(root.scrollTop / cellHeight) - visibleRows * 2);
          const end = Math.min(historyLines.length, start + visibleRows * 5);
          if (start === historyStart && end === historyEnd) return;
          historyStart = start;
          historyEnd = end;
          for (const [index, node] of mountedHistoryRows) {
            if ((index < start || index >= end) && node !== touchRow) {
              node.remove();
              mountedHistoryRows.delete(index);
            }
          }
          // Keep surviving DOM nodes, including the node under the finger.
          // Replacing innerHTML here loses Android touchmove/touchend events.
          for (let index = start; index < end; index++) {
            if (mountedHistoryRows.has(index)) continue;
            const template = document.createElement('template');
            template.innerHTML = historyLines[index];
            const node = template.content.firstElementChild;
            if (!node) continue;
            node.style.position = 'absolute';
            node.style.top = index * cellHeight + 'px';
            node.style.left = '0';
            node.style.right = '0';
            mountedHistoryRows.set(index, node);
            const following = Array.from(historyRows.children).find((child) => Number(child.dataset.historyIndex) > index);
            node.dataset.historyIndex = String(index);
            historyRows.insertBefore(node, following || null);
          }
        };

        const draw = () => {
          drawRAF = null;
          drawHistory();
          const nextHtml = renderSnapshot.html || '';
          if (!nativeSelectionActive && !interactionActive && nextHtml !== lastRenderedHtml) {
            terminalHtml.innerHTML = nextHtml;
            lastRenderedHtml = nextHtml;
          }
          updateCursor();
        };

        const syncViewport = (force) => {
          if (nativeSelectionActive && !force) {
            pendingViewportSyncAfterSelection = true;
            return;
          }

          const viewport = getViewportSize();
          const nextCellWidth = measureCellWidth();
          const nextCellHeight = LINE_HEIGHT_PX;
          const nextGrid = gridSize(
            viewport.width,
            viewport.height,
            nextCellWidth,
            nextCellHeight,
          );
          const nextCols = nextGrid.cols;
          const nextRows = nextGrid.rows;
          viewportWidth = viewport.width;
          viewportHeight = viewport.height;
          cellWidth = nextCellWidth;
          cellHeight = nextCellHeight;
          live.style.height = viewportHeight + 'px';
          if (followLive) root.scrollTop = root.scrollHeight;

          const shouldReport = force ||
            nextCols !== lastReportedCols ||
            nextRows !== lastReportedRows ||
            Math.abs(nextCellWidth - lastReportedCellWidth) > 0.25 ||
            nextCellHeight !== lastReportedCellHeight;
          if (shouldReport) {
            lastReportedCols = nextCols;
            lastReportedRows = nextRows;
            lastReportedCellWidth = nextCellWidth;
            lastReportedCellHeight = nextCellHeight;
            send({
              type: 'resize',
              cols: nextCols,
              rows: nextRows,
              cellWidth: nextCellWidth,
              cellHeight: nextCellHeight,
            });
          }
          scheduleDraw();
        };

        const selectionContainsNode = (node) => {
          if (!node) {
            return false;
          }
          const element = node.nodeType === Node.TEXT_NODE ? node.parentNode : node;
          return element === terminalHtml || terminalHtml.contains(element) ||
            element === historyRows || historyRows.contains(element);
        };

        const hasTerminalSelection = () => {
          const selection = window.getSelection();
          if (!selection || selection.isCollapsed || selection.rangeCount === 0) {
            return false;
          }
          return selectionContainsNode(selection.anchorNode) ||
            selectionContainsNode(selection.focusNode);
        };

        const clippedTextForRow = (range, row) => {
          const rowRange = document.createRange();
          rowRange.selectNodeContents(row);
          const clipped = range.cloneRange();
          if (clipped.compareBoundaryPoints(Range.START_TO_START, rowRange) < 0) {
            clipped.setStart(rowRange.startContainer, rowRange.startOffset);
          }
          if (clipped.compareBoundaryPoints(Range.END_TO_END, rowRange) > 0) {
            clipped.setEnd(rowRange.endContainer, rowRange.endOffset);
          }
          return clipped.toString();
        };

        const normalizedTerminalSelectionText = () => {
          const selection = window.getSelection();
          if (!selection || selection.isCollapsed || selection.rangeCount === 0) {
            return null;
          }
          if (
            !selectionContainsNode(selection.anchorNode) &&
            !selectionContainsNode(selection.focusNode)
          ) {
            return null;
          }

          const range = selection.getRangeAt(0);
          const rows = Array.from(root.querySelectorAll('.terminal-row'));
          if (rows.length === 0) {
            return selection.toString();
          }
          const selected = [];
          for (const row of rows) {
            if (range.intersectsNode(row)) {
              selected.push({ row, text: clippedTextForRow(range, row) });
            }
          }
          if (selected.length === 0) {
            return selection.toString();
          }

          let text = '';
          for (let index = 0; index < selected.length; index += 1) {
            const current = selected[index];
            if (index > 0) {
              const previous = selected[index - 1].row;
              const previousSoftWraps = previous.dataset.wrap === '1';
              const currentContinuesWrap = current.row.dataset.wrapContinuation === '1';
              if (!previousSoftWraps && !currentContinuesWrap) {
                text += '\n';
              }
            }
            text += current.text;
          }
          return text;
        };

        const clearSelection = () => {
          const selection = window.getSelection();
          if (selection) {
            selection.removeAllRanges();
          }
          syncNativeSelectionState();
        };

        const reportPosition = () => {
          const nextFollow = root.scrollHeight - root.clientHeight - root.scrollTop < 2;
          if (nextFollow !== followLive) {
            followLive = nextFollow;
            send({ type: 'viewportScroll', sessionId: scrollSessionId, atBottom: followLive });
          }
        };

        const setInteraction = (active) => {
          if (active === interactionActive) return;
          interactionActive = active;
          send({ type: 'historyInteraction', sessionId: scrollSessionId, active });
        };

        const settleScroll = () => {
          if (idleTimer != null) clearTimeout(idleTimer);
          idleTimer = null;
          if (touching || nativeSelectionActive) return;
          reportPosition();
          setInteraction(false);
          scheduleDraw();
          if (pendingHistory) {
            const next = pendingHistory;
            pendingHistory = null;
            applyHistory(next);
          }
        };

        const scheduleIdle = () => {
          if (idleTimer != null) clearTimeout(idleTimer);
          idleTimer = setTimeout(settleScroll, 180);
        };

        const jumpLive = () => {
          followLive = true;
          root.scrollTo({ top: root.scrollHeight, behavior: 'instant' });
          send({ type: 'viewportScroll', sessionId: scrollSessionId, atBottom: true });
          scheduleDraw();
        };

        const applyHistory = (next) => {
          if (interactionActive || nativeSelectionActive) {
            pendingHistory = next;
            return;
          }
          const oldTop = root.scrollTop;
          historyLines = next.rows || [];
          mountedHistoryRows.clear();
          historyRows.innerHTML = '';
          historyStart = -1;
          historyEnd = -1;
          history.style.height = historyLines.length * cellHeight + 'px';
          historyNotice.textContent = next.notice || '';
          if (followLive) root.scrollTop = root.scrollHeight;
          else if (next.reset) {
            // Pane/width changes replace coordinates. Keep the reader in history
            // at the closest physical row; never jump to the live screen.
            root.scrollTop = Math.min(oldTop, Math.max(0, historyLines.length * cellHeight - cellHeight));
          } else root.scrollTop = Math.max(0, oldTop - next.removed * cellHeight);
          drawHistory();
        };

        const syncNativeSelectionState = () => {
          const nextActive = hasTerminalSelection();
          if (nativeSelectionActive === nextActive) return nextActive;
          nativeSelectionActive = nextActive;
          setInteraction(nextActive || touching);
          send({ type: 'selectionActive', active: nextActive });
          if (!nextActive) {
            if (pendingViewportSyncAfterSelection) {
              pendingViewportSyncAfterSelection = false;
              syncViewport(true);
            }
            settleScroll();
          }
          scheduleDraw();
          return nextActive;
        };

        root.addEventListener('scroll', () => {
          reportPosition();
          setInteraction(true);
          scheduleDraw();
          scheduleIdle();
        }, { passive: true });
        root.addEventListener('scrollend', settleScroll, { passive: true });

        document.addEventListener('touchstart', (event) => {
          const touch = event.touches[0];
          if (!touch) return;
          touching = true;
          moved = false;
          tapInTerminal = root.contains(event.target);
          touchRow = event.target.closest ? event.target.closest('.terminal-row') : null;
          touchX = touch.clientX;
          touchY = touch.clientY;
          setInteraction(true);
        }, { capture: true, passive: true });
        document.addEventListener('touchmove', (event) => {
          const touch = event.touches[0];
          if (touch && (Math.abs(touch.clientX - touchX) > 4 || Math.abs(touch.clientY - touchY) > 4)) moved = true;
        }, { capture: true, passive: true });
        document.addEventListener('touchend', (event) => {
          touching = false;
          touchRow = null;
          const touch = event.changedTouches && event.changedTouches[0];
          if (tapInTerminal && !moved && touch && !syncNativeSelectionState()) emitTap(touch.clientX, touch.clientY);
          scheduleIdle();
        }, { capture: true, passive: true });
        document.addEventListener('touchcancel', () => {
          touching = false;
          touchRow = null;
          moved = true;
          scheduleIdle();
        }, { capture: true, passive: true });
        historyNotice.addEventListener('click', () => {
          send({ type: 'historyRetry', sessionId: scrollSessionId });
        });

        document.addEventListener('selectionchange', syncNativeSelectionState);

        document.addEventListener('copy', (event) => {
          const text = normalizedTerminalSelectionText();
          if (text == null) {
            return;
          }
          event.preventDefault();
          if (event.clipboardData) {
            event.clipboardData.setData('text/plain', text);
          }
          send({ type: 'copyText', text });
        });

        setInterval(() => {
          cursorBlinkVisible = !cursorBlinkVisible;
          scheduleDraw();
        }, 530);

        window.__zenRenderSnapshot = (nextSnapshot) => {
          renderSnapshot = nextSnapshot || renderSnapshot;
          scheduleDraw();
        };

        window.__zenTheme = (nextTheme) => {
          if (nextTheme) {
            activeTheme = nextTheme;
            applyTheme();
            scheduleDraw();
          }
        };

        window.__zenHistory = applyHistory;

        window.__zenSetScrollContext = (sessionId, token, reason) => {
          const changed = sessionId !== scrollSessionId;
          scrollSessionId = typeof sessionId === 'string' && sessionId ? sessionId : null;
          scrollToken = typeof token === 'string' && token ? token : null;
          if (changed) {
            touching = false;
            interactionActive = false;
            pendingHistory = null;
            historyLines = [];
            history.style.height = '0px';
            historyRows.innerHTML = '';
            mountedHistoryRows.clear();
            historyStart = -1;
            historyEnd = -1;
            followLive = true;
            settleScroll();
          }
        };

        window.__zenBlur = () => {
          touching = false;
          clearSelection();
          settleScroll();
        };

        window.__zenWakeRenderer = () => {
          syncViewport(false);
          scheduleDraw();
          const rootEl = document.documentElement;
          const previousTransform = rootEl.style.transform;
          rootEl.style.transform = 'translateZ(0)';
          void rootEl.offsetHeight;
          rootEl.style.transform = previousTransform || '';
        };

        window.__zenResumeInput = () => {
          jumpLive();
          clearSelection();
          window.__zenWakeRenderer();
        };

        window.__zenScrollToBottom = () => {
          jumpLive();
          clearSelection();
          window.__zenWakeRenderer();
        };

        const handleViewportChange = () => syncViewport(false);
        window.addEventListener('resize', handleViewportChange);
        window.addEventListener('orientationchange', handleViewportChange);
        if (typeof ResizeObserver === 'function') {
          const observer = new ResizeObserver(handleViewportChange);
          observer.observe(root);
        }

        applyTheme();
        requestAnimationFrame(() => {
          try {
            syncViewport(true);
            rendererReady = true;
            send({ type: 'ready' });
          } catch (error) {
            reportScriptIssue('bootstrapError', error);
          }
        });
      })().catch((error) => {
        reportScriptIssue('bootstrapError', error);
      });
    </script>
  </body>
</html>`;
}
