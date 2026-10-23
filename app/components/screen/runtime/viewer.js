// Remote screen viewer: noVNC rendering plus touch gestures, local zoom and
// key injection. It never opens a network connection: the host owns the
// authenticated WebSocket and relays RFB bytes as base64 both ways.
// scripts/generate-screen-viewer.mjs bundles it with noVNC for the native
// WebView (via bootstrap.js) and for web; app code imports only the output.
import RFB from "@novnc/novnc";

const XK = {
  BackSpace: 0xff08, Tab: 0xff09, Return: 0xff0d, Escape: 0xff1b, Delete: 0xffff,
  Home: 0xff50, Left: 0xff51, Up: 0xff52, Right: 0xff53, Down: 0xff54,
  PageUp: 0xff55, PageDown: 0xff56, End: 0xff57,
  Shift: 0xffe1, Control: 0xffe3, Option: 0xffe9, Command: 0xffeb,
  F1: 0xffbe,
};

const SLOP = 9;
const LONG_PRESS_MS = 480;
const DOUBLE_TAP_MS = 380;
const SCROLL_STEP = 14;
const MAX_ZOOM = 2; // CSS pixels per remote pixel at most

const STYLE = `
.mewla-screen{position:absolute;inset:0;overflow:hidden;-webkit-user-select:none;user-select:none;-webkit-touch-callout:none}
.mewla-screen .rfb,.mewla-screen .rfb>div{position:absolute!important;inset:0;display:block!important;overflow:hidden!important;background:transparent!important}
.mewla-screen .rfb canvas{position:absolute;left:0;top:0;margin:0!important;transform-origin:0 0}
.mewla-screen .touch{position:absolute;inset:0;touch-action:none;z-index:2}
.mewla-screen .cursor{position:absolute;left:0;top:0;width:18px;height:24px;display:none;pointer-events:none;z-index:3;margin:-1px 0 0 -1px}
`;

/**
 * Mounts a viewer into `container` and returns its host interface. `post`
 * receives viewer messages; `handle` takes host message batches.
 */
export function mountScreenViewer(container, post) {
  const doc = container.ownerDocument;
  if (!doc.getElementById("mewla-screen-style")) {
    const style = doc.createElement("style");
    style.id = "mewla-screen-style";
    style.textContent = STYLE;
    doc.head.appendChild(style);
  }
  const root = doc.createElement("div");
  root.className = "mewla-screen";
  const overlay = doc.createElement("div");
  overlay.className = "touch";
  root.appendChild(overlay);
  root.insertAdjacentHTML("beforeend", '<svg class="cursor" viewBox="0 0 18 24"><path d="M1 1v19l5-5 3.5 7 3-1.5L9 13.5h7z" fill="#000" stroke="#fff" stroke-width="1.5" stroke-linejoin="round"/></svg>');
  const cursorEl = root.lastChild;
  container.appendChild(root);

  let rfb = null;
  let canvas = null;
  let channel = null;
  let fbW = 0;
  let fbH = 0;
  const view = { s: 1, tx: 0, ty: 0, fit: 1 };
  let mode = "touch";
  let cursor = { x: 0, y: 0 };
  let buttons = 0;
  const stats = { bytes: 0, updates: 0, since: performance.now() };
  let pendingInput = 0;
  const latencies = [];
  // Where an update's time goes on this side: the hop from the app into this
  // page (bridge), and from handing noVNC its bytes to the first redraw
  // (decode and draw). Both are medians of the latest few.
  const bridge = [];
  const present = [];
  let presentFrom = 0;


  function bytesToBase64(bytes) {
    let binary = "";
    for (let i = 0; i < bytes.length; i += 0x8000) {
      binary += String.fromCharCode.apply(null, bytes.subarray(i, i + 0x8000));
    }
    return btoa(binary);
  }

  function base64ToBytes(text) {
    const binary = atob(text);
    const out = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i);
    return out;
  }

  // A WebSocket-shaped channel for noVNC. Writes go to the host; the host
  // delivers server bytes through screenHost({type:"rfb"}).
  function makeChannel() {
    return {
      binaryType: "arraybuffer",
      protocol: "",
      readyState: "open",
      onopen: null,
      onmessage: null,
      onclose: null,
      onerror: null,
      send(data) {
        const bytes = data instanceof Uint8Array ? data : new Uint8Array(data);
        post({ type: "send", data: bytesToBase64(bytes) });
      },
      close() {
        if (this.readyState === "closed") return;
        this.readyState = "closed";
        post({ type: "closed" });
      },
    };
  }

  function sendRaw(bytes) {
    if (channel && channel.readyState === "open") channel.send(bytes);
  }

  function sendPointer(x, y, mask) {
    const px = Math.max(0, Math.min(fbW - 1, Math.round(x)));
    const py = Math.max(0, Math.min(fbH - 1, Math.round(y)));
    sendRaw(new Uint8Array([5, mask, px >> 8, px & 255, py >> 8, py & 255]));
  }

  function markInput() {
    if (!pendingInput) pendingInput = performance.now();
  }

  // ---- view transform ----------------------------------------------------

  function applyView() {
    if (!canvas) return;
    canvas.style.transform = `translate(${view.tx}px, ${view.ty}px) scale(${view.s})`;
    drawCursor();
  }

  function clampView() {
    const cw = root.clientWidth;
    const ch = root.clientHeight;
    const w = fbW * view.s;
    const h = fbH * view.s;
    view.tx = w <= cw ? (cw - w) / 2 : Math.min(0, Math.max(cw - w, view.tx));
    view.ty = h <= ch ? (ch - h) / 2 : Math.min(0, Math.max(ch - h, view.ty));
  }

  function fitView() {
    if (!fbW || !fbH) return;
    view.fit = Math.min(root.clientWidth / fbW, root.clientHeight / fbH);
    view.s = view.fit;
    clampView();
    applyView();
    reportZoom();
  }

  function zoomAt(scale, cx, cy) {
    const max = Math.max(view.fit, MAX_ZOOM);
    const next = Math.max(view.fit, Math.min(max, scale));
    const fx = (cx - view.tx) / view.s;
    const fy = (cy - view.ty) / view.s;
    view.s = next;
    view.tx = cx - fx * next;
    view.ty = cy - fy * next;
    clampView();
    applyView();
  }

  let lastZoomReport = 0;
  function reportZoom() {
    const zoom = view.fit ? view.s / view.fit : 1;
    if (Math.abs(zoom - lastZoomReport) < 0.01) return;
    lastZoomReport = zoom;
    post({ type: "zoom", zoom });
  }

  function toRemote(clientX, clientY) {
    const r = root.getBoundingClientRect();
    return { x: (clientX - r.left - view.tx) / view.s, y: (clientY - r.top - view.ty) / view.s };
  }

  function local(clientX, clientY) {
    const r = root.getBoundingClientRect();
    return { x: clientX - r.left, y: clientY - r.top };
  }

  function drawCursor() {
    if (mode !== "trackpad" || !fbW) {
      cursorEl.style.display = "none";
      return;
    }
    cursorEl.style.display = "block";
    cursorEl.style.transform = `translate(${view.tx + cursor.x * view.s}px, ${view.ty + cursor.y * view.s}px)`;
  }

  // Keep the trackpad cursor on screen while zoomed in.
  function followCursor() {
    const margin = 40;
    const sx = view.tx + cursor.x * view.s;
    const sy = view.ty + cursor.y * view.s;
    if (sx < margin) view.tx += margin - sx;
    if (sx > root.clientWidth - margin) view.tx -= sx - (root.clientWidth - margin);
    if (sy < margin) view.ty += margin - sy;
    if (sy > root.clientHeight - margin) view.ty -= sy - (root.clientHeight - margin);
    clampView();
  }

  // ---- pointer output ------------------------------------------------------

  let moveQueued = null;
  function queueMove(x, y) {
    const first = moveQueued === null;
    moveQueued = { x, y };
    if (!first) return;
    requestAnimationFrame(() => {
      if (!moveQueued) return;
      sendPointer(moveQueued.x, moveQueued.y, buttons);
      moveQueued = null;
    });
  }

  function flushMove() {
    if (moveQueued) sendPointer(moveQueued.x, moveQueued.y, buttons);
    moveQueued = null;
  }

  function click(x, y, button) {
    flushMove();
    sendPointer(x, y, 0);
    markInput();
    if (button === "left") {
      sendPointer(x, y, 1);
      sendPointer(x, y, 0);
      return;
    }
    // Menus that open on press close again on an instant release (Qt does),
    // so a right click holds the button briefly like a real one.
    sendPointer(x, y, 4);
    setTimeout(() => sendPointer(x, y, buttons), 90);
  }

  function press(x, y) {
    flushMove();
    buttons = 1;
    sendPointer(x, y, buttons);
    markInput();
  }

  function release(x, y) {
    flushMove();
    buttons = 0;
    sendPointer(x, y, 0);
    markInput();
  }

  // RFB wheel: buttons 4/5 vertical and 6/7 horizontal, one press per notch.
  let scrollRest = { x: 0, y: 0 };
  function scrollBy(x, y, dx, dy) {
    scrollRest.x += dx;
    scrollRest.y += dy;
    const send = (mask, count) => {
      for (let i = 0; i < Math.min(count, 12); i++) {
        sendPointer(x, y, buttons | mask);
        sendPointer(x, y, buttons);
      }
    };
    const vy = Math.trunc(scrollRest.y / SCROLL_STEP);
    if (vy) {
      send(vy > 0 ? 16 : 8, Math.abs(vy));
      scrollRest.y -= vy * SCROLL_STEP;
      markInput();
    }
    const vx = Math.trunc(scrollRest.x / SCROLL_STEP);
    if (vx) {
      send(vx > 0 ? 64 : 32, Math.abs(vx));
      scrollRest.x -= vx * SCROLL_STEP;
      markInput();
    }
  }

  // ---- gestures ------------------------------------------------------------

  const pointers = new Map();
  let gesture = null;
  let lastTap = { at: 0, x: 0, y: 0 };

  function haptic() {
    post({ type: "haptic" });
  }

  function startOne(p) {
    const start = { x: p.x, y: p.y };
    gesture = {
      kind: "pending",
      start,
      last: start,
      at: performance.now(),
      held: false,
      remote: mode === "trackpad" ? { ...cursor } : toRemote(p.clientX, p.clientY),
      afterTap: mode === "trackpad" && performance.now() - lastTap.at < DOUBLE_TAP_MS,
    };
    const g = gesture;
    g.timer = setTimeout(() => {
      if (gesture !== g || g.kind !== "pending") return;
      g.held = true;
      haptic();
      if (mode === "touch") queueMove(g.remote.x, g.remote.y);
    }, LONG_PRESS_MS);
    if (g.afterTap) {
      // Tap, then press and drag: drag with the button held (trackpad mode).
      clearTimeout(g.timer);
      g.kind = "drag";
      press(cursor.x, cursor.y);
    }
  }

  function moveOne(p) {
    const g = gesture;
    const dx = p.x - g.last.x;
    const dy = p.y - g.last.y;
    const moved = Math.hypot(p.x - g.start.x, p.y - g.start.y) > SLOP;
    if (g.kind === "pending" && moved) {
      clearTimeout(g.timer);
      if (mode === "trackpad") g.kind = g.held ? "drag" : "move";
      else if (g.held || view.s <= view.fit * 1.02) g.kind = "drag";
      else g.kind = "pan";
      if (g.kind === "drag") {
        if (mode === "touch") press(g.remote.x, g.remote.y);
        else press(cursor.x, cursor.y);
      }
    }
    g.last = { x: p.x, y: p.y };
    if (g.kind === "pan") {
      view.tx += dx;
      view.ty += dy;
      clampView();
      applyView();
    } else if (g.kind === "drag" && mode === "touch") {
      const r = toRemote(p.clientX, p.clientY);
      queueMove(r.x, r.y);
    } else if ((g.kind === "move" || g.kind === "drag") && mode === "trackpad") {
      const speed = Math.hypot(dx, dy);
      const accel = Math.min(2.6, 1 + speed / 18);
      // At fit, a swipe across the phone crosses the remote screen; zoomed in,
      // the cursor slows by the square root of the zoom for precision.
      const unit = (accel * 1.2) / (view.fit * Math.sqrt(view.s / view.fit));
      cursor.x = Math.max(0, Math.min(fbW - 1, cursor.x + dx * unit));
      cursor.y = Math.max(0, Math.min(fbH - 1, cursor.y + dy * unit));
      followCursor();
      applyView();
      queueMove(cursor.x, cursor.y);
    }
  }

  function endOne(p, cancelled) {
    const g = gesture;
    gesture = null;
    clearTimeout(g.timer);
    if (cancelled) {
      if (g.kind === "drag") release(mode === "touch" ? toRemote(p.clientX, p.clientY).x : cursor.x, mode === "touch" ? toRemote(p.clientX, p.clientY).y : cursor.y);
      return;
    }
    if (g.kind === "drag") {
      const r = mode === "touch" ? toRemote(p.clientX, p.clientY) : cursor;
      release(r.x, r.y);
      return;
    }
    if (g.kind !== "pending") return;
    if (g.held) {
      click(g.remote.x, g.remote.y, "right");
      return;
    }
    // A tap. In touch mode a second tap near the first lands on the same
    // remote point so macOS sees a double-click.
    let target = mode === "trackpad" ? cursor : g.remote;
    const now = performance.now();
    if (mode === "touch" && now - lastTap.at < DOUBLE_TAP_MS && Math.hypot(lastTap.x - g.start.x, lastTap.y - g.start.y) < 28) {
      target = lastTap.remote;
    }
    click(target.x, target.y, "left");
    lastTap = { at: now, x: g.start.x, y: g.start.y, remote: { x: target.x, y: target.y } };
  }

  function twoFingerState() {
    const [a, b] = [...pointers.values()];
    return {
      dist: Math.hypot(a.x - b.x, a.y - b.y),
      cx: (a.x + b.x) / 2,
      cy: (a.y + b.y) / 2,
      clientX: (a.clientX + b.clientX) / 2,
      clientY: (a.clientY + b.clientY) / 2,
    };
  }

  function startTwo() {
    if (gesture && gesture.kind === "drag") {
      const r = mode === "touch" ? gesture.remote : cursor;
      release(r.x, r.y);
    }
    if (gesture) clearTimeout(gesture.timer);
    const s = twoFingerState();
    gesture = { kind: "two", decided: null, start: s, last: s, scale: view.s, at: performance.now() };
  }

  function moveTwo() {
    const g = gesture;
    const s = twoFingerState();
    if (!g.decided) {
      if (Math.abs(s.dist - g.start.dist) > 26) g.decided = "pinch";
      else if (Math.hypot(s.cx - g.start.cx, s.cy - g.start.cy) > 12) g.decided = "scroll";
    }
    if (g.decided === "pinch") {
      zoomAt(g.scale * (s.dist / g.start.dist), s.cx, s.cy);
      view.tx += s.cx - g.last.cx;
      view.ty += s.cy - g.last.cy;
      clampView();
      applyView();
    } else if (g.decided === "scroll") {
      const at = mode === "trackpad" ? cursor : toRemote(s.clientX, s.clientY);
      // Content follows the fingers, like a Mac trackpad's natural scrolling.
      scrollBy(at.x, at.y, g.last.cx - s.cx, g.last.cy - s.cy);
    }
    g.last = s;
  }

  function endTwo() {
    const g = gesture;
    if (!g.decided && performance.now() - g.at < 320) {
      const at = mode === "trackpad" ? cursor : toRemote(g.start.clientX, g.start.clientY);
      click(at.x, at.y, "right");
    }
    if (g.decided === "pinch") reportZoom();
    scrollRest = { x: 0, y: 0 };
    // Lifting one finger after two never turns into a tap or drag.
    gesture = { kind: "done" };
  }

  function pointerInfo(e) {
    const l = local(e.clientX, e.clientY);
    return { x: l.x, y: l.y, clientX: e.clientX, clientY: e.clientY };
  }

  // Desktop browsers with a real mouse get direct mouse semantics.
  function mouseEvent(e, phase) {
    const r = toRemote(e.clientX, e.clientY);
    const bit = e.button === 2 ? 4 : e.button === 1 ? 2 : 1;
    if (phase === "down") buttons |= bit;
    if (phase === "up") buttons &= ~bit;
    if (phase === "move") queueMove(r.x, r.y);
    else {
      flushMove();
      sendPointer(r.x, r.y, buttons);
      markInput();
    }
  }

  overlay.addEventListener("pointerdown", (e) => {
    e.preventDefault();
    if (!rfb) return;
    try { overlay.setPointerCapture(e.pointerId); } catch {}
    if (e.pointerType === "mouse") return mouseEvent(e, "down");
    pointers.set(e.pointerId, pointerInfo(e));
    if (pointers.size === 1) startOne(pointers.get(e.pointerId));
    else if (pointers.size === 2) startTwo();
  });
  overlay.addEventListener("pointermove", (e) => {
    if (!rfb) return;
    if (e.pointerType === "mouse") return mouseEvent(e, "move");
    if (!pointers.has(e.pointerId)) return;
    pointers.set(e.pointerId, pointerInfo(e));
    if (!gesture) return;
    if (pointers.size === 1 && gesture.kind !== "two" && gesture.kind !== "done") moveOne(pointers.get(e.pointerId));
    else if (pointers.size === 2 && gesture.kind === "two") moveTwo();
  });
  const finish = (e, cancelled) => {
    if (!rfb) return;
    if (e.pointerType === "mouse") return mouseEvent(e, "up");
    if (!pointers.has(e.pointerId)) return;
    const p = pointerInfo(e);
    const count = pointers.size;
    pointers.delete(e.pointerId);
    if (!gesture) return;
    if (gesture.kind === "two" && count === 2) endTwo();
    else if (count === 1 && gesture.kind !== "two" && gesture.kind !== "done") endOne(p, cancelled);
    if (pointers.size === 0) gesture = null;
  };
  overlay.addEventListener("pointerup", (e) => finish(e, false));
  overlay.addEventListener("pointercancel", (e) => finish(e, true));
  overlay.addEventListener("contextmenu", (e) => e.preventDefault());
  overlay.addEventListener("wheel", (e) => {
    e.preventDefault();
    if (!rfb) return;
    if (e.ctrlKey) {
      const l = local(e.clientX, e.clientY);
      zoomAt(view.s * Math.exp(-e.deltaY / 200), l.x, l.y);
      reportZoom();
      return;
    }
    const r = toRemote(e.clientX, e.clientY);
    scrollBy(r.x, r.y, e.deltaX, e.deltaY);
  }, { passive: false });

  // ---- keys ----------------------------------------------------------------

  function keysymFor(ch) {
    if (ch === "\n" || ch === "\r") return XK.Return;
    if (ch === "\t") return XK.Tab;
    const cp = ch.codePointAt(0);
    if (cp >= 0x20 && cp <= 0x7e) return cp;
    if (cp >= 0xa0 && cp <= 0xff) return cp;
    return 0x01000000 | cp;
  }

  function keysymNamed(name) {
    if (/^F([1-9]|1[0-2])$/.test(name)) return XK.F1 + Number(name.slice(1)) - 1;
    return XK[name] || 0;
  }

  function withModifiers(mods, action) {
    const held = (mods || []).map(keysymNamed).filter(Boolean);
    for (const k of held) rfb.sendKey(k, null, true);
    action();
    for (const k of held.reverse()) rfb.sendKey(k, null, false);
    markInput();
  }

  function typeText(text, mods) {
    for (const ch of text) {
      const keysym = keysymFor(ch);
      withModifiers(mods, () => rfb.sendKey(keysym, null));
    }
  }

  // ---- stats ---------------------------------------------------------------

  function remember(list, ms) {
    list.push(ms);
    if (list.length > 20) list.shift();
  }

  function median(list) {
    if (!list.length) return null;
    const sorted = [...list].sort((a, b) => a - b);
    return sorted[Math.floor(sorted.length / 2)];
  }

  // Called when an update has been drawn, to time input → redraw and the
  // viewer's own decode and draw.
  function onDrawn() {
    if (presentFrom) {
      remember(present, performance.now() - presentFrom);
      presentFrom = 0;
    }
    if (pendingInput) {
      const ms = performance.now() - pendingInput;
      pendingInput = 0;
      if (ms < 5000) {
        latencies.push(ms);
        if (latencies.length > 20) latencies.shift();
      }
    }
  }

  const statsTimer = setInterval(() => {
    if (!rfb) return;
    const now = performance.now();
    const seconds = (now - stats.since) / 1000;
    post({
      type: "stats",
      fps: stats.updates / seconds,
      kbps: (stats.bytes * 8) / 1000 / seconds,
      inputMs: median(latencies),
      bridgeMs: median(bridge),
      presentMs: median(present),
      width: fbW,
      height: fbH,
    });
    stats.updates = 0;
    stats.bytes = 0;
    stats.since = now;
    // An input that drew nothing (such as a click on empty desktop) must not
    // be paired with an unrelated later update.
    if (pendingInput && now - pendingInput > 3000) pendingInput = 0;
  }, 1000);

  // ---- lifecycle -----------------------------------------------------------

  function observeCanvas() {
    canvas = root.querySelector("canvas");
    if (!canvas) return;
    const resize = () => {
      if (canvas.width === fbW && canvas.height === fbH) return;
      fbW = canvas.width;
      fbH = canvas.height;
      if (!cursor.x && !cursor.y) cursor = { x: fbW / 2, y: fbH / 2 };
      fitView();
    };
    new MutationObserver(resize).observe(canvas, { attributes: true, attributeFilter: ["width", "height"] });
    resize();
  }

  function open() {
    close(false);
    channel = makeChannel();
    const target = doc.createElement("div");
    target.className = "rfb";
    root.prepend(target);
    rfb = new RFB(target, channel, { shared: true });
    rfb.scaleViewport = false;
    rfb.clipViewport = false;
    rfb.focusOnClick = false;
    rfb.background = "transparent";
    // A FramebufferUpdate ends with one display flip, which draws at once
    // unless decoded pictures are still queued; then noVNC flips again from
    // the queue when it gets there. Count updates by the first, and time
    // redraws by whichever draws.
    const display = rfb._display;
    if (display && typeof display.flip === "function") {
      const flip = display.flip.bind(display);
      display.flip = (...args) => {
        const fromQueue = args[0] === true;
        const queued = !fromQueue && display._renderQ && display._renderQ.length !== 0;
        if (!fromQueue) stats.updates++;
        const result = flip(...args);
        if (!queued) onDrawn();
        return result;
      };
    }
    rfb.addEventListener("connect", () => {
      observeCanvas();
      post({ type: "connected", width: fbW, height: fbH });
    });
    rfb.addEventListener("disconnect", (e) => post({ type: "disconnected", clean: !!e.detail.clean }));
    rfb.addEventListener("desktopname", (e) => post({ type: "name", name: e.detail.name }));
    rfb.addEventListener("bell", () => haptic());
    // noVNC attaches to the channel on a timer; hold bytes until it has.
    setTimeout(() => deliver([]), 0);
  }

  function close(notify) {
    if (!rfb) return;
    const old = rfb;
    rfb = null;
    canvas = null;
    fbW = fbH = 0;
    buttons = 0;
    pointers.clear();
    gesture = null;
    if (channel) {
      channel.readyState = "closed";
      if (channel.onclose) channel.onclose({ code: 1000, reason: "", wasClean: true });
    }
    channel = null;
    inbox = [];
    try { old.disconnect(); } catch {}
    root.querySelectorAll(".rfb").forEach((el) => el.remove());
    if (notify) post({ type: "disconnected", clean: true });
  }

  let inbox = [];
  function deliver(chunks, sentAt) {
    if (!channel) return;
    // Wall clock on both sides of the bridge: the same device.
    if (typeof sentAt === "number" && chunks.length) {
      const hop = Date.now() - sentAt;
      if (hop >= 0 && hop < 5000) remember(bridge, hop);
    }
    inbox.push(...chunks);
    if (!channel.onmessage) {
      setTimeout(() => deliver([]), 10);
      return;
    }
    const queued = inbox;
    inbox = [];
    if (queued.length && !presentFrom) presentFrom = performance.now();
    for (const chunk of queued) {
      const bytes = base64ToBytes(chunk);
      stats.bytes += bytes.length;
      channel.onmessage({ data: bytes.buffer });
    }
  }

  function handle(message) {
    switch (message.type) {
      case "open": open(); break;
      case "close": close(false); break;
      case "rfb": deliver(message.data, message.at); break;
      case "text": if (rfb) typeText(message.text, message.mods); break;
      case "key": if (rfb) withModifiers(message.mods, () => rfb.sendKey(keysymNamed(message.key), null)); break;
      case "mode":
        mode = message.mode === "trackpad" ? "trackpad" : "touch";
        drawCursor();
        break;
      case "fit": fitView(); break;
    }
  }

  const resizeObserver = new ResizeObserver(() => {
    if (!fbW) return;
    const wasFit = view.s <= view.fit * 1.02;
    view.fit = Math.min(root.clientWidth / fbW, root.clientHeight / fbH);
    if (wasFit) fitView();
    else { clampView(); applyView(); }
  });
  resizeObserver.observe(root);

  return {
    handle(batch) { for (const message of batch) handle(message); },
    destroy() {
      close(false);
      clearInterval(statsTimer);
      resizeObserver.disconnect();
      container.removeChild(root);
    },
  };
}
