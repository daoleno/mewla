#!/usr/bin/env node
// Give each iOS target its own manual-signing profile in the generated Xcode project.
//
//   node scripts/set-ios-target-signing.js app/ios/Mewla.xcodeproj/project.pbxproj \
//     "Mewla=<app profile name>" "MewlaLiveWidgets=<extension profile name>"
//
// xcodebuild's PROVISIONING_PROFILE_SPECIFIER on the command line applies to every target, so
// an app with an extension would sign the extension with the app's profile and the archive would
// fail. With one profile per target the command line carries only the shared settings.
const fs = require("fs");
const { XcodeProject } = require("@bacons/xcode");
const xcodeParse = require("@bacons/xcode/json");

function applyTargetSigning(project, assignments) {
  const targets = project.rootObject.props.targets;
  for (const [targetName, profileName] of Object.entries(assignments)) {
    const target = targets.find((item) => item.props.name === targetName);
    if (!target) {
      throw new Error(`no target named ${targetName} in the Xcode project`);
    }
    if (!profileName) {
      throw new Error(`no provisioning profile name for ${targetName}`);
    }
    target.setBuildSetting("CODE_SIGN_STYLE", "Manual");
    target.setBuildSetting("CODE_SIGN_IDENTITY", "Apple Distribution");
    target.setBuildSetting("PROVISIONING_PROFILE_SPECIFIER", profileName);
  }
  return project;
}

function parseAssignments(args) {
  const assignments = {};
  for (const arg of args) {
    const split = arg.indexOf("=");
    if (split <= 0) throw new Error(`expected Target=Profile Name, got ${arg}`);
    assignments[arg.slice(0, split)] = arg.slice(split + 1);
  }
  return assignments;
}

if (require.main === module) {
  const [pbxprojPath, ...pairs] = process.argv.slice(2);
  if (!pbxprojPath || pairs.length === 0) {
    console.error("usage: set-ios-target-signing.js project.pbxproj Target=Profile ...");
    process.exit(2);
  }
  const project = XcodeProject.open(pbxprojPath);
  applyTargetSigning(project, parseAssignments(pairs));
  fs.writeFileSync(pbxprojPath, xcodeParse.build(project.toJSON()));
  console.log(`signing set for ${pairs.length} targets`);
}

module.exports = { applyTargetSigning, parseAssignments };
