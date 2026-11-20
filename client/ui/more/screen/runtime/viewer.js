// Remote screen viewer: the computer's screen as pushed video (video.js),
// touch control (core/screen/touchControl: one model, no modes), local zoom
// and key injection. The host owns the authenticated WebSocket and relays the
// WebRTC signalling both ways; the picture and the input go peer to peer.
// generate.mjs bundles it (with noVNC's keyboard module, for desktop keys) for
// the native WebView and for web; app code imports only the output.
import Keyboard from "@novnc/novnc/core/input/keyboard.js";
import { TOUCH, TouchControl } from "../../../../core/screen/touchControl";
import { createVideoPicture } from "./video.js";

const XK = {
  BackSpace: 0xff08, Tab: 0xff09, Return: 0xff0d, Escape: 0xff1b, Delete: 0xffff,
  Home: 0xff50, Left: 0xff51, Up: 0xff52, Right: 0xff53, Down: 0xff54,
  PageUp: 0xff55, PageDown: 0xff56, End: 0xff57,
  Shift: 0xffe1, Control: 0xffe3, Option: 0xffe9, Command: 0xffeb,
  F1: 0xffbe,
};

// A desktop mouse's wheel: one notch moves a Chrome page about this many remote pixels (measured).
const NOTCH_REMOTE = 120;
const MIN_NOTCH_LOCAL = 8;
// Room kept around the field the keyboard comes up for.
const KEYBOARD_MARGIN = 48;
const KNOB = TOUCH.KNOB_R * 2;

const STYLE = `
.mewla-screen{position:absolute;inset:0;overflow:hidden;-webkit-user-select:none;user-select:none;-webkit-touch-callout:none}
.mewla-screen video.picture{position:absolute;left:0;top:0;margin:0!important;transform-origin:0 0}
.mewla-screen video.picture{outline:none;background:#000;object-fit:fill;pointer-events:none}
.mewla-screen .cover{position:absolute;inset:0;background:#000;z-index:1;pointer-events:none}
.mewla-screen .touch{position:absolute;inset:0;touch-action:none;z-index:2}
.mewla-screen .cursor{position:absolute;left:0;top:0;width:18px;height:24px;display:none;pointer-events:none;z-index:3;margin:-1px 0 0 -1px;transform-origin:1px 1px}
.mewla-screen .cursor.pressed path{fill:#3a3a3c}
.mewla-screen .knob{position:absolute;left:0;top:0;width:${KNOB}px;height:${KNOB}px;margin:-${KNOB / 2}px 0 0 -${KNOB / 2}px;border-radius:50%;box-sizing:border-box;display:none;pointer-events:none;z-index:3;transition:background-color .12s,border-color .12s,opacity .12s}
.mewla-screen .knob{border:1.5px solid rgba(255,255,255,.75);background:rgba(0,0,0,.12);box-shadow:0 0 0 1px rgba(0,0,0,.25);opacity:.55}
.mewla-screen .knob.aimed{border:2px solid rgba(0,0,0,.45);background:#fff;box-shadow:0 1px 6px rgba(0,0,0,.45);opacity:1}
.mewla-screen .ring{position:absolute;width:36px;height:36px;margin:-18px 0 0 -18px;border-radius:50%;border:2px solid rgba(255,255,255,.9);box-shadow:0 0 0 1px rgba(0,0,0,.35);pointer-events:none;z-index:3;animation:mewla-ring .32s ease-out forwards}
@keyframes mewla-ring{from{transform:scale(.5);opacity:1}to{transform:scale(1.15);opacity:0}}
.mewla-screen .zoom{all:unset;position:absolute;right:12px;bottom:${TOUCH.EDGE_BOTTOM}px;width:40px;height:40px;border-radius:50%;background:rgba(28,28,30,.72);display:none;align-items:center;justify-content:center;z-index:4;box-shadow:0 0 0 1px rgba(255,255,255,.18)}
.mewla-screen .zoom:active{background:rgba(60,60,64,.9)}
.mewla-screen .zoom .out{display:none}
.mewla-screen .zoom.in .out{display:block}
.mewla-screen .zoom.in .plus{display:none}
`;

const ZOOM_ICON = '<svg width="20" height="20" viewBox="0 0 20 20" fill="none" stroke="#fff" stroke-width="1.8" stroke-linecap="round"><circle cx="8.5" cy="8.5" r="5.5"/><path d="M12.6 12.6 17 17M6 8.5h5"/><path class="plus" d="M8.5 6v5"/></svg>';

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
  // Black over the picture until its first frame is shown (the video element is never hidden: WebKit
  // stops a muted autoplay it thinks nobody sees).
  const cover = doc.createElement("div");
  cover.className = "cover";
  root.appendChild(cover);
  // The computer's pointer, drawn here at finger speed, and its handle.
  root.insertAdjacentHTML("beforeend", '<svg class="cursor" viewBox="0 0 18 24"><path d="M1 1v19l5-5 3.5 7 3-1.5L9 13.5h7z" fill="#000" stroke="#fff" stroke-width="1.5" stroke-linejoin="round"/></svg>');
  const cursorEl = root.lastChild;
  const knobEl = doc.createElement("div");
  knobEl.className = "knob";
  root.appendChild(knobEl);
  // One hand's zoom on a whole desktop: in around the pointer, and back.
  const zoomEl = doc.createElement("button");
  zoomEl.className = "zoom";
  zoomEl.setAttribute("aria-label", "Zoom");
  zoomEl.innerHTML = ZOOM_ICON;
  root.appendChild(zoomEl);
  container.appendChild(root);

  // The picture: pushed video (WebRTC) in a <video> element.
  let video = null;
  let keyboard = null;
  let picture = null;
  let fbW = 0;
  let fbH = 0;
  const control = new TouchControl({ handle: true });
  const view = control.view;
  // The computer's video carries its own cursor: the arrow is not drawn twice (the handle still is).
  let cursorInPicture = false;
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

  // ---- view and pointer ------------------------------------------------------

  function applyView() {
    if (picture) picture.style.transform = `translate(${view.tx}px, ${view.ty}px) scale(${view.s})`;
    drawPointer();
    drawZoom();
  }

  function drawPointer() {
    const p = control.pointer;
    const on = fbW && p.shown && control.visible();
    const tip = control.toLocal(p);
    cursorEl.style.display = on && !cursorInPicture ? "block" : "none";
    if (on) {
      cursorEl.style.transform = `translate(${tip.x}px, ${tip.y}px)${p.pressed ? " scale(.86)" : ""}`;
      cursorEl.classList.toggle("pressed", p.pressed);
    }
    const knob = on ? control.handlePlace() : null;
    knobEl.style.display = knob ? "block" : "none";
    if (knob) {
      knobEl.style.transform = `translate(${knob.x}px, ${knob.y}px)`;
      knobEl.classList.toggle("aimed", p.aimed);
    }
  }

  function drawZoom() {
    zoomEl.style.display = fbW && control.handle ? "flex" : "none";
    zoomEl.classList.toggle("in", !control.fitted());
  }

  let lastZoomReport = 0;
  function reportZoom() {
    const zoom = view.fit ? view.s / view.fit : 1;
    if (Math.abs(zoom - lastZoomReport) < 0.01) return;
    lastZoomReport = zoom;
    post({ type: "zoom", zoom });
  }

  function fitView() {
    run(control.fitView());
    reportZoom();
  }

  function local(clientX, clientY) {
    const r = root.getBoundingClientRect();
    return { x: clientX - r.left, y: clientY - r.top };
  }

  function toRemote(clientX, clientY) {
    return control.toRemote(local(clientX, clientY));
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
    sendPointer(x, y, buttons);
    markInput();
    if (button === "left") {
      sendPointer(x, y, buttons | 1);
      sendPointer(x, y, buttons);
      return;
    }
    // Menus that open on press close again on an instant release (Qt does),
    // so a right click holds the button briefly like a real one.
    sendPointer(x, y, buttons | 4);
    setTimeout(() => sendPointer(x, y, buttons), 90);
  }

  function setButton(x, y, down) {
    flushMove();
    buttons = down ? 1 : 0;
    sendPointer(x, y, buttons);
    markInput();
  }

  // RFB wheel: buttons 8/16 vertical and 32/64 horizontal, one press per notch.
  function wheel(x, y, dx, dy) {
    flushMove();
    const send = (mask, count) => {
      for (let i = 0; i < Math.min(count, 12); i++) {
        sendPointer(x, y, buttons | mask);
        sendPointer(x, y, buttons);
      }
    };
    if (dy) send(dy > 0 ? 16 : 8, Math.abs(dy));
    if (dx) send(dx > 0 ? 64 : 32, Math.abs(dx));
    markInput();
  }

  // What the touch control decided, done.
  function run(actions) {
    let viewChanged = false;
    let pointerChanged = false;
    for (const a of actions) {
      switch (a.type) {
        case "move": queueMove(a.x, a.y); break;
        case "button": setButton(a.x, a.y, a.down); break;
        case "click": click(a.x, a.y, a.button); break;
        case "wheel": wheel(a.x, a.y, a.dx, a.dy); break;
        case "scroll":
          flushMove();
          if (video) video.scroll(Math.round(a.x), Math.round(a.y), a.dx, a.dy);
          markInput();
          break;
        case "view": viewChanged = true; break;
        case "pointer": pointerChanged = true; break;
        case "ring": ring(a); break;
        case "haptic": post({ type: "haptic" }); break;
      }
    }
    if (viewChanged) applyView();
    else if (pointerChanged) drawPointer();
    schedule();
  }

  // The control's timers (a hold, a flick's frames).
  let timer = 0;
  function schedule() {
    clearTimeout(timer);
    const at = control.deadline();
    if (at === null) return;
    timer = setTimeout(() => {
      timer = 0;
      run(control.tick(performance.now()));
    }, Math.max(0, at - performance.now()));
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

  zoomEl.addEventListener("pointerdown", (e) => e.stopPropagation());
  zoomEl.addEventListener("click", (e) => {
    e.stopPropagation();
    run(control.zoomButton());
    reportZoom();
  });

  // ---- touch and mouse ------------------------------------------------------

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
    if (!live()) return;
    try { overlay.setPointerCapture(e.pointerId); } catch {}
    if (e.pointerType === "mouse") {
      focusKeyboard();
      return mouseEvent(e, "down");
    }
    const l = local(e.clientX, e.clientY);
    run(control.down(e.pointerId, l.x, l.y, performance.now()));
  });
  overlay.addEventListener("pointermove", (e) => {
    if (!live()) return;
    if (e.pointerType === "mouse") return mouseEvent(e, "move");
    // Every sample the touch screen took, not one per frame: the handle's speed is measured from them.
    const samples = e.getCoalescedEvents ? e.getCoalescedEvents() : [];
    for (const s of samples.length ? samples : [e]) {
      const l = local(s.clientX, s.clientY);
      run(control.move(e.pointerId, l.x, l.y, s.timeStamp || performance.now()));
    }
  });
  overlay.addEventListener("pointerup", (e) => {
    if (!live()) return;
    if (e.pointerType === "mouse") return mouseEvent(e, "up");
    const l = local(e.clientX, e.clientY);
    run(control.up(e.pointerId, l.x, l.y, performance.now()));
    reportZoom();
  });
  overlay.addEventListener("pointercancel", (e) => {
    if (!live()) return;
    if (e.pointerType === "mouse") return mouseEvent(e, "up");
    run(control.cancel(e.pointerId, performance.now()));
  });
  overlay.addEventListener("contextmenu", (e) => e.preventDefault());
  // A desktop mouse's wheel and trackpad: notches, as a wheel turns.
  let wheelRest = { x: 0, y: 0 };
  overlay.addEventListener("wheel", (e) => {
    e.preventDefault();
    if (!live()) return;
    if (e.ctrlKey) {
      const l = local(e.clientX, e.clientY);
      control.zoomAt(view.s * Math.exp(-e.deltaY / 200), l);
      applyView();
      reportZoom();
      return;
    }
    const r = toRemote(e.clientX, e.clientY);
    // A notch of the wheel (about 100 px, or 3 lines) is a notch on the screen.
    const unit = e.deltaMode === 1 ? 33 : 1;
    const step = Math.max(MIN_NOTCH_LOCAL, NOTCH_REMOTE * view.s);
    const notch = step / 100;
    wheelRest.x += e.deltaX * unit * notch;
    wheelRest.y += e.deltaY * unit * notch;
    const nx = Math.trunc(wheelRest.x / step);
    const ny = Math.trunc(wheelRest.y / step);
    wheelRest.x -= nx * step;
    wheelRest.y -= ny * step;
    if (nx || ny) wheel(r.x, r.y, nx, ny);
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
    // A new picture (a window laid out for the phone, or put back): nothing aimed at the old one counts.
    beforeKeyboard = null;
    tapped = false;
    control.viewport(root.clientWidth, root.clientHeight);
    run(control.picture(width, height));
    reportZoom();
  }

  // Pushed video: the computer offers, this page answers through the host.
  function startVideo() {
    const ticket = opening;
    let connected = false;
    video = createVideoPicture(doc, {
      signal: (signal) => post({ type: "signal", signal }),
      // What the computer's offer says it can do: scroll by pixels, and whether its video carries its cursor.
      offered: (features) => {
        if (ticket !== opening) return;
        control.fineScroll = !!features.fineScroll;
        cursorInPicture = !!features.cursor;
        drawPointer();
      },
      resized: (width, height) => {
        if (ticket !== opening) return;
        picture = video.element;
        pictureSized(width, height);
      },
      // The view is live with its first picture shown, not before: the page's own loading state covers
      // the time without one.
      frame: () => {
        if (ticket !== opening) return;
        stats.updates++;
        onDrawn();
        if (!connected && fbW) {
          connected = true;
          cover.style.display = "none";
          post({ type: "connected", width: fbW, height: fbH });
        }
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
    cover.style.display = "";
    fbW = fbH = 0;
    buttons = 0;
    clearTimeout(timer);
    control.picture(0, 0);
    drawPointer();
    drawZoom();
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
      // A window laid out for the phone is used like a phone's page: no pointer drawn, no zoom button.
      case "layout":
        run(control.setHandle(!message.reflowed));
        drawZoom();
        break;
      case "align":
        control.align = message.align === "top" ? "top" : "center";
        control.clamp();
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

  // A tap since the picture came: the keyboard keeps the point it clicked in sight.
  let tapped = false;
  overlay.addEventListener("pointerup", () => { tapped = true; });
  let lastSize = { w: root.clientWidth, h: root.clientHeight };
  // The scale the view had before the keyboard came up, while it is up; null when it isn't.
  let beforeKeyboard = null;
  const resizeObserver = new ResizeObserver(() => {
    reportSize();
    const w = root.clientWidth;
    const h = root.clientHeight;
    const last = lastSize;
    lastSize = { w, h };
    if (!fbW) {
      control.viewport(w, h);
      return;
    }
    const wasFit = control.fitted();
    if (w === last.w && h < last.h) {
      // The keyboard came up: the picture keeps its size and the view moves so the field tapped last stays
      // in sight above it.
      if (beforeKeyboard === null) beforeKeyboard = { fit: wasFit, height: last.h };
      control.size(w, h);
      if (tapped) {
        const y = control.toLocal(control.pointer).y;
        if (y > h - KEYBOARD_MARGIN) view.ty -= y - (h - KEYBOARD_MARGIN);
        if (y < KEYBOARD_MARGIN) view.ty += KEYBOARD_MARGIN - y;
      }
      control.clamp();
      applyView();
      return;
    }
    if (w === last.w && h > last.h && beforeKeyboard) {
      const back = beforeKeyboard;
      control.size(w, h);
      if (h < back.height) {
        control.clamp();
        applyView();
        return;
      }
      beforeKeyboard = null;
      if (back.fit) return fitView();
      control.clamp();
      applyView();
      return;
    }
    beforeKeyboard = null;
    run(control.viewport(w, h));
    applyView();
  });
  resizeObserver.observe(root);

  return {
    handle(batch) { for (const message of batch) handle(message); },
    destroy() {
      close(false);
      clearInterval(statsTimer);
      clearTimeout(sizeTimer);
      clearTimeout(timer);
      resizeObserver.disconnect();
      container.removeChild(root);
    },
  };
}
