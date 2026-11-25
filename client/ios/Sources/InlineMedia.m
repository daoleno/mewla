// Screen's <video> must play in the page, muted, as soon as it has frames: in the system's fullscreen player
// it takes the taps meant for the computer and leaves the inline picture stalled when it is closed. NSH 0.7
// makes its web views with allowsInlineMediaPlayback YES (longbridge/nsh#2970), but keeps WebKit's iPhone
// default that every media type waits for a tap (mediaTypesRequiringUserActionForPlayback), and has no prop for
// it: so every web view this app makes still starts video without sound without a tap (Screen's video is muted).
// Sound still needs one: a page from the computer (a file preview's HTML) cannot start playing audio by itself.
// Upstream: NSH-ISSUE-webview-autoplay.md in the upgrade's worklog.
// Objective-C rather than Swift: the swapped method is an initializer, and ARC's init rules
// (self consumed, a retained result) hold for a category method named in the init family.
#import <objc/runtime.h>
#import <WebKit/WebKit.h>

@implementation WKWebView (MewlaInlineMedia)

+ (void)load {
  static dispatch_once_t once;
  dispatch_once(&once, ^{
    Method original = class_getInstanceMethod(self, @selector(initWithFrame:configuration:));
    Method inline_ = class_getInstanceMethod(self, @selector(initMewlaWithFrame:configuration:));
    if (original && inline_) method_exchangeImplementations(original, inline_);
  });
}

- (instancetype)initMewlaWithFrame:(CGRect)frame configuration:(WKWebViewConfiguration *)configuration {
  // A web view copies its configuration when it is made: a later change has no effect.
  configuration.allowsInlineMediaPlayback = YES;
  configuration.mediaTypesRequiringUserActionForPlayback = WKAudiovisualMediaTypeAudio;
  // The implementations are exchanged: this calls WebKit's own initializer.
  return [self initMewlaWithFrame:frame configuration:configuration];
}

@end
