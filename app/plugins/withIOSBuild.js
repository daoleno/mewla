/**
 * Expo config plugin: keep Expo iOS modules on one Swift ABI, pin packaged
 * marketing/build versions, and embed Ghostty MIT notice in the app bundle.
 *
 * Expo can resolve precompiled Swift modules independently from the local
 * ExpoModulesCore source selected by the lockfile. We observed that mix cause
 * a dyld crash before React Native started. Building all Expo iOS modules from
 * the locked sources keeps those binary interfaces on one ABI.
 */
const fs = require("fs");
const path = require("path");
const {
  createRunOncePlugin,
  withDangerousMod,
  withPodfileProperties,
  withXcodeProject,
  IOSConfig,
} = require("@expo/config-plugins");

const NOTICE_SRC_REL = "assets/notices/GHOSTTY-MIT.txt";
// Xcode Copy Bundle Resources flattens ordinary files; keep the notice at the
// app bundle root so the packaged path matches verify-ios-artifact.
const NOTICE_BUNDLE_REL = "GHOSTTY-MIT.txt";

function applyIOSPodfileProperties(properties) {
  return {
    ...properties,
    EXPO_USE_PRECOMPILED_MODULES: "false",
  };
}

function applyIOSVersionBuildSettings(
  project,
  { marketingVersion, buildNumber },
) {
  if (
    !/^(0|[1-9][0-9]{0,3})\.(0|[1-9][0-9]?)\.(0|[1-9][0-9]?)$/.test(
      marketingVersion,
    )
  ) {
    throw new Error(`invalid iOS marketing version: ${marketingVersion}`);
  }
  if (!/^[1-9][0-9]*$/.test(buildNumber)) {
    throw new Error(`invalid iOS build number: ${buildNumber}`);
  }

  const configurations = project.pbxXCBuildConfigurationSection();
  for (const [key, configuration] of Object.entries(configurations)) {
    if (
      key.endsWith("_comment") ||
      !configuration ||
      !configuration.buildSettings
    ) {
      continue;
    }
    configuration.buildSettings.MARKETING_VERSION = marketingVersion;
    configuration.buildSettings.CURRENT_PROJECT_VERSION = buildNumber;
  }
  return project;
}

function copyGhosttyNoticeIntoIOSProject(
  projectRoot,
  platformProjectRoot,
  projectName,
) {
  const src = path.join(projectRoot, NOTICE_SRC_REL);
  if (!fs.existsSync(src)) {
    throw new Error(`withIOSBuild: missing Ghostty MIT notice at ${src}`);
  }
  const destDir = path.join(platformProjectRoot, projectName);
  fs.mkdirSync(destDir, { recursive: true });
  const dest = path.join(destDir, NOTICE_BUNDLE_REL);
  fs.copyFileSync(src, dest);
  return {
    projectName,
    bundleRelativePath: NOTICE_BUNDLE_REL,
    projectRelativePath: path.join(projectName, NOTICE_BUNDLE_REL),
  };
}

function linkGhosttyNoticeResource(
  project,
  { projectName, projectRelativePath },
) {
  if (project.hasFile(projectRelativePath)) {
    return project;
  }
  return IOSConfig.XcodeUtils.addResourceFileToGroup({
    filepath: projectRelativePath,
    groupName: projectName,
    project,
    isBuildFile: true,
    verbose: false,
  });
}

function withGhosttyNotice(config) {
  config = withDangerousMod(config, [
    "ios",
    async (cfg) => {
      const projectName = IOSConfig.XcodeUtils.getProjectName(
        cfg.modRequest.projectRoot,
      );
      copyGhosttyNoticeIntoIOSProject(
        cfg.modRequest.projectRoot,
        cfg.modRequest.platformProjectRoot,
        projectName,
      );
      return cfg;
    },
  ]);
  return withXcodeProject(config, (cfg) => {
    const projectName = IOSConfig.XcodeUtils.getProjectName(
      cfg.modRequest.projectRoot,
    );
    cfg.modResults = linkGhosttyNoticeResource(cfg.modResults, {
      projectName,
      projectRelativePath: path.join(projectName, NOTICE_BUNDLE_REL),
    });
    return cfg;
  });
}

function withIOSBuild(config, options) {
  config = withPodfileProperties(config, (cfg) => {
    cfg.modResults = applyIOSPodfileProperties(cfg.modResults);
    return cfg;
  });
  config = withXcodeProject(config, (cfg) => {
    cfg.modResults = applyIOSVersionBuildSettings(cfg.modResults, options);
    return cfg;
  });
  return withGhosttyNotice(config);
}

const pkg = { name: "with-ios-build", version: "1.0.0" };

module.exports = createRunOncePlugin(withIOSBuild, pkg.name, pkg.version);
module.exports.applyIOSPodfileProperties = applyIOSPodfileProperties;
module.exports.applyIOSVersionBuildSettings =
  applyIOSVersionBuildSettings;
module.exports.copyGhosttyNoticeIntoIOSProject =
  copyGhosttyNoticeIntoIOSProject;
module.exports.linkGhosttyNoticeResource = linkGhosttyNoticeResource;
module.exports.NOTICE_SRC_REL = NOTICE_SRC_REL;
module.exports.NOTICE_BUNDLE_REL = NOTICE_BUNDLE_REL;
