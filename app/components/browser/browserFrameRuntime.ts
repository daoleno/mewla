// The web build's twin of renderer.ts: pixels only, mounted straight into the
// page because the web UI's CSP allows no srcdoc frames. It opens no
// connection; frames come in and the person's pointer input goes out through
// the same messages the native renderer posts.
export interface BrowserFrameInput { kind: string; x?: number; y?: number; dx?: number; dy?: number; button?: string }
export interface BrowserFramePayload { seq: number; data: string; metadata?: { deviceWidth?: number; deviceHeight?: number } }

export function mountBrowserFrame(container: HTMLElement, post: (message: unknown) => void) {
  const screen = document.createElement("img");
  screen.alt = "Remote host browser";
  screen.draggable = false;
  Object.assign(screen.style, { width: "100%", height: "100%", objectFit: "contain", touchAction: "none", userSelect: "none" });
  container.appendChild(screen);
  let width = 1280, height = 800, seq = 0, ready = true, start: { x: number; y: number } | null = null, scroll = false, lastMove = 0;
  const point = (e: { clientX: number; clientY: number }) => {
    const r = screen.getBoundingClientRect(), scale = Math.min(r.width / width, r.height / height);
    return {
      x: Math.max(0, Math.min(width, (e.clientX - r.left - (r.width - width * scale) / 2) / scale)),
      y: Math.max(0, Math.min(height, (e.clientY - r.top - (r.height - height * scale) / 2) / scale)),
    };
  };
  const input = (value: BrowserFrameInput) => post({ type: "input", input: value });
  screen.onpointerdown = (e) => {
    screen.setPointerCapture(e.pointerId);
    start = point(e);
    if (!scroll) input({ kind: "mousePressed", ...start, button: "left" });
  };
  screen.onpointermove = (e) => {
    if (!start || Date.now() - lastMove < 80) return;
    lastMove = Date.now();
    const p = point(e);
    if (scroll) { input({ kind: "mouseWheel", ...p, dx: start.x - p.x, dy: start.y - p.y }); start = p; }
    else input({ kind: "mouseMoved", ...p, button: "left" });
  };
  screen.onpointerup = (e) => {
    if (start && !scroll) input({ kind: "mouseReleased", ...point(e), button: "left" });
    start = null;
  };
  screen.onpointercancel = () => {
    if (start && !scroll) input({ kind: "mouseReleased", ...start, button: "left" });
    start = null;
  };
  screen.onwheel = (e) => { e.preventDefault(); input({ kind: "mouseWheel", ...point(e), dx: e.deltaX, dy: e.deltaY }); };
  return {
    frame(frame: BrowserFramePayload) {
      if (!ready || frame.seq <= seq) return;
      ready = false;
      seq = frame.seq;
      width = frame.metadata?.deviceWidth || 1280;
      height = frame.metadata?.deviceHeight || 800;
      const done = () => requestAnimationFrame(() => { ready = true; post({ type: "ack", seq: frame.seq }); });
      screen.onload = done;
      screen.onerror = () => { ready = true; post({ type: "ack", seq: frame.seq }); };
      screen.src = "data:image/jpeg;base64," + frame.data;
    },
    scroll(value: boolean) { scroll = value; },
    destroy() { screen.remove(); },
  };
}
