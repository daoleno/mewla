import { TERMINAL_SCROLL_ROUTE_SOURCE } from './terminalWheel';
import { TERMINAL_ROW_UPDATES_SOURCE } from './terminalRowUpdates';
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
      font-family: 'TerminalFont';
      src: url('${escapedFontUri}') format('truetype');
      font-display: swap;
    }
  ` : '';
  const terminalFontFamily = escapedFontUri ? "'TerminalFont', monospace" : 'monospace';

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
      #terminal-html, #terminal-wheel-frame, #terminal-scroll-blend, #history-rows {
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
      #terminal-wheel-frame { position: absolute; inset: 0; pointer-events: none; display: none; }
      #terminal-scroll-blend { position: absolute; left: 0; right: 0; z-index: 2; pointer-events: none; display: none; }
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
        <div id="terminal-wheel-frame"></div>
        <div id="terminal-scroll-blend"></div>
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
      window.__mewlaRendererGeneration = RENDERER_GENERATION;

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
          .then(() => document.fonts.load(FONT_SIZE + 'px "TerminalFont"'))
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
        const wheelHtml = document.getElementById('terminal-wheel-frame');
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
          cursorCol: 0,
          cursorRow: 0,
          cursorVisible: false,
        };
        let viewportWidth = 1;
        let viewportHeight = 1;
        let cellWidth = Math.max(1, FONT_SIZE * CELL_WIDTH_FALLBACK);
        let cellHeight = LINE_HEIGHT_PX;
        const rowUpdates = (${TERMINAL_ROW_UPDATES_SOURCE})();
        let presentedRevision = -1;
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
        const selectScrollRoute = ${TERMINAL_SCROLL_ROUTE_SOURCE};
        const wheelCenter = 1000000;
        let paneModes = [];
        let wheelViewport = false;
        let wheelGesture = false;
        let wheelTop = wheelCenter;
        let wheelPixels = 0;
        let wheelDirection = 0;
        let wheelAwaitingFrame = false;
        let wheelFrameAt = 0;
        let wheelRAF = null;
        let wheelX = 0;
        let wheelY = 0;

        const blend = document.getElementById('terminal-scroll-blend');
        let blendAnimation = null;
        let presentedRows = [];
        const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)');

        const stopBlend = () => {
          if (blendAnimation) blendAnimation.cancel();
          blendAnimation = null;
          blend.style.display = 'none';
          blend.replaceChildren();
        };

        // Interpolate only a proven row translation. Exact styled-row matches
        // preserve ANSI/CJK cells; unchanged rows outside the region stay pinned.
        // Unknown layouts, selection and unrelated redraws use the original grid.
        const blendRows = (previous, next) => {
          if (!wheelGesture || (touching && !moved) || nativeSelectionActive || reducedMotion.matches || !previous) {
            stopBlend();
            return;
          }
          const parse = (html) => {
            const template = document.createElement('template');
            template.innerHTML = html;
            return Array.from(template.content.querySelectorAll('.terminal-row'));
          };
          const before = parse(previous);
          const after = parse(next);
          if (before.length !== after.length) { stopBlend(); return; }
          const oldRows = before.map((row) => row.innerHTML);
          const newRows = after.map((row) => row.innerHTML);
          let start = 0;
          let end = 0;
          let delta = 0;
          let bestLength = 0;
          for (let shift = -12; shift <= 12; shift++) {
            if (!shift) continue;
            let runStart = 0;
            let nonblank = 0;
            for (let row = 0; row <= after.length; row++) {
              const oldRow = row + shift;
              if (row < after.length && oldRow >= 0 && oldRow < before.length &&
                  oldRows[oldRow] === newRows[row]) {
                if (after[row].textContent.trim()) nonblank++;
                continue;
              }
              const length = row - runStart;
              if (nonblank >= 4 && length > bestLength && length >= 8) {
                // Fixed headers and transient badges may change beside the
                // transcript. Animate only the longest proven contiguous band.
                start = runStart + Math.min(0, shift);
                end = row + Math.max(0, shift);
                delta = shift;
                bestLength = length;
              }
              runStart = row + 1;
              nonblank = 0;
            }
          }
          if (!delta) { stopBlend(); return; }
          stopBlend();
          const strip = document.createElement('div');
          const rows = delta > 0
            ? before.slice(start, end).concat(after.slice(end - delta, end))
            : after.slice(start, start - delta).concat(before.slice(start, end));
          for (const row of rows) strip.appendChild(row);
          blend.appendChild(strip);
          blend.style.top = start * cellHeight + 'px';
          blend.style.height = (end - start) * cellHeight + 'px';
          blend.style.display = 'block';
          const blendEnd = delta > 0 ? -delta * cellHeight : 0;
          const from = delta > 0 ? 0 : delta * cellHeight;
          // The PTY round trip has already delayed this frame. Present its
          // exact row translation within one display interval, never another
          // round-trip interval. Carrying residual displacement between frames
          // made the old ANSI layer trail the latest grid on continuous output.
          const animation = strip.animate([
            { transform: 'translateY(' + from + 'px)' },
            { transform: 'translateY(' + blendEnd + 'px)' },
          ], { duration: 16, easing: 'linear', fill: 'forwards' });
          blendAnimation = animation;
          animation.onfinish = () => { if (blendAnimation === animation) stopBlend(); };
        };

        const stopWheel = () => {
          stopBlend();
          wheelGesture = false;
          wheelPixels = 0;
          wheelDirection = 0;
          wheelAwaitingFrame = false;
        };

        const wheelFrame = (timestamp) => {
          wheelRAF = null;
          if (!wheelGesture || (touching && !moved) || nativeSelectionActive || !scrollSessionId) return;
          if (timestamp - wheelFrameAt < 16) {
            wheelRAF = requestAnimationFrame(wheelFrame);
            return;
          }
          const ticks = Math.trunc(wheelPixels / cellHeight);
          if (!ticks) return;
          // Consume even while awaiting a redraw: no delayed backlog on LAN.
          wheelPixels %= cellHeight;
          // Some apps consume the first reverse wheel without redrawing. A
          // frame is not an input acknowledgement. Drop blocked deltas, then
          // allow a fresh tick after a bounded no-output deadline; never replay.
          if (wheelAwaitingFrame && timestamp - wheelFrameAt < 32) return;
          wheelFrameAt = timestamp;
          wheelAwaitingFrame = true;
          send({ type: 'wheel', sessionId: scrollSessionId, token: scrollToken,
            ticks: Math.max(-3, Math.min(3, ticks)), x: wheelX, y: wheelY });
        };

        const setWheelViewport = (enabled) => {
          if (enabled === wheelViewport) return;
          stopWheel();
          wheelViewport = enabled;
          // A fixed descendant is excluded from Android WebView's native
          // scroll hit testing. Sticky content stays on screen while retaining
          // the scroller as the touch target's scrolling ancestor.
          if (enabled) root.insertBefore(live, history);
          else root.insertBefore(history, live);
          live.style.position = enabled ? 'sticky' : 'relative';
          live.style.zIndex = enabled ? '1' : '';
          live.style.top = enabled ? '0' : '';
          live.style.left = enabled ? '0' : '';
          live.style.width = enabled ? '100%' : '';
          history.style.height = enabled ? (wheelCenter * 2 + viewportHeight) + 'px' : historyLines.length * cellHeight + 'px';
          historyRows.style.display = enabled ? 'none' : '';
          root.scrollTop = enabled ? wheelCenter : root.scrollHeight;
          wheelTop = root.scrollTop;
          followLive = true;
        };

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
          if (wheelHtml) {
            wheelHtml.style.color = activeTheme.foreground;
            wheelHtml.style.background = activeTheme.background;
          }
          blend.style.color = activeTheme.foreground;
          blend.style.background = activeTheme.background;
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
          if (nativeSelectionActive || wheelViewport) return;
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
          // Keep the original touched subtree attached until touchend. A separate
          // non-interactive layer shows app redraws while its transcript scrolls.
          // Replacing that subtree drops Android's subsequent touch events.
          const overlay = wheelViewport && touching && !nativeSelectionActive;
          if (rowUpdates.revision !== presentedRevision && !nativeSelectionActive) {
            if (wheelGesture) {
              blendRows(presentedRows.join(''), rowUpdates.lines.join(''));
            } else {
              stopBlend();
            }
            presentedRows = rowUpdates.lines.slice();
            presentedRevision = rowUpdates.revision;
          }
          if (wheelHtml) {
            wheelHtml.style.display = overlay ? 'block' : 'none';
            terminalHtml.style.opacity = overlay ? '0' : '1';
            if (overlay) rowUpdates.paint(wheelHtml);
          }
          if (!nativeSelectionActive && (!interactionActive || (wheelViewport && !touching))) {
            rowUpdates.paint(terminalHtml);
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
          if (followLive && !wheelViewport) root.scrollTop = root.scrollHeight;

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
          if (wheelViewport) return;
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
          stopWheel();
          if (wheelViewport) {
            root.scrollTop = wheelCenter;
            wheelTop = wheelCenter;
          }
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
          stopWheel();
          followLive = true;
          root.scrollTo({ top: wheelViewport ? wheelCenter : root.scrollHeight, behavior: 'instant' });
          wheelTop = root.scrollTop;
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
          paneModes = next.panes || [];
          setWheelViewport(paneModes.some((p) => p.id === next.paneId && p.alternate));
          history.style.height = wheelViewport ? (wheelCenter * 2 + viewportHeight) + 'px' : historyLines.length * cellHeight + 'px';
          historyNotice.textContent = next.notice || '';
          if (followLive && !wheelViewport) root.scrollTop = root.scrollHeight;
          else if (!wheelViewport && next.reset) {
            // Pane/width changes replace coordinates. Keep the reader in history
            // at the closest physical row; never jump to the live screen.
            root.scrollTop = Math.min(oldTop, Math.max(0, historyLines.length * cellHeight - cellHeight));
          } else if (!wheelViewport) root.scrollTop = Math.max(0, oldTop - next.removed * cellHeight);
          drawHistory();
        };

        const syncNativeSelectionState = () => {
          const nextActive = hasTerminalSelection();
          if (nativeSelectionActive === nextActive) return nextActive;
          nativeSelectionActive = nextActive;
          if (nextActive) stopWheel();
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
          if (wheelViewport) {
            const delta = root.scrollTop - wheelTop;
            wheelTop = root.scrollTop;
            if (wheelGesture && delta) {
              const direction = Math.sign(delta);
              if (direction !== wheelDirection) {
                stopBlend();
                // Native slop already established a drag. Do not wait another
                // full cell before the first tick or a direction reversal.
                wheelPixels = direction * cellHeight;
                // A reverse tick must work even when the old direction hit an
                // app boundary and therefore produced no redraw.
                wheelAwaitingFrame = false;
              }
              wheelDirection = direction;
              wheelPixels += delta;
              if (wheelRAF == null) wheelRAF = requestAnimationFrame(wheelFrame);
            }
            if (!wheelGesture) return;
          }
          reportPosition();
          setInteraction(true);
          scheduleDraw();
          scheduleIdle();
        }, { passive: true });
        root.addEventListener('scrollend', settleScroll, { passive: true });

        document.addEventListener('touchstart', (event) => {
          const touch = event.touches[0];
          if (!touch) return;
          stopWheel();
          touching = true;
          moved = false;
          tapInTerminal = root.contains(event.target);
          touchRow = event.target.closest ? event.target.closest('.terminal-row') : null;
          touchX = touch.clientX;
          touchY = touch.clientY;
          wheelX = touchX - live.getBoundingClientRect().left;
          wheelY = touchY - live.getBoundingClientRect().top;
          const route = selectScrollRoute(paneModes, Math.floor(wheelX / cellWidth), Math.floor(wheelY / cellHeight));
          if (tapInTerminal && followLive && !nativeSelectionActive && !hasTerminalSelection()) {
            setWheelViewport(route === 'wheel' || (route === 'none' && wheelViewport));
          }
          wheelTop = root.scrollTop;
          wheelGesture = wheelViewport && event.touches.length === 1 && tapInTerminal &&
            !nativeSelectionActive && !hasTerminalSelection() &&
            route === 'wheel';
          setInteraction(true);
        }, { capture: true, passive: true });
        document.addEventListener('touchmove', (event) => {
          const touch = event.touches[0];
          if (event.touches.length !== 1) stopWheel();
          if (wheelGesture && touch) {
            wheelX = Math.max(0, Math.min(viewportWidth - 1, touch.clientX - live.getBoundingClientRect().left));
            wheelY = Math.max(0, Math.min(viewportHeight - 1, touch.clientY - live.getBoundingClientRect().top));
          }
          if (touch && (Math.abs(touch.clientX - touchX) > 4 || Math.abs(touch.clientY - touchY) > 4)) {
            const firstMove = !moved;
            moved = true;
            if (firstMove && wheelGesture && Math.abs(touch.clientY - touchY) > 4) {
              // Start the TUI round trip at touch slop, before the WebView's
              // native scroll callback. Native scrolling still owns distance,
              // velocity and fling; prime only its existing first-direction tick.
              wheelDirection = Math.sign(touchY - touch.clientY);
              wheelPixels = wheelDirection * cellHeight;
              wheelFrame(performance.now());
            }
          }
        }, { capture: true, passive: true });
        document.addEventListener('touchend', (event) => {
          touching = false;
          touchRow = null;
          const touch = event.changedTouches && event.changedTouches[0];
          if (tapInTerminal && !moved && touch && !syncNativeSelectionState()) emitTap(touch.clientX, touch.clientY);
          scheduleDraw();
          scheduleIdle();
        }, { capture: true, passive: true });
        document.addEventListener('touchcancel', () => {
          stopWheel();
          touching = false;
          touchRow = null;
          moved = true;
          // Retire the gesture layer on the next frame even if the app stops
          // producing output. Waiting for idle/cursor blink leaves stale pixels.
          scheduleDraw();
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

        window.__mewlaRenderSnapshot = (nextSnapshot) => {
          if (!nextSnapshot || !rowUpdates.apply(nextSnapshot)) return;
          renderSnapshot = nextSnapshot;
          // The response has arrived. Release backpressure now: waiting for a
          // separate acknowledgement RAF can miss this vsync's wheel callback
          // and add another full frame to every round trip.
          wheelAwaitingFrame = false;
          scheduleDraw();
        };

        window.__mewlaTheme = (nextTheme) => {
          if (nextTheme) {
            activeTheme = nextTheme;
            applyTheme();
            scheduleDraw();
          }
        };

        window.__mewlaHistory = applyHistory;

        window.__mewlaSetScrollContext = (sessionId, token, reason) => {
          const changed = sessionId !== scrollSessionId;
          stopWheel();
          scrollSessionId = typeof sessionId === 'string' && sessionId ? sessionId : null;
          scrollToken = typeof token === 'string' && token ? token : null;
          if (changed) {
            presentedRows = [];
            presentedRevision = -1;
            paneModes = [];
            setWheelViewport(false);
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

        window.__mewlaBlur = () => {
          stopWheel();
          touching = false;
          clearSelection();
          settleScroll();
        };

        window.__mewlaWakeRenderer = () => {
          syncViewport(false);
          scheduleDraw();
          const rootEl = document.documentElement;
          const previousTransform = rootEl.style.transform;
          rootEl.style.transform = 'translateZ(0)';
          void rootEl.offsetHeight;
          rootEl.style.transform = previousTransform || '';
        };

        window.__mewlaResumeInput = () => {
          jumpLive();
          clearSelection();
          window.__mewlaWakeRenderer();
        };

        window.__mewlaScrollToBottom = () => {
          jumpLive();
          clearSelection();
          window.__mewlaWakeRenderer();
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
