const { describe, expect, it } = require("bun:test");
const createConfig = require("./app.config.js");
const { expo: baseConfig } = require("./app.base.json");
const { buildNumber: trackedIOSBuildNumber } = require("./ios-build.json");
const {
  resolveIOSBuildNumber,
  resolveIOSIdentity,
  resolveIOSMarketingVersion,
} = createConfig;

const IDENTITY_ENV_KEYS = [
  "MEWLA_ANDROID_DISTRIBUTION",
  "MEWLA_IOS_APP_VARIANT",
  "MEWLA_IOS_BUILD_NUMBER",
  "MEWLA_IOS_DISTRIBUTION",
  "MEWLA_IOS_LIVE",
];

// Shell or CI identity variables must not leak into these assertions.
function withIdentityEnv(values, run) {
  const previous = Object.fromEntries(
    IDENTITY_ENV_KEYS.map((key) => [key, process.env[key]]),
  );
  for (const key of IDENTITY_ENV_KEYS) delete process.env[key];
  Object.assign(process.env, values);
  try {
    return run();
  } finally {
    for (const key of IDENTITY_ENV_KEYS) {
      if (previous[key] === undefined) delete process.env[key];
      else process.env[key] = previous[key];
    }
  }
}

describe("native platform config", () => {
  const config = withIdentityEnv({}, () => createConfig());

  it("defines a stable iOS application identity", () => {
    expect(Number.isInteger(trackedIOSBuildNumber)).toBe(true);
    expect(trackedIOSBuildNumber).toBeGreaterThan(0);
    const expectedBuildNumber = String(trackedIOSBuildNumber);
    expect(config.name).toBe("Mewla");
    expect(config.slug).toBe("mewla");
    expect(config.ios.bundleIdentifier).toBe("com.daoleno.mewla");
    expect(config.android.package).toBe("com.daoleno.mewla");
    expect(config.ios.infoPlist.CFBundleDisplayName).toBe("Mewla");
    expect(config.ios.infoPlist.CFBundleShortVersionString).toBe(
      baseConfig.version.split("-", 1)[0],
    );
    expect(config.ios.infoPlist.CFBundleVersion).toBe(expectedBuildNumber);
    expect(config.ios.buildNumber).toBe(expectedBuildNumber);
  });

  it("ships Android as a GitHub APK that can install its own update", () => {
    expect(config.extra.distribution).toEqual({ android: "github-apk", ios: "none" });
    expect(config.plugins).toContainEqual([
      "./plugins/withAndroidAppUpdate",
      { channel: "github-apk" },
    ]);
  });

  it("takes a store distribution channel from the build environment", () => {
    const store = withIdentityEnv(
      { MEWLA_ANDROID_DISTRIBUTION: "play-store", MEWLA_IOS_DISTRIBUTION: "app-store" },
      () => createConfig(),
    );
    expect(store.extra.distribution).toEqual({ android: "play-store", ios: "app-store" });
    expect(() =>
      withIdentityEnv({ MEWLA_ANDROID_DISTRIBUTION: "app-store" }, () => createConfig()),
    ).toThrow("MEWLA_ANDROID_DISTRIBUTION");
    expect(() =>
      withIdentityEnv({ MEWLA_IOS_DISTRIBUTION: "github-apk" }, () => createConfig()),
    ).toThrow("MEWLA_IOS_DISTRIBUTION");
  });

  it("routes remote push through Mewla's EAS project and FCM app without per-user setup", () => {
    expect(config.extra.eas.projectId).toBe("1b76d386-969b-4841-ae68-f32895ad2301");
    expect(config.android.googleServicesFile).toBe("./google-services.json");
    const { project_info, client } = require("./google-services.json");
    expect(project_info.project_id).toBe("mewla-50425");
    expect(
      client.map((entry) => entry.client_info.android_client_info.package_name),
    ).toEqual(expect.arrayContaining([config.android.package, `${config.android.package}.debug`]));
    const notifications = config.plugins.find(
      (plugin) => Array.isArray(plugin) && plugin[0] === "expo-notifications",
    );
    expect(notifications?.[1]?.defaultChannel).toBe("mewla-agents");
  });

  it("lets a fork point push at its own EAS project", () => {
    process.env.MEWLA_EXPO_PROJECT_ID = "fork-project";
    try {
      expect(createConfig().extra.eas.projectId).toBe("fork-project");
    } finally {
      delete process.env.MEWLA_EXPO_PROJECT_ID;
    }
  });

  it("registers only the mewla scheme", () => {
    expect(config.scheme).toBe("mewla");
  });

  it("selects the Preview bundle identity while keeping the installed name Mewla", () => {
    expect(resolveIOSIdentity("production")).toMatchObject({
      displayName: "Mewla",
      bundleIdentifier: "com.daoleno.mewla",
      nativeProjectName: "Mewla",
      artifactName: "mewla-ios",
    });
    expect(resolveIOSIdentity("preview")).toEqual({
      variant: "preview",
      displayName: "Mewla",
      bundleIdentifier: "com.daoleno.mewla.preview",
      nativeProjectName: "Mewla",
      artifactName: "mewla-preview-ios",
      notificationMode: "production",
      timeSensitive: true,
      liveExtensionBundleIdentifier: "com.daoleno.mewla.preview.live",
      appGroup: "group.com.daoleno.mewla.preview",
    });
  });

  it("builds the Live Activity extension only when asked, with its own identity per variant", () => {
    const off = withIdentityEnv({ MEWLA_IOS_APP_VARIANT: "preview" }, () => createConfig());
    // Time Sensitive calendar alerts are the only entitlement without Live.
    expect(off.ios.entitlements).toEqual({ "com.apple.developer.usernotifications.time-sensitive": true });
    expect(off.ios.infoPlist.NSSupportsLiveActivities).toBeUndefined();
    const production = withIdentityEnv({ MEWLA_IOS_APP_VARIANT: "production" }, () => createConfig());
    expect(production.ios.entitlements).toBeUndefined();
    expect(JSON.stringify(off.plugins)).not.toContain("apple-targets");

    for (const [variant, group] of [
      ["preview", "group.com.daoleno.mewla.preview"],
      ["production", "group.com.daoleno.mewla"],
    ]) {
      const on = withIdentityEnv(
        { MEWLA_IOS_APP_VARIANT: variant, MEWLA_IOS_LIVE: "1" },
        () => createConfig(),
      );
      expect(on.ios.entitlements["com.apple.security.application-groups"]).toEqual([group]);
      expect(on.ios.infoPlist.NSSupportsLiveActivities).toBe(true);
      expect(on.plugins.some((plugin) => Array.isArray(plugin) && plugin[0] === "@bacons/apple-targets")).toBe(true);
      const target = withIdentityEnv({ MEWLA_IOS_APP_VARIANT: variant }, () =>
        require("./targets/mewla-live/expo-target.config.js")(on),
      );
      expect(target.bundleIdentifier).toBe(`${on.ios.bundleIdentifier}.live`);
      expect(target.entitlements["com.apple.security.application-groups"]).toEqual([group]);
    }
  });

  it("applies the complete Preview identity to Expo without changing Android package identity", () => {
    const preview = withIdentityEnv({ MEWLA_IOS_APP_VARIANT: "preview" }, () =>
      createConfig(),
    );
    expect(preview.name).toBe("Mewla");
    expect(preview.ios.bundleIdentifier).toBe("com.daoleno.mewla.preview");
    expect(preview.ios.infoPlist.CFBundleDisplayName).toBe("Mewla");
    expect(preview.android.package).toBe("com.daoleno.mewla");
    expect(preview.android).toEqual(config.android);
  });

  it("rejects a non-integer MEWLA_IOS_BUILD_NUMBER", () => {
    expect(() =>
      withIdentityEnv({ MEWLA_IOS_BUILD_NUMBER: "beta" }, () => createConfig()),
    ).toThrow("MEWLA_IOS_BUILD_NUMBER must be a positive integer");
  });

  it("keeps production as the default and rejects free-form identities", () => {
    expect(resolveIOSIdentity()).toEqual(resolveIOSIdentity("production"));
    expect(() => resolveIOSIdentity("Preview")).toThrow("production, preview");
    expect(() => resolveIOSIdentity("com.example.other")).toThrow(
      "production, preview",
    );
  });

  it("explains local-network access used by self-hosted daemons on iOS", () => {
    expect(config.ios.infoPlist.ITSAppUsesNonExemptEncryption).toBe(false);
    expect(config.ios.infoPlist.NSLocalNetworkUsageDescription).toContain(
      "self-hosted daemon",
    );
    expect(config.ios.infoPlist.NSAppTransportSecurity).toEqual({
      NSAllowsLocalNetworking: true,
      NSAllowsArbitraryLoads: false,
    });
  });

  it("owns production APS entitlement from release identity without disabling local notifications", () => {
    const notifications = config.plugins.find(
      (plugin) => Array.isArray(plugin) && plugin[0] === "expo-notifications",
    );
    expect(notifications?.[1]?.mode).toBe("production");
    expect(resolveIOSIdentity("production").notificationMode).toBe(
      "production",
    );
    expect(resolveIOSIdentity("preview").notificationMode).toBe("production");
  });

  it("keeps QR scanning audio-disabled and omits the retired media surface", () => {
    const camera = config.plugins.find(
      (plugin) => Array.isArray(plugin) && plugin[0] === "expo-camera",
    );
    const audio = config.plugins.find(
      (plugin) => Array.isArray(plugin) && plugin[0] === "expo-audio",
    );

    expect(camera?.[1]?.microphonePermission).toBe(false);
    expect(camera?.[1]?.recordAudioAndroid).toBe(false);
    expect(camera?.[1]?.barcodeScannerEnabled).toBe(true);
    expect(audio).toBeUndefined();
    expect(config.plugins).not.toContain("expo-video");
  });

  it("includes the tracked Android plugin that enables private-network HTTP", () => {
    expect(config.plugins).toContain("./plugins/withAndroidRelease");
  });

  it("accepts an explicit monotonically increasing CI build number", () => {
    expect(resolveIOSBuildNumber("42", "2")).toBe("42");
    expect(resolveIOSBuildNumber("", "2")).toBe("2");
    expect(() => resolveIOSBuildNumber("0", "2")).toThrow();
    expect(() => resolveIOSBuildNumber("beta", "2")).toThrow();
  });

  it("derives an App Store-valid iOS marketing version and rejects invalid values", () => {
    expect(resolveIOSMarketingVersion("0.1.0-beta.2")).toBe("0.1.0");
    expect(resolveIOSMarketingVersion("12.34.56")).toBe("12.34.56");
    expect(() => resolveIOSMarketingVersion("0.1-beta")).toThrow();
    expect(() => resolveIOSMarketingVersion("01.2.3")).toThrow();
    expect(() => resolveIOSMarketingVersion("1.234.5")).toThrow();
  });
});
