const path = require("path");
const { getDefaultConfig } = require("expo/metro-config");

const config = getDefaultConfig(__dirname);

// The app is a workspace of the repo, so Expo watches the whole repo root. Most
// of it is not the app: release downloads, daemon builds, the site, docs and
// native build output run to several GB and made Metro crawl and hold them all.
const repoRoot = path.resolve(__dirname, "..");
const escape = (value) => value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
const underRepo = (...segments) =>
  new RegExp(`^${escape(path.join(repoRoot, ...segments))}(/|$)`);

config.resolver.blockList = [
  ...(Array.isArray(config.resolver.blockList)
    ? config.resolver.blockList
    : config.resolver.blockList
      ? [config.resolver.blockList]
      : []),
  underRepo("dist-download"),
  underRepo("release"),
  underRepo("build"),
  underRepo("daemon"),
  underRepo("site"),
  underRepo("docs"),
  underRepo("scripts"),
  underRepo("bin"),
  underRepo("config"),
  underRepo(".git"),
  underRepo("app", "dist"),
  underRepo("app", "android", "app", "build"),
  underRepo("app", "android", "build"),
  underRepo("app", "android", ".gradle"),
  underRepo("app", "ios", "build"),
  underRepo("app", "ios", "Pods"),
  // Gradle output of every native module (app/modules/*, node_modules/*): a
  // native build rewrites thousands of files there and Metro grew past 8 GB.
  /\/android\/(build|\.cxx|\.gradle)\//,
];

module.exports = config;
