const { resolveIOSIdentity } = require("../../iosIdentity");

/**
 * The Dynamic Island, Lock Screen Live Activity and home-screen widgets: one WidgetKit
 * extension. Its bundle id and App Group come from the closed iOS identity, so Preview and
 * Production each get their own pair (register them in the Apple Developer portal: docs/ios-ci-release.md).
 */
/** @type {import('@bacons/apple-targets/app.plugin').ConfigFunction} */
module.exports = (config) => {
  const identity = resolveIOSIdentity(process.env.MEWLA_IOS_APP_VARIANT);
  return {
    type: "widget",
    name: "MewlaLive",
    displayName: "Mewla",
    bundleIdentifier: identity.liveExtensionBundleIdentifier,
    // Widgets and Live Activities need containerBackground and Text(timerInterval:) (iOS 17).
    deploymentTarget: "17.0",
    images: { seal: "./seal.png" },
    entitlements: {
      "com.apple.security.application-groups": [identity.appGroup],
    },
  };
};
