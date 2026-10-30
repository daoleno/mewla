const fs = require("fs");
const path = require("path");
const { expo: baseConfig } = require("./app.base.json");
const { buildNumber: trackedIOSBuildNumber } = require("./ios-build.json");
const {
  resolveIOSIdentity,
  resolveIOSNotificationMode,
} = require("./iosIdentity");

if (process.env.EXPO_NO_DOTENV !== "1") {
  loadEnvFile(path.join(__dirname, ".env.local"));
}

function readEnv(name) {
  const value = process.env[`MEWLA_${name}`];
  return typeof value === "string" ? value.trim() : "";
}

// Mewla's own EAS project: remote push is routed through it for every official
// build, so nobody needs a project of their own. It is public (it ships inside
// the app); a fork that sends its own pushes sets MEWLA_EXPO_PROJECT_ID.
const officialExpoProjectId = "1b76d386-969b-4841-ae68-f32895ad2301";

module.exports = () => {
  const extra = { ...(baseConfig.extra || {}) };
  const iosIdentity = resolveIOSIdentity(readEnv("IOS_APP_VARIANT"));
  const iosNotificationMode = resolveIOSNotificationMode(iosIdentity);
  const iosMarketingVersion = resolveIOSMarketingVersion(baseConfig.version);
  const iosBuildNumber = resolveIOSBuildNumber(
    readEnv("IOS_BUILD_NUMBER"),
    trackedIOSBuildNumber,
  );

  // The Live Activity / widget extension is its own signed target, so it ships only once its App
  // IDs, App Group and provisioning profile exist (docs/ios-ci-release.md). Until then iOS builds
  // are exactly what they were; CI's unsigned Simulator build turns it on to keep it compiling.
  const iosLive = readEnv("IOS_LIVE") === "1";

  extra.eas = { projectId: readEnv("EXPO_PROJECT_ID") || officialExpoProjectId };
  extra.distribution = resolveDistribution(
    readEnv("ANDROID_DISTRIBUTION"),
    readEnv("IOS_DISTRIBUTION"),
  );

  return {
    ...baseConfig,
    ios: {
      ...(baseConfig.ios || {}),
      bundleIdentifier: iosIdentity.bundleIdentifier,
      buildNumber: iosBuildNumber,
      ...(iosLive || iosIdentity.timeSensitive
        ? {
            entitlements: {
              ...(iosLive
                ? { "com.apple.security.application-groups": [iosIdentity.appGroup] }
                : {}),
              // Calendar alerts that break through Focus (the Preview identity).
              ...(iosIdentity.timeSensitive
                ? { "com.apple.developer.usernotifications.time-sensitive": true }
                : {}),
            },
          }
        : {}),
      infoPlist: {
        ...((baseConfig.ios && baseConfig.ios.infoPlist) || {}),
        // remote-notification: a live_state push wakes the app to update the Live Activity.
        ...(iosLive
          ? { NSSupportsLiveActivities: true, UIBackgroundModes: ["remote-notification"] }
          : {}),
        CFBundleDisplayName: iosIdentity.displayName,
        CFBundleShortVersionString: iosMarketingVersion,
        CFBundleVersion: iosBuildNumber,
      },
    },
    plugins: [
      ...(baseConfig.plugins || []),
      "expo-font",
      "expo-status-bar",
      // Own APS environment from the release identity so TestFlight/App Store
      // remote push is production-entitled; local notifications are unaffected.
      // Android draws the status-bar glyph from the icon's alpha, tinted by
      // color: the white seal, in seal vermilion.
      [
        "expo-notifications",
        {
          mode: iosNotificationMode,
          icon: "./assets/branding/mewla-notification.png",
          color: "#C8372B",
          // A push carries no channel of its own; Android files it here (the app
          // creates the channel at launch, see registerForPushNotificationsAsync).
          defaultChannel: "mewla-agents",
        },
      ],
      // Keep Expo Swift modules on one source-built ABI so patch upgrades
      // cannot mix precompiled modules and ExpoModulesCore binaries.
      // Also packages Ghostty MIT notice into the iOS app bundle.
      [
        "./plugins/withIOSBuild",
        {
          marketingVersion: iosMarketingVersion,
          buildNumber: iosBuildNumber,
        },
      ],
      // Dynamic Island, Lock Screen Live Activity and home-screen widgets: the WidgetKit
      // extension in targets/mewla-live, with its own bundle id and the shared App Group.
      ...(iosLive
        ? [["@bacons/apple-targets", { appleTeamId: readEnv("APPLE_TEAM_ID") || undefined }]]
        : []),
      // Package Ghostty MIT notice into Android assets + env-based release signing.
      "./plugins/withAndroidRelease",
      // Resolve duplicate BouncyCastle metadata in the Android resource merge.
      "./plugins/withAndroidPackaging",
      // Live Updates and widgets: FCM live_state data messages reach Mewla's own service.
      "./plugins/withAndroidMewlaLive",
      // GitHub APK builds may install their own signed, verified update.
      [
        "./plugins/withAndroidAppUpdate",
        { channel: extra.distribution.android },
      ],
      // Phone use (Labs): the GitHub APK only; a store build drops its service entries.
      [
        "./plugins/withAndroidPhoneUse",
        { channel: extra.distribution.android },
      ],
    ],
    extra,
  };
};

// Build-time distribution channel per platform; see services/appUpdate.ts.
function resolveDistribution(android, ios) {
  const resolved = {
    android: android || "github-apk",
    ios: ios || "none",
  };
  if (!["github-apk", "play-store", "none"].includes(resolved.android)) {
    throw new Error(
      "MEWLA_ANDROID_DISTRIBUTION must be github-apk, play-store or none",
    );
  }
  if (!["app-store", "none"].includes(resolved.ios)) {
    throw new Error("MEWLA_IOS_DISTRIBUTION must be app-store or none");
  }
  return resolved;
}

function resolveIOSBuildNumber(value, fallback) {
  const candidate =
    typeof value === "string" && value.trim() ? value.trim() : String(fallback);
  if (!/^[1-9][0-9]*$/.test(candidate)) {
    throw new Error("MEWLA_IOS_BUILD_NUMBER must be a positive integer");
  }
  return candidate;
}

function resolveIOSMarketingVersion(value) {
  const candidate = typeof value === "string" ? value.split("-", 1)[0] : "";
  if (
    !/^(0|[1-9][0-9]{0,3})\.(0|[1-9][0-9]?)\.(0|[1-9][0-9]?)$/.test(candidate)
  ) {
    throw new Error(
      "iOS marketing version must be three numeric components (for example 0.1.0)",
    );
  }
  return candidate;
}

module.exports.resolveDistribution = resolveDistribution;
module.exports.resolveIOSBuildNumber = resolveIOSBuildNumber;
module.exports.resolveIOSMarketingVersion = resolveIOSMarketingVersion;
module.exports.resolveIOSIdentity = resolveIOSIdentity;
module.exports.resolveIOSNotificationMode = resolveIOSNotificationMode;

function loadEnvFile(filePath) {
  if (!fs.existsSync(filePath)) {
    return;
  }

  const content = fs.readFileSync(filePath, "utf8");
  for (const rawLine of content.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line || line.startsWith("#")) {
      continue;
    }

    const separatorIndex = line.indexOf("=");
    if (separatorIndex <= 0) {
      continue;
    }

    const key = line.slice(0, separatorIndex).trim();
    if (!key || Object.prototype.hasOwnProperty.call(process.env, key)) {
      continue;
    }

    let value = line.slice(separatorIndex + 1).trim();
    if (
      (value.startsWith('"') && value.endsWith('"')) ||
      (value.startsWith("'") && value.endsWith("'"))
    ) {
      value = value.slice(1, -1);
    }

    process.env[key] = value;
  }
}
