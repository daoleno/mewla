// A virtual display for the fidelity captures (macos-fidelity.yml): a CI runner's screen is 1024 × 768 at 1×, too small
// for the design's 1280 × 800 window and too coarse for 0.5 pt hairlines. This adds a HiDPI display of the given size in
// points (CoreGraphics' CGVirtualDisplay, as Chromium's tests use it), makes it the main one, and keeps it while it runs.
//
//   clang -fobjc-arc -framework Foundation -framework CoreGraphics vdisplay.m -o vdisplay
//   ./vdisplay 1512 982 &     # prints "display <id> <w>x<h> pt, <pw>x<ph> px" once it is the main display
#import <Foundation/Foundation.h>
#import <CoreGraphics/CoreGraphics.h>

@interface CGVirtualDisplayDescriptor : NSObject
@property(retain, nonatomic) dispatch_queue_t queue;
@property(retain, nonatomic) NSString *name;
@property(nonatomic) unsigned int maxPixelsWide;
@property(nonatomic) unsigned int maxPixelsHigh;
@property(nonatomic) CGSize sizeInMillimeters;
@property(nonatomic) unsigned int productID;
@property(nonatomic) unsigned int vendorID;
@property(nonatomic) unsigned int serialNum;
@property(copy, nonatomic) void (^terminationHandler)(id, id);
@end

@interface CGVirtualDisplayMode : NSObject
- (instancetype)initWithWidth:(unsigned int)width height:(unsigned int)height refreshRate:(double)refreshRate;
@end

@interface CGVirtualDisplaySettings : NSObject
@property(retain, nonatomic) NSArray *modes;
@property(nonatomic) unsigned int hiDPI;
@end

@interface CGVirtualDisplay : NSObject
- (instancetype)initWithDescriptor:(CGVirtualDisplayDescriptor *)descriptor;
- (BOOL)applySettings:(CGVirtualDisplaySettings *)settings;
@property(readonly, nonatomic) unsigned int displayID;
@end

int main(int argc, const char *argv[]) {
  @autoreleasepool {
    unsigned int w = argc > 1 ? (unsigned int)atoi(argv[1]) : 1512;
    unsigned int h = argc > 2 ? (unsigned int)atoi(argv[2]) : 982;
    CGVirtualDisplayDescriptor *d = [[CGVirtualDisplayDescriptor alloc] init];
    d.queue = dispatch_get_main_queue();
    d.name = @"Fidelity";
    d.maxPixelsWide = w * 2;
    d.maxPixelsHigh = h * 2;
    // A 14" panel's size: about 254 pixels per inch at 2×.
    d.sizeInMillimeters = CGSizeMake(w * 2 * 25.4 / 254.0, h * 2 * 25.4 / 254.0);
    d.productID = 0x1234;
    d.vendorID = 0x3456;
    d.serialNum = 0x0001;
    d.terminationHandler = ^(id a, id b) { fprintf(stderr, "virtual display terminated\n"); };
    CGVirtualDisplay *display = [[CGVirtualDisplay alloc] initWithDescriptor:d];
    if (!display) { fprintf(stderr, "no CGVirtualDisplay\n"); return 1; }
    CGVirtualDisplaySettings *s = [[CGVirtualDisplaySettings alloc] init];
    s.hiDPI = 1;
    s.modes = @[
      [[CGVirtualDisplayMode alloc] initWithWidth:w * 2 height:h * 2 refreshRate:60],
      [[CGVirtualDisplayMode alloc] initWithWidth:w height:h refreshRate:60],
    ];
    if (![display applySettings:s]) { fprintf(stderr, "applySettings failed\n"); return 1; }
    CGDirectDisplayID vid = display.displayID;
    // Let the window server see it.
    [[NSRunLoop mainRunLoop] runUntilDate:[NSDate dateWithTimeIntervalSinceNow:2.0]];

    // Its HiDPI mode of w × h points, if it offers one.
    NSDictionary *opts = @{(__bridge NSString *)kCGDisplayShowDuplicateLowResolutionModes: @YES};
    CFArrayRef modes = CGDisplayCopyAllDisplayModes(vid, (__bridge CFDictionaryRef)opts);
    CGDisplayModeRef best = NULL;
    for (CFIndex i = 0; modes && i < CFArrayGetCount(modes); i++) {
      CGDisplayModeRef m = (CGDisplayModeRef)CFArrayGetValueAtIndex(modes, i);
      fprintf(stderr, "virtual mode %zux%zu pt %zux%zu px\n", CGDisplayModeGetWidth(m), CGDisplayModeGetHeight(m),
              CGDisplayModeGetPixelWidth(m), CGDisplayModeGetPixelHeight(m));
      if (CGDisplayModeGetWidth(m) == w && CGDisplayModeGetPixelWidth(m) == w * 2) best = m;
    }
    CGDisplayConfigRef config;
    CGBeginDisplayConfiguration(&config);
    if (best) CGConfigureDisplayWithDisplayMode(config, vid, best, NULL);
    // The main display is the one at the origin: the virtual one there, the runner's own to its right.
    CGConfigureDisplayOrigin(config, vid, 0, 0);
    CGDirectDisplayID own = CGMainDisplayID();
    if (own != vid) CGConfigureDisplayOrigin(config, own, (int32_t)w, 0);
    CGError err = CGCompleteDisplayConfiguration(config, kCGConfigurePermanently);
    if (err) fprintf(stderr, "display configuration: error %d\n", err);
    [[NSRunLoop mainRunLoop] runUntilDate:[NSDate dateWithTimeIntervalSinceNow:2.0]];
    CGDisplayModeRef now = CGDisplayCopyDisplayMode(vid);
    printf("display %u %zux%zu pt, %zux%zu px, main %u\n", vid, CGDisplayModeGetWidth(now), CGDisplayModeGetHeight(now),
           CGDisplayModeGetPixelWidth(now), CGDisplayModeGetPixelHeight(now), CGMainDisplayID());
    fflush(stdout);
    [[NSRunLoop mainRunLoop] run];
  }
  return 0;
}
