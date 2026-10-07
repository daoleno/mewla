const IOS_IDENTITIES = Object.freeze({
  production: Object.freeze({
    variant: "production",
    displayName: "Mewla",
    bundleIdentifier: "com.daoleno.mewla",
    nativeProjectName: "Mewla",
    artifactName: "mewla-ios",
    // Release identities ship to TestFlight/App Store Connect; remote push needs
    // production APS. Local notifications do not depend on this entitlement.
    notificationMode: "production",
  }),
  preview: Object.freeze({
    variant: "preview",
    displayName: "Mewla",
    bundleIdentifier: "com.daoleno.mewla.preview",
    nativeProjectName: "Mewla",
    artifactName: "mewla-preview-ios",
    notificationMode: "production",
  }),
});

function resolveIOSIdentity(value) {
  const variant =
    typeof value === "string" && value.trim() ? value.trim() : "production";
  const identity = IOS_IDENTITIES[variant];
  if (!identity) {
    throw new Error(
      `MEWLA_IOS_APP_VARIANT must be one of: ${Object.keys(IOS_IDENTITIES).join(", ")}`,
    );
  }
  return identity;
}

function resolveIOSNotificationMode(identity) {
  const mode = identity && identity.notificationMode;
  if (mode !== "production" && mode !== "development") {
    throw new Error("iOS notification mode must be production or development");
  }
  return mode;
}

module.exports = {
  IOS_IDENTITIES,
  resolveIOSIdentity,
  resolveIOSNotificationMode,
};
