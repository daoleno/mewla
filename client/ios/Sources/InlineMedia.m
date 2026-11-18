// NSH 0.6.0 makes its web views with WebKit's iPhone default, allowsInlineMediaPlayback NO: Screen's
// <video> then opens the system's fullscreen player, which takes the taps meant for the computer and
// leaves the inline picture stalled when it is closed. NSH has no prop for it (iOS web views take only
// allowsBackForwardNavigationGestures), so until it does, every web view this app makes plays media
// inline and without a tap. Screen's video is muted and nothing else in the app plays any.
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
  configuration.mediaTypesRequiringUserActionForPlayback = WKAudiovisualMediaTypeNone;
  // The implementations are exchanged: this calls WebKit's own initializer.
  return [self initMewlaWithFrame:frame configuration:configuration];
}

@end
