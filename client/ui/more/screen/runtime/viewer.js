// Remote screen viewer: the computer's screen as pushed video (video.js),
// plus touch gestures, local zoom and key injection. The host owns the
// authenticated WebSocket and relays the WebRTC signalling both ways; the
// picture and the input go peer to peer. generate.mjs bundles it (with
// noVNC's keyboard module, for desktop keys) for the native WebView and for
// web; app code imports only the output.
import Keyboard from "@novnc/novnc/core/input/keyboard.js";
import { createVideoPicture } from "./video.js";

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
// One wheel notch moves a Chrome page by about this many remote pixels (measured); a finger
// dragging a page scrolls it by the distance the finger moved on the picture.
const NOTCH_REMOTE = 120;
const MIN_NOTCH_LOCAL = 8;
const MAX_ZOOM = 2; // CSS pixels per remote pixel at most
// A double tap zooms the view this much past fit (and back).
const TAP_ZOOM = 2.5;
// Room kept around the field the keyboard comes up for.
const KEYBOARD_MARGIN = 48;

const STYLE = `
.mewla-screen{position:absolute;inset:0;overflow:hidden;-webkit-user-select:none;user-select:none;-webkit-touch-callout:none}
.mewla-screen video.picture{position:absolute;left:0;top:0;margin:0!important;transform-origin:0 0}
.mewla-screen video.picture{outline:none;background:#000;object-fit:fill;pointer-events:none}
.mewla-screen .touch{position:absolute;inset:0;touch-action:none;z-index:2}
.mewla-screen .cursor{position:absolute;left:0;top:0;width:18px;height:24px;display:none;pointer-events:none;z-index:3;margin:-1px 0 0 -1px}
.mewla-screen .ring{position:absolute;width:36px;height:36px;margin:-18px 0 0 -18px;border-radius:50%;border:2px solid rgba(255,255,255,.9);box-shadow:0 0 0 1px rgba(0,0,0,.35);pointer-events:none;z-index:3;animation:mewla-ring .32s ease-out forwards}
@keyframes mewla-ring{from{transform:scale(.5);opacity:1}to{transform:scale(1.15);opacity:0}}
.mewla-screen .menu{position:absolute;display:none;flex-direction:column;gap:2px;padding:6px;border-radius:16px;background:rgba(28,28,30,.96);box-shadow:0 8px 28px rgba(0,0,0,.45);z-index:4;font:500 15px/1.2 -apple-system,system-ui,Roboto,sans-serif}
.mewla-screen .menu button{all:unset;min-width:148px;min-height:44px;padding:0 14px;border-radius:11px;color:#fff;box-sizing:border-box;display:flex;align-items:center}
.mewla-screen .menu button:active{background:rgba(255,255,255,.16)}
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
  // A long press offers what a finger can't do in one gesture: a right click, a double click, a drag.
  const menuEl = doc.createElement("div");
  menuEl.className = "menu";
  menuEl.innerHTML = '<button data-act="right">Right click</button><button data-act="double">Double click</button><button data-act="drag">Drag</button>';
  root.appendChild(menuEl);
  container.appendChild(root);

  // The picture: pushed video (WebRTC) in a <video> element.
  let video = null;
  let keyboard = null;
  let picture = null;
  let fbW = 0;
  let fbH = 0;
  const view = { s: 1, tx: 0, ty: 0, fit: 1 };
  let mode = "touch";
  // Where a picture shorter than the page sits: the middle, or against the top.
  let align = "center";
  let cursor = { x: 0, y: 0 };
  let buttons = 0;
  const stats = { bytes: 0, updates: 0, since: performance.now() };
  let pendingInput = 0;
  const latencies = [];

  function live() {
    return !!video;
  }

  function sendPointer(x, y, mask) {
    const px = Math.max(0, Math.min(fbW - 1, Math.round(x)));
    const py = Math.max(0, Math.min(fbH - 1, Math.round(y)));
    if (video) video.sendPointer(px, py, mask);
  }

  // A key press and release (down undefined), or one of them.
  function sendKey(keysym, code, down) {
    if (!video) return;
    if (down === undefined) {
      video.key(keysym, true);
      video.key(keysym, false);
    } else video.key(keysym, down);
  }

  function paste(text) {
    if (video) video.paste(text);
  }

  function markInput() {
    if (!pendingInput) pendingInput = performance.now();
  }

  // ---- view transform ----------------------------------------------------

  function applyView() {
    if (!picture) return;
    picture.style.transform = `translate(${view.tx}px, ${view.ty}px) scale(${view.s})`;
    drawCursor();
  }

  function clampView() {
    const cw = root.clientWidth;
    const ch = root.clientHeight;
    const w = fbW * view.s;
    const h = fbH * view.s;
    view.tx = w <= cw ? (cw - w) / 2 : Math.min(0, Math.max(cw - w, view.tx));
    view.ty = h <= ch ? (align === "top" ? 0 : (ch - h) / 2) : Math.min(0, Math.max(ch - h, view.ty));
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
  // `dx`/`dy` are local pixels (a mouse wheel's own units, or finger travel);
  // a notch is as long on the page as a notch is on the remote screen.
  function scrollBy(x, y, dx, dy) {
    const step = Math.max(MIN_NOTCH_LOCAL, NOTCH_REMOTE * view.s);
    scrollRest.x += dx;
    scrollRest.y += dy;
    const send = (mask, count) => {
      for (let i = 0; i < Math.min(count, 12); i++) {
        sendPointer(x, y, buttons | mask);
        sendPointer(x, y, buttons);
      }
    };
    const vy = Math.trunc(scrollRest.y / step);
    if (vy) {
      send(vy > 0 ? 16 : 8, Math.abs(vy));
      scrollRest.y -= vy * step;
      markInput();
    }
    const vx = Math.trunc(scrollRest.x / step);
    if (vx) {
      send(vx > 0 ? 64 : 32, Math.abs(vx));
      scrollRest.x -= vx * step;
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

  function zoomed() {
    return view.s > view.fit * 1.02;
  }

  // Where a tap landed, for a moment.
  function ring(point) {
    const el = doc.createElement("div");
    el.className = "ring";
    el.style.left = `${point.x}px`;
    el.style.top = `${point.y}px`;
    root.appendChild(el);
    setTimeout(() => el.remove(), 360);
  }

  // The next one-finger drag drags (the long-press menu's Drag).
  let dragArmed = false;
  let menuAt = null;
  function showMenu(local, remote) {
    menuAt = remote;
    menuEl.style.display = "flex";
    const w = menuEl.offsetWidth;
    const h = menuEl.offsetHeight;
    menuEl.style.left = `${Math.max(8, Math.min(root.clientWidth - w - 8, local.x - w / 2))}px`;
    menuEl.style.top = `${local.y - h - 16 >= 8 ? local.y - h - 16 : Math.min(root.clientHeight - h - 8, local.y + 16)}px`;
  }
  function hideMenu() {
    menuEl.style.display = "none";
    menuAt = null;
  }
  menuEl.addEventListener("pointerdown", (e) => e.stopPropagation());
  menuEl.addEventListener("click", (e) => {
    const act = e.target?.dataset?.act;
    const at = menuAt;
    hideMenu();
    if (!at || !live()) return;
    if (act === "right") click(at.x, at.y, "right");
    else if (act === "double") {
      click(at.x, at.y, "left");
      click(at.x, at.y, "left");
    } else if (act === "drag") dragArmed = true;
  });

  // A touch beside the picture (the page is taller or wider than it) is not a
  // click on the computer: it would land on the picture's nearest edge.
  function besidePicture(point) {
    return point.x < 0 || point.y < 0 || point.x > fbW || point.y > fbH;
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
      beside: mode === "touch" && besidePicture(toRemote(p.clientX, p.clientY)),
      armed: mode === "touch" && dragArmed,
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
      // Touching the picture moves it like any page: a drag scrolls, and a
      // press held first (or Drag from the long-press menu) drags, selecting
      // text or moving a slider. Zoomed in, a drag moves the view instead.
      else g.kind = g.held || g.armed ? "drag" : zoomed() ? "pan" : "scroll";
      if ((g.kind === "drag" || g.kind === "scroll") && g.beside) g.kind = "none";
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
    } else if (g.kind === "scroll") {
      // Content follows the finger: the finger going up scrolls the page down.
      scrollBy(g.remote.x, g.remote.y, -dx, -dy);
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
    if (g.armed) dragArmed = false;
    if (g.kind === "scroll") scrollRest = { x: 0, y: 0 };
    if (cancelled) {
      if (g.kind === "drag") release(mode === "touch" ? toRemote(p.clientX, p.clientY).x : cursor.x, mode === "touch" ? toRemote(p.clientX, p.clientY).y : cursor.y);
      return;
    }
    if (g.kind === "drag") {
      const r = mode === "touch" ? toRemote(p.clientX, p.clientY) : cursor;
      release(r.x, r.y);
      return;
    }
    if (g.kind !== "pending" || g.beside) return;
    if (g.held) {
      if (mode === "touch") showMenu(g.start, g.remote);
      else click(g.remote.x, g.remote.y, "right");
      return;
    }
    const now = performance.now();
    // Two quick taps in one place zoom the view in there, or back out to fit (a double click is in the
    // long-press menu). The first tap has clicked already.
    if (mode === "touch" && now - lastTap.at < DOUBLE_TAP_MS && Math.hypot(lastTap.x - g.start.x, lastTap.y - g.start.y) < 28) {
      if (zoomed()) fitView();
      else {
        zoomAt(view.fit * TAP_ZOOM, g.start.x, g.start.y);
        reportZoom();
      }
      lastTap = { at: 0, x: 0, y: 0, remote: lastTap.remote };
      return;
    }
    const target = mode === "trackpad" ? cursor : g.remote;
    click(target.x, target.y, "left");
    if (mode === "touch") ring(g.start);
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
      else if (Math.hypot(s.cx - g.start.cx, s.cy - g.start.cy) > 12) g.decided = mode === "touch" ? "pinch" : "scroll";
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
    if (menuAt) hideMenu();
    if (!live()) return;
    try { overlay.setPointerCapture(e.pointerId); } catch {}
    if (e.pointerType === "mouse") {
      focusKeyboard();
      return mouseEvent(e, "down");
    }
    pointers.set(e.pointerId, pointerInfo(e));
    if (pointers.size === 1) startOne(pointers.get(e.pointerId));
    else if (pointers.size === 2) startTwo();
  });
  overlay.addEventListener("pointermove", (e) => {
    if (!live()) return;
    if (e.pointerType === "mouse") return mouseEvent(e, "move");
    if (!pointers.has(e.pointerId)) return;
    pointers.set(e.pointerId, pointerInfo(e));
    if (!gesture) return;
    if (pointers.size === 1 && gesture.kind !== "two" && gesture.kind !== "done") moveOne(pointers.get(e.pointerId));
    else if (pointers.size === 2 && gesture.kind === "two") moveTwo();
  });
  const finish = (e, cancelled) => {
    if (!live()) return;
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
    if (!live()) return;
    if (e.ctrlKey) {
      const l = local(e.clientX, e.clientY);
      zoomAt(view.s * Math.exp(-e.deltaY / 200), l.x, l.y);
      reportZoom();
      return;
    }
    const r = toRemote(e.clientX, e.clientY);
    // A notch of the wheel (about 100 px, or 3 lines) is a notch on the screen.
    const unit = e.deltaMode === 1 ? 33 : 1;
    const notch = Math.max(MIN_NOTCH_LOCAL, NOTCH_REMOTE * view.s) / 100;
    scrollBy(r.x, r.y, e.deltaX * unit * notch, e.deltaY * unit * notch);
  }, { passive: false });

  // ---- keys ----------------------------------------------------------------

  // A mouse and a keyboard use the computer directly: a click gives the
  // picture the keyboard, and noVNC sends every key, modifiers and releases
  // included. (Phones type through the system keyboard, which the host owns.)
  function focusKeyboard() {
    const target = picture;
    if (target && doc.activeElement !== target) target.focus({ preventScroll: true });
  }

  // A shortcut needs its modifier to have gone down with the picture focused.
  // When the person focused it while holding Ctrl (a Ctrl-click, say), or the
  // keyboard is an automated one that reports the flag without a Control key
  // event, noVNC never saw the modifier: send it around the key.
  const MODIFIER_KEYS = [
    ["ctrlKey", ["ControlLeft", "ControlRight"], XK.Control],
    ["altKey", ["AltLeft", "AltRight"], XK.Option],
    ["metaKey", ["MetaLeft", "MetaRight"], XK.Command],
  ];
  const heldCodes = new Set();
  const borrowed = new Map(); // key code -> modifier keysyms sent on its behalf
  root.addEventListener("keydown", (e) => {
    if (!live()) return;
    if (e.code) heldCodes.add(e.code);
    if (/^(Control|Alt|Meta|Shift)/.test(e.code) || !e.code || borrowed.has(e.code)) return;
    const sent = [];
    for (const [flag, codes, keysym] of MODIFIER_KEYS) {
      if (e[flag] && !codes.some((c) => heldCodes.has(c))) {
        sendKey(keysym, codes[0], true);
        sent.push([keysym, codes[0]]);
      }
    }
    if (sent.length) borrowed.set(e.code, sent);
  }, true);
  root.addEventListener("keyup", (e) => {
    heldCodes.delete(e.code);
    const sent = borrowed.get(e.code);
    if (!sent) return;
    borrowed.delete(e.code);
    // The shortcut fired on the key's press; the modifier need not outlast it.
    if (live()) for (const [keysym, code] of sent.reverse()) sendKey(keysym, code, false);
  }, true);

  // The remote's paste reads the remote's clipboard, so the person's text goes
  // there first. The browser only offers its clipboard to a paste it is
  // allowed to run, so the key is held back (the default action stays) until
  // the paste event arrives, then the text is sent and the key follows.
  let heldPaste = null;
  function releasePaste(text) {
    const held = heldPaste;
    heldPaste = null;
    if (!held || !live()) return;
    clearTimeout(held.timer);
    if (text) paste(text);
    sendKey(0x76, "KeyV", true);
    sendKey(0x76, "KeyV", false);
    markInput();
  }
  root.addEventListener("keydown", (e) => {
    if (!live() || e.key.toLowerCase() !== "v" || !(e.ctrlKey || e.metaKey) || e.altKey || e.shiftKey || e.repeat) return;
    if (!navigator.clipboard && !("onpaste" in doc)) return;
    e.stopPropagation(); // not to noVNC
    heldPaste = { timer: setTimeout(() => releasePaste(""), 250) };
  }, true);
  root.addEventListener("paste", (e) => {
    if (!heldPaste) return;
    e.preventDefault();
    releasePaste(e.clipboardData ? e.clipboardData.getData("text/plain") : "");
  });
  root.addEventListener("keyup", (e) => {
    // The release of a held-back V is not for the remote: its press goes out
    // with the paste.
    if (e.key.toLowerCase() === "v" && heldPaste) e.stopPropagation();
  }, true);

  // Full size, with the browser's own shortcuts (Ctrl+W, Ctrl+T) handed to the
  // remote where the browser allows it.
  function fullSize() {
    if (!root.requestFullscreen) return;
    root.requestFullscreen().then(() => {
      if (navigator.keyboard && navigator.keyboard.lock) navigator.keyboard.lock().catch(() => {});
    }, () => {});
  }
  doc.addEventListener("fullscreenchange", () => {
    if (!doc.fullscreenElement && navigator.keyboard && navigator.keyboard.unlock) navigator.keyboard.unlock();
  });

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
    for (const k of held) sendKey(k, null, true);
    action();
    for (const k of held.reverse()) sendKey(k, null, false);
    markInput();
  }

  function typeText(text, mods) {
    for (const ch of text) {
      const keysym = keysymFor(ch);
      withModifiers(mods, () => sendKey(keysym, null));
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

  // Called when a picture has been shown, to time input → picture.
  function onDrawn() {
    if (pendingInput) {
      const ms = performance.now() - pendingInput;
      pendingInput = 0;
      if (ms < 5000) {
        latencies.push(ms);
        if (latencies.length > 20) latencies.shift();
      }
    }
  }

  const statsTimer = setInterval(async () => {
    if (!live()) return;
    stats.bytes += await video.received();
    if (!live()) return;
    const now = performance.now();
    const seconds = (now - stats.since) / 1000;
    post({
      type: "stats",
      fps: stats.updates / seconds,
      kbps: (stats.bytes * 8) / 1000 / seconds,
      inputMs: median(latencies),
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

  function pictureSized(width, height) {
    if (width === fbW && height === fbH) return;
    fbW = width;
    fbH = height;
    // A new picture (a window laid out for the phone, or put back): nothing tapped on the old one counts.
    lastTap = { at: 0, x: 0, y: 0 };
    beforeKeyboard = null;
    if (!cursor.x && !cursor.y) cursor = { x: fbW / 2, y: fbH / 2 };
    fitView();
  }

  // Pushed video: the computer offers, this page answers through the host.
  function startVideo() {
    const ticket = opening;
    let connected = false;
    video = createVideoPicture(doc, {
      signal: (signal) => post({ type: "signal", signal }),
      resized: (width, height) => {
        if (ticket !== opening) return;
        picture = video.element;
        pictureSized(width, height);
        if (!connected) {
          connected = true;
          post({ type: "connected", width, height });
        }
      },
      frame: () => {
        if (ticket !== opening) return;
        stats.updates++;
        onDrawn();
      },
      failed: () => {
        if (ticket === opening) post({ type: "disconnected", clean: false });
      },
    });
    root.prepend(video.element);
    // The browser's keys, as noVNC reads them for its picture: a click gives the picture the keyboard.
    keyboard = new Keyboard(video.element);
    keyboard.onkeyevent = (keysym, code, down) => {
      sendKey(keysym, code, down);
      if (down) markInput();
    };
    keyboard.grab();
  }

  let opening = 0;
  function open() {
    close(false);
    ++opening;
    startVideo();
  }

  function close(notify) {
    opening++;
    if (keyboard) {
      try { keyboard.ungrab(); } catch {}
      keyboard = null;
    }
    if (!video) return;
    const old = video;
    video = null;
    picture = null;
    fbW = fbH = 0;
    buttons = 0;
    pointers.clear();
    gesture = null;
    old.close();
    if (notify) post({ type: "disconnected", clean: true });
  }

  function handle(message) {
    switch (message.type) {
      case "open": open(); break;
      case "close": close(false); break;
      case "text": if (live()) typeText(message.text, message.mods); break;
      case "key": if (live()) withModifiers(message.mods, () => sendKey(keysymNamed(message.key), null)); break;
      case "signal": if (video) video.signal(message.signal); break;
      case "mode":
        mode = message.mode === "trackpad" ? "trackpad" : "touch";
        drawCursor();
        break;
      case "align":
        align = message.align === "top" ? "top" : "center";
        clampView();
        applyView();
        break;
      case "fit": fitView(); break;
      case "fullsize": fullSize(); break;
    }
  }

  // The view's size, for laying an agent's page out for it: on open and whenever it settles at a new size.
  let sizeTimer = 0;
  function reportSize() {
    clearTimeout(sizeTimer);
    sizeTimer = setTimeout(() => post({ type: "size", width: root.clientWidth, height: root.clientHeight, dpr: globalThis.devicePixelRatio || 1 }), 120);
  }
  reportSize();

  let lastSize = { w: root.clientWidth, h: root.clientHeight };
  // The scale the view had before the keyboard came up, while it is up; null when it isn't.
  let beforeKeyboard = null;
  const resizeObserver = new ResizeObserver(() => {
    reportSize();
    const w = root.clientWidth;
    const h = root.clientHeight;
    const last = lastSize;
    lastSize = { w, h };
    if (!fbW) return;
    const wasFit = view.s <= view.fit * 1.02;
    view.fit = Math.min(w / fbW, h / fbH);
    if (w === last.w && h < last.h) {
      // The keyboard came up: the picture keeps its size and the view moves so the field tapped last stays
      // in sight above it.
      if (beforeKeyboard === null) beforeKeyboard = { fit: wasFit, height: last.h };
      const at = lastTap.remote;
      if (at) {
        const y = view.ty + at.y * view.s;
        if (y > h - KEYBOARD_MARGIN) view.ty -= y - (h - KEYBOARD_MARGIN);
        if (y < KEYBOARD_MARGIN) view.ty += KEYBOARD_MARGIN - y;
      }
      clampView();
      applyView();
      return;
    }
    if (w === last.w && h > last.h && beforeKeyboard) {
      const back = beforeKeyboard;
      if (h < back.height) {
        clampView();
        applyView();
        return;
      }
      beforeKeyboard = null;
      if (back.fit) return fitView();
      clampView();
      applyView();
      return;
    }
    beforeKeyboard = null;
    if (wasFit) fitView();
    else { clampView(); applyView(); }
  });
  resizeObserver.observe(root);

  return {
    handle(batch) { for (const message of batch) handle(message); },
    destroy() {
      close(false);
      clearInterval(statsTimer);
      clearTimeout(sizeTimer);
      resizeObserver.disconnect();
      container.removeChild(root);
    },
  };
}
