// xclick X Y: click at screen coordinates on $DISPLAY (XTest). Bench helper
// for approving Chrome's own prompts on an isolated display.
#include <X11/Xlib.h>
#include <X11/extensions/XTest.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
int main(int argc, char **argv) {
  if (argc < 3) { fprintf(stderr, "usage: xclick X Y\n"); return 2; }
  Display *d = XOpenDisplay(NULL);
  if (!d) { fprintf(stderr, "no display\n"); return 1; }
  XTestFakeMotionEvent(d, -1, atoi(argv[1]), atoi(argv[2]), 0);
  XFlush(d); usleep(50000);
  XTestFakeButtonEvent(d, 1, True, 0); XFlush(d); usleep(50000);
  XTestFakeButtonEvent(d, 1, False, 0); XFlush(d);
  XCloseDisplay(d);
  return 0;
}
