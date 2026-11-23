// TCC probe: what macOS answers this process for Screen Recording and Accessibility, through the public checks
// (CGPreflightScreenCaptureAccess, AXIsProcessTrusted) and TCC.framework's preflight (granted / denied / unknown:
// no row), with its pid and parent. `probe check|request <label>` appends one JSON line to $PROBE_OUT.
// `request` asks first (CGRequestScreenCaptureAccess, AXIsProcessTrustedWithOptions with the prompt), as Mewla does.
#include <ApplicationServices/ApplicationServices.h>
#include <CoreGraphics/CoreGraphics.h>
#include <dlfcn.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

typedef int (*preflight_fn)(CFStringRef, CFDictionaryRef);

static int tcc_preflight(const char *service) {
  static preflight_fn fn;
  static int loaded;
  if (!loaded) {
    loaded = 1;
    void *tcc = dlopen("/System/Library/PrivateFrameworks/TCC.framework/TCC", RTLD_LAZY);
    if (tcc) fn = (preflight_fn)dlsym(tcc, "TCCAccessPreflight");
  }
  if (!fn) return -1;
  CFStringRef name = CFStringCreateWithCString(NULL, service, kCFStringEncodingUTF8);
  int result = fn(name, NULL);
  CFRelease(name);
  return result;
}

int main(int argc, char **argv) {
  const char *mode = argc > 1 ? argv[1] : "check";
  const char *label = argc > 2 ? argv[2] : "?";
  if (strcmp(mode, "request") == 0) {
    if (!CGPreflightScreenCaptureAccess()) CGRequestScreenCaptureAccess();
    const void *keys[] = {kAXTrustedCheckOptionPrompt};
    const void *values[] = {kCFBooleanTrue};
    CFDictionaryRef options = CFDictionaryCreate(NULL, keys, values, 1, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    AXIsProcessTrustedWithOptions(options);
    CFRelease(options);
    sleep(3);
  }
  char line[1024];
  snprintf(line, sizeof line,
           "{\"label\":\"%s\",\"mode\":\"%s\",\"pid\":%d,\"ppid\":%d,\"screen\":%s,\"ax\":%s,"
           "\"tcc_screen\":%d,\"tcc_ax\":%d,\"tcc_postevent\":%d}\n",
           label, mode, getpid(), getppid(), CGPreflightScreenCaptureAccess() ? "true" : "false",
           AXIsProcessTrusted() ? "true" : "false", tcc_preflight("kTCCServiceScreenCapture"),
           tcc_preflight("kTCCServiceAccessibility"), tcc_preflight("kTCCServicePostEvent"));
  const char *out = getenv("PROBE_OUT");
  FILE *f = out ? fopen(out, "a") : stdout;
  if (!f) return 1;
  fputs(line, f);
  if (f != stdout) fclose(f);
  return 0;
}
