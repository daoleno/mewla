// xuser: a simulated person on an isolated X display, for the web non-interference probe.
//
//   xuser TITLE_SUBSTRING INTERVAL_MS
//
// Finds the top-level window whose title contains TITLE_SUBSTRING (the user page in Chrome), gives it the keyboard
// and the top of the stack, then every INTERVAL_MS types the next character with XTest, as a hand would. 20 times a
// second it samples the real input focus, the topmost mapped window and the real pointer. One JSON line per event on
// stdout; on SIGTERM it prints a final {"ev":"summary", ...} line and exits.
//
// This is the web counterpart of CDB's BenchSentinel (trycua/cua libs/cua-driver-bench): it measures four of the
// eight "user undisturbed" criteria (keyboard focus, window raised over the user, typing lost, real pointer moved).
// It does not measure clipboard, Space switches or dialogs, and says so in every row.
//
// Build: cc -O2 -o xuser xuser.c -lX11 -lXtst
#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <X11/Xatom.h>
#include <X11/extensions/XTest.h>
#include <X11/keysym.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

static volatile sig_atomic_t stop = 0;
static void onterm(int s) { (void)s; stop = 1; }
static double now(void) { struct timespec t; clock_gettime(CLOCK_MONOTONIC, &t); return t.tv_sec + t.tv_nsec / 1e9; }

static Window top_of(Display *d, Window w) {
  Window root, parent, *kids; unsigned n;
  for (;;) {
    if (!XQueryTree(d, w, &root, &parent, &kids, &n)) return w;
    if (kids) XFree(kids);
    if (parent == root || parent == 0) return w;
    w = parent;
  }
}

static int title_has(Display *d, Window w, const char *needle) {
  char *name = NULL;
  if (XFetchName(d, w, &name) && name) { int ok = strstr(name, needle) != NULL; XFree(name); if (ok) return 1; }
  Atom net = XInternAtom(d, "_NET_WM_NAME", False), utf8 = XInternAtom(d, "UTF8_STRING", False), type;
  int fmt; unsigned long len, after; unsigned char *prop = NULL;
  if (XGetWindowProperty(d, w, net, 0, 256, False, utf8, &type, &fmt, &len, &after, &prop) == Success && prop) {
    int ok = strstr((char *)prop, needle) != NULL; XFree(prop); return ok;
  }
  return 0;
}

static Window find(Display *d, Window w, const char *needle) {
  if (title_has(d, w, needle)) return w;
  Window root, parent, *kids; unsigned n;
  if (!XQueryTree(d, w, &root, &parent, &kids, &n)) return 0;
  Window hit = 0;
  for (unsigned i = 0; i < n && !hit; i++) hit = find(d, kids[i], needle);
  if (kids) XFree(kids);
  return hit;
}

static Window topmost_mapped(Display *d, Window root) {
  Window r, p, *kids; unsigned n; Window top = 0;
  if (!XQueryTree(d, root, &r, &p, &kids, &n)) return 0;
  for (unsigned i = 0; i < n; i++) {  // bottom to top: the last mapped one wins
    XWindowAttributes a;
    if (XGetWindowAttributes(d, kids[i], &a) && a.map_state == IsViewable && a.class == InputOutput && a.width > 100) top = kids[i];
  }
  if (kids) XFree(kids);
  return top;
}

// Whether window w's frame overlaps the person's frame (root coordinates). A window put somewhere that does not cover
// the person's is raised in the stacking sense but is not "in the way".
static int overlaps(Display *d, Window w, Window utop) {
  XWindowAttributes a, b;
  if (!XGetWindowAttributes(d, w, &a) || !XGetWindowAttributes(d, utop, &b)) return 1;
  return a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height;
}

// One window as a JSON fragment: id, override_redirect (Chrome's own popups: omnibox, bubbles), class, title, geometry.
static void describe(Display *d, Window w, char *out, size_t n) {
  XWindowAttributes a; XClassHint ch = {0}; char *name = NULL; char cls[64] = "", title[64] = "";
  int ok = XGetWindowAttributes(d, w, &a);
  if (XGetClassHint(d, w, &ch)) { if (ch.res_class) snprintf(cls, sizeof cls, "%s", ch.res_class); if (ch.res_name) XFree(ch.res_name); if (ch.res_class) XFree(ch.res_class); }
  if (XFetchName(d, w, &name) && name) { snprintf(title, sizeof title, "%s", name); XFree(name); }
  for (char *c = title; *c; c++) if (*c == '"' || *c == '\\' || *c < 32) *c = ' ';
  snprintf(out, n, "\"id\":%lu,\"override\":%d,\"cls\":\"%s\",\"name\":\"%s\",\"geom\":\"%d,%d %dx%d\"", (unsigned long)w,
           ok ? a.override_redirect : -1, cls, title, ok ? a.x : 0, ok ? a.y : 0, ok ? a.width : 0, ok ? a.height : 0);
}

static void type_char(Display *d, char c) {
  KeySym ks = XStringToKeysym((char[]){c, 0});
  KeyCode kc = XKeysymToKeycode(d, ks);
  if (!kc) return;
  XTestFakeKeyEvent(d, kc, True, 0); XFlush(d); usleep(8000);
  XTestFakeKeyEvent(d, kc, False, 0); XFlush(d);
}

int main(int argc, char **argv) {
  if (argc < 3) { fprintf(stderr, "usage: xuser TITLE INTERVAL_MS\n"); return 2; }
  int interval = atoi(argv[2]);
  Display *d = XOpenDisplay(NULL);
  if (!d) { fprintf(stderr, "no display\n"); return 1; }
  signal(SIGTERM, onterm); signal(SIGINT, onterm);
  Window root = DefaultRootWindow(d), user = 0;
  for (int i = 0; i < 100 && !user; i++) { user = find(d, root, argv[1]); if (!user) usleep(100000); }
  if (!user) { printf("{\"ev\":\"error\",\"msg\":\"user window not found\"}\n"); return 1; }
  Window utop = top_of(d, user);
  XRaiseWindow(d, utop);
  XSetInputFocus(d, utop, RevertToPointerRoot, CurrentTime);
  // With a window manager running, the person gets the window the way a click on it would: _NET_ACTIVE_WINDOW
  // (source 2, a pager), so the WM's own idea of the active window is theirs too.
  if (XGetSelectionOwner(d, XInternAtom(d, "WM_S0", False)) != None || getenv("XUSER_WM")) {
    XEvent ev = {0};
    ev.xclient.type = ClientMessage; ev.xclient.window = user; ev.xclient.message_type = XInternAtom(d, "_NET_ACTIVE_WINDOW", False);
    ev.xclient.format = 32; ev.xclient.data.l[0] = 2; ev.xclient.data.l[1] = CurrentTime;
    XSendEvent(d, root, False, SubstructureRedirectMask | SubstructureNotifyMask, &ev);
  }
  XFlush(d);
  usleep(300000);

  int px0, py0, rx, ry; unsigned mask; Window rw, cw;
  XQueryPointer(d, root, &rw, &cw, &rx, &ry, &px0, &py0, &mask);
  int lastx = rx, lasty = ry;
  Window lastfocus = 0, lasttop = 0; int rev;
  XGetInputFocus(d, &lastfocus, &rev);
  int typed = 0, focus_away = 0, raised = 0, raised_covering = 0, raised_popup = 0, pointer_moves = 0, typed_not_in_user = 0;
  char desc[512];
  double covered = 0, lastsample = now();  // seconds the person's window had another window over it
  double t0 = now(), next_type = t0 + interval / 1000.0;
  printf("{\"ev\":\"start\",\"user_top\":%lu}\n", (unsigned long)utop);
  fflush(stdout);
  while (!stop) {
    usleep(50000);
    double t = now() - t0;
    Window f; XGetInputFocus(d, &f, &rev);
    int in_user = (f != 0 && f != PointerRoot && top_of(d, f) == utop);
    if (f != lastfocus) {
      describe(d, f && f != PointerRoot ? top_of(d, f) : root, desc, sizeof desc);
      printf("{\"ev\":\"focus\",\"t\":%.2f,\"in_user\":%d,%s}\n", t, in_user, desc);
      if (!in_user) focus_away++;
      lastfocus = f;
    }
    Window top = topmost_mapped(d, root);
    {
      double n = now();
      if (top && top != utop && overlaps(d, top, utop)) covered += n - lastsample;
      lastsample = n;
    }
    if (top != lasttop) {
      if (lasttop && top != utop) {
        XWindowAttributes a; int popup = XGetWindowAttributes(d, top, &a) && a.override_redirect;
        describe(d, top, desc, sizeof desc);
        // A popup (override_redirect: Chrome's omnibox, bubbles) is not another window of the person's desktop being
        // put over theirs; counted apart so the two can be told.
        int covers = !popup && overlaps(d, top, utop);
        if (popup) raised_popup++; else { raised++; if (covers) raised_covering++; }
        printf("{\"ev\":\"raised\",\"t\":%.2f,\"popup\":%d,\"covers\":%d,%s}\n", t, popup, covers, desc);
      }
      lasttop = top;
    }
    XQueryPointer(d, root, &rw, &cw, &rx, &ry, &px0, &py0, &mask);
    if (abs(rx - lastx) > 1 || abs(ry - lasty) > 1) { pointer_moves++; printf("{\"ev\":\"pointer\",\"t\":%.2f}\n", t); lastx = rx; lasty = ry; }
    if (now() >= next_type) {
      next_type += interval / 1000.0;
      if (!in_user) typed_not_in_user++;
      type_char(d, 'a' + (typed % 26)); typed++;
      printf("{\"ev\":\"type\",\"n\":%d,\"in_user\":%d}\n", typed, in_user);
    }
    fflush(stdout);
  }
  printf("{\"ev\":\"summary\",\"typed\":%d,\"typed_not_in_user\":%d,\"focus_away\":%d,\"raised\":%d,\"raised_covering\":%d,\"raised_popup\":%d,\"pointer_moves\":%d,\"covered_ms\":%d}\n",
         typed, typed_not_in_user, focus_away, raised, raised_covering, raised_popup, pointer_moves, (int)(covered * 1000));
  fflush(stdout);
  return 0;
}
