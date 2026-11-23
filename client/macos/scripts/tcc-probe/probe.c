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
typedef CFArrayRef (*info_fn)(CFBundleRef);

// What System Settings lists for this bundle (TCC.framework's TCCAccessCopyInformationForBundle): each service's
// entry with its switch, appended as text to $PROBE_OUT.info under the label.
static void list_info(const char *label) {
  void *tcc = dlopen("/System/Library/PrivateFrameworks/TCC.framework/TCC", RTLD_LAZY);
  info_fn fn = tcc ? (info_fn)dlsym(tcc, "TCCAccessCopyInformationForBundle") : NULL;
  const char *out = getenv("PROBE_OUT");
  char path[1024];
  snprintf(path, sizeof path, "%s.info", out ? out : "/tmp/probe");
  FILE *f = fopen(path, "a");
  if (!f) return;
  fprintf(f, "=== %s (pid %d)\n", label, getpid());
  CFBundleRef bundle = CFBundleGetMainBundle();
  CFArrayRef info = (fn && bundle) ? fn(bundle) : NULL;
  if (!info) {
    fprintf(f, "%s\n", fn ? "(null)" : "(no symbol)");
  } else {
    CFStringRef d = CFCopyDescription(info);
    char buf[16384];
    if (CFStringGetCString(d, buf, sizeof buf, kCFStringEncodingUTF8)) fputs(buf, f);
    fputc('\n', f);
    CFRelease(d);
    CFRelease(info);
  }
  fclose(f);
}

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
  list_info(label);
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
