#!/usr/bin/env node
// Generate the app's open-source licenses (Settings → Open-source licenses).
//
// JavaScript packages are the ones that actually ship: every node_modules
// package named in the source maps of the Android and iOS Metro bundles.
// Packages autolinked into the native build are listed too. Bundled code,
// fonts and native libraries come from components.json, with
// their license texts in texts/ or app/assets/notices/. The output is
// app/assets/notices/licenses.json.
//
// Usage (repository root, after bun install):
//   bun run licenses:generate                 # export both bundles, rewrite the file
//   bun run licenses:generate --check         # fail if the file is stale
//   bun run licenses:generate --bundle-dir D  # reuse D/<platform>/_expo/... exports
//   bun run licenses:generate --pods app/ios/Podfile.lock
//       fail if a CocoaPod is neither built from a listed package nor in components.json
//   bun run licenses:generate --maven deps.txt
//       the same for `./gradlew :app:dependencies --configuration releaseRuntimeClasspath`

import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, mkdtempSync, readdirSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, relative, sep } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const here = join(root, "scripts", "app-licenses");
const outputPath = join(root, "app", "assets", "notices", "licenses.json");
const outputName = relative(root, outputPath);
const components = JSON.parse(readFileSync(join(here, "components.json"), "utf8"));
const platforms = ["android", "ios"];

const args = process.argv.slice(2);
const option = (name) => {
  const index = args.indexOf(name);
  return index < 0 ? null : args[index + 1];
};

function fail(message) {
  console.error(message);
  process.exit(1);
}

function normalizeText(text) {
  return text.replace(/\r\n?/g, "\n").replace(/[ \t]+$/gm, "").trim() + "\n";
}

function readText(path) {
  const base = path.startsWith("app/") ? root : here;
  return normalizeText(readFileSync(join(base, path), "utf8"));
}

// ---- JavaScript packages in the bundles ----

function exportBundles(outDir) {
  for (const platform of platforms) {
    console.error(`exporting ${platform} bundle…`);
    execFileSync(
      "bunx",
      ["expo", "export", "--platform", platform, "--source-maps", "--output-dir", join(outDir, platform)],
      { cwd: join(root, "app"), stdio: ["ignore", "ignore", "inherit"] },
    );
  }
}

function sourceMaps(dir) {
  const found = [];
  const walk = (current) => {
    for (const entry of readdirSync(current, { withFileTypes: true })) {
      const path = join(current, entry.name);
      if (entry.isDirectory()) walk(path);
      else if (entry.name.endsWith(".map")) found.push(path);
    }
  };
  walk(dir);
  return found;
}

// packageDir returns the package directory of a bundled source path: the
// deepest node_modules/<name> (or node_modules/@scope/<name>) segment.
function packageDir(source) {
  const marker = `${sep}node_modules${sep}`;
  const normalized = source.split("/").join(sep);
  const index = normalized.lastIndexOf(marker);
  if (index < 0) return null;
  const rest = normalized.slice(index + marker.length).split(sep);
  const name = rest[0].startsWith("@") ? `${rest[0]}${sep}${rest[1]}` : rest[0];
  return normalized.slice(0, index + marker.length) + name;
}

function bundledPackages(bundleDir) {
  const byDir = new Map();
  for (const platform of platforms) {
    const maps = sourceMaps(join(bundleDir, platform));
    if (maps.length === 0) fail(`no source maps under ${join(bundleDir, platform)}`);
    for (const map of maps) {
      const { sources } = JSON.parse(readFileSync(map, "utf8"));
      for (const source of sources) {
        const dir = packageDir(source);
        if (!dir) continue;
        // Metro names sources relative to the workspace root ("/node_modules/…").
        const absolute = [dir, join(root, dir), join(root, "app", dir)].find((candidate) =>
          existsSync(join(candidate, "package.json")),
        );
        if (!absolute) fail(`cannot find the package of bundled source ${source}`);
        const real = realpathSync(absolute);
        if (!byDir.has(real)) byDir.set(real, new Set());
        byDir.get(real).add(platform);
      }
    }
  }
  return byDir;
}

// autolinkedPackages adds the packages whose native code the release build
// links even when none of their JavaScript is in the bundle (expo-splash-screen,
// for example). First-party modules under app/modules are skipped.
function autolinkedPackages(byDir) {
  const run = (command) =>
    JSON.parse(
      execFileSync("bunx", ["expo-modules-autolinking", ...command, "--json"], {
        cwd: join(root, "app"),
        encoding: "utf8",
        stdio: ["ignore", "pipe", "inherit"],
        maxBuffer: 64 << 20,
      }),
    );
  for (const platform of platforms) {
    const names = [
      ...run(["resolve", "--platform", platform]).modules.filter((module) => !module.debugOnly).map((module) => module.packageName),
      ...Object.keys(run(["react-native-config", "--platform", platform]).dependencies ?? {}),
    ];
    for (const name of names) {
      const dir = resolvePackage(name, join(root, "app"));
      if (!dir) fail(`autolinked package ${name} is not installed`);
      const real = realpathSync(dir);
      if (real.startsWith(join(root, "app", "modules") + sep)) continue;
      if (!byDir.has(real)) byDir.set(real, new Set());
      byDir.get(real).add(platform);
    }
  }
  return byDir;
}

function resolvePackage(name, fromDir) {
  for (let dir = fromDir; ; dir = dirname(dir)) {
    const candidate = join(dir, "node_modules", name);
    if (existsSync(join(candidate, "package.json"))) return candidate;
    if (dir === dirname(dir)) return null;
  }
}

// dependencyClosure lists the installed production dependencies of a package
// (for code another bundle, such as mermaid.min.js, already contains).
function dependencyClosure(name) {
  const start = resolvePackage(name, join(root, "app"));
  if (!start) fail(`${name} is not installed; run bun install`);
  const seen = new Set();
  const queue = [start];
  while (queue.length > 0) {
    const dir = queue.shift();
    const manifest = JSON.parse(readFileSync(join(dir, "package.json"), "utf8"));
    for (const dependency of Object.keys(manifest.dependencies ?? {})) {
      if (dependency.startsWith("@types/")) continue;
      const resolved = resolvePackage(dependency, dir);
      if (!resolved || seen.has(resolved)) continue;
      seen.add(resolved);
      queue.push(resolved);
    }
  }
  return seen;
}

function licenseName(manifest) {
  const value = manifest.license ?? manifest.licenses;
  if (typeof value === "string") return value;
  if (Array.isArray(value)) return value.map((entry) => entry.type ?? entry).join(" OR ");
  if (value && typeof value === "object") return value.type ?? "UNKNOWN";
  return "UNKNOWN";
}

function repositoryURL(manifest) {
  let url = typeof manifest.repository === "string" ? manifest.repository : manifest.repository?.url;
  if (!url) return manifest.homepage || `https://www.npmjs.com/package/${manifest.name}`;
  url = url.replace(/^git\+/, "").replace(/\.git$/, "").replace(/^(git|ssh):\/\/(git@)?/, "https://");
  url = url.replace(/^git@github\.com:/, "https://github.com/");
  if (/^github:/.test(url)) url = url.replace(/^github:/, "https://github.com/");
  if (/^[\w.-]+\/[\w.-]+$/.test(url)) url = `https://github.com/${url}`;
  return url;
}

function licenseFile(dir) {
  const names = readdirSync(dir)
    .filter((name) => /^(licen[cs]e|copying)([.-].*)?$/i.test(name))
    .sort((a, b) => a.length - b.length || a.localeCompare(b));
  return names.length > 0 ? join(dir, names[0]) : null;
}

// repositoryKey is "host/owner/repo" of a repository URL, for packages
// published from one monorepo.
function repositoryKey(url) {
  const match = /^https?:\/\/(?:www\.)?([^/]+\/[^/]+\/[^/#?]+)/.exec(url);
  return match ? match[1].toLowerCase() : null;
}

function detectLicense(text) {
  if (/^\s*(the )?mit license/i.test(text) || /permission is hereby granted, free of charge/i.test(text)) return "MIT";
  if (/^\s*isc license/i.test(text) || /permission to use, copy, modify, and\/or distribute/i.test(text)) return "ISC";
  if (/apache license,?\s+version 2\.0/i.test(text)) return "Apache-2.0";
  return "UNKNOWN";
}

function npmItem(dir, extra = {}) {
  const manifest = JSON.parse(readFileSync(join(dir, "package.json"), "utf8"));
  const file = licenseFile(dir);
  const text = file ? normalizeText(readFileSync(file, "utf8")) : null;
  let license = licenseName(manifest);
  if (license === "UNKNOWN" && text) license = detectLicense(text);
  return { name: manifest.name, version: manifest.version, license, url: repositoryURL(manifest), text, ...extra };
}

// fillMissingTexts gives a package without a license file the license file of
// its repository: from a listed package of the same repository, or from
// components.json repositoryTexts.
function fillMissingTexts(items) {
  const byRepository = new Map();
  for (const entry of items) {
    const key = repositoryKey(entry.url);
    if (key && entry.text && !byRepository.has(key)) byRepository.set(key, entry);
  }
  for (const entry of items) {
    if (entry.text) continue;
    const key = repositoryKey(entry.url);
    const sibling = key && byRepository.get(key);
    if (sibling) {
      entry.text = sibling.text;
      entry.note = `License of the ${key} repository, as in ${sibling.name}.`;
    } else if (key && components.repositoryTexts[key]) {
      entry.text = readText(components.repositoryTexts[key]);
      entry.note = `License of the ${key} repository.`;
    } else {
      fail(`${entry.name} ships no license file; add its repository (${key ?? entry.url}) to repositoryTexts in scripts/app-licenses/components.json`);
    }
  }
  return items;
}

// ---- Output ----

function build(bundleDir) {
  const texts = new Map();
  const textId = (text) => {
    if (text == null) return undefined;
    const id = createHash("sha256").update(text).digest("hex").slice(0, 12);
    texts.set(id, text);
    return id;
  };
  const item = ({ text, ...rest }) => {
    const out = { ...rest, text: typeof text === "string" ? textId(text) : undefined };
    for (const key of Object.keys(out)) if (out[key] === undefined) delete out[key];
    return out;
  };
  const dedupe = (items) => [...new Map(items.map((entry) => [`${entry.name}@${entry.version ?? ""}`, entry])).values()];
  const byName = (a, b) => a.name.localeCompare(b.name) || (a.version ?? "").localeCompare(b.version ?? "");

  const bundled = [];
  for (const entry of components.bundled) {
    const bundledItem = item({ ...pick(entry), text: readText(entry.text) });
    if (entry.npmDependenciesOf) {
      const included = [...dependencyClosure(entry.npmDependenciesOf)].map((dir) => npmItem(dir));
      bundledItem.includes = dedupe(fillMissingTexts(included).map(item)).sort(byName);
    }
    bundled.push(bundledItem);
  }

  const found = [];
  for (const [dir, seenOn] of autolinkedPackages(bundledPackages(bundleDir))) {
    const onlyOn = seenOn.size < platforms.length ? { platforms: [...seenOn].sort() } : {};
    found.push(npmItem(dir, onlyOn));
  }
  const packages = fillMissingTexts(found).map(item);

  const sections = [
    { title: "Bundled code", items: dedupe(bundled).sort(byName) },
    { title: "Fonts", items: components.fonts.map((entry) => item({ ...pick(entry), text: readText(entry.text) })).sort(byName) },
    {
      title: "Native libraries",
      items: components.native
        .map((entry) => item({ ...pick(entry), platforms: nativePlatforms(entry), text: entry.text ? readText(entry.text) : undefined }))
        .sort(byName),
    },
    { title: "JavaScript packages", items: dedupe(packages).sort(byName) },
  ];
  const sortedTexts = Object.fromEntries([...texts.entries()].sort(([a], [b]) => a.localeCompare(b)));
  return (
    JSON.stringify(
      {
        $comment: "Generated by scripts/app-licenses/generate.mjs (bun run licenses:generate). Do not edit.",
        sections,
        texts: sortedTexts,
      },
      null,
      1,
    ) + "\n"
  );
}

function pick(entry) {
  const { name, version, license, url, platforms: only, note } = entry;
  return { name, version, license, url, platforms: only, note };
}

function nativePlatforms(entry) {
  if (entry.platforms) return entry.platforms.length === platforms.length ? undefined : entry.platforms;
  const on = [];
  if (entry.maven?.length) on.push("android");
  if (entry.pods?.length) on.push("ios");
  return on.length === platforms.length ? undefined : on;
}

// ---- Native build checks ----

function listedPackages() {
  const current = JSON.parse(readFileSync(outputPath, "utf8"));
  return new Set(current.sections.flatMap((section) => section.items.map((entry) => entry.name)));
}

const globMatch = (pattern, name) =>
  pattern.endsWith("*") ? name.startsWith(pattern.slice(0, -1)) : pattern === name;

function checkPods(lockPath) {
  const lock = readFileSync(lockPath, "utf8");
  const pods = new Set();
  const podsSection = lock.split(/^DEPENDENCIES:/m)[0];
  for (const match of podsSection.matchAll(/^ {2}- "?([^\s/"(]+)/gm)) pods.add(match[1]);
  const external = new Map();
  const externalSection = lock.split(/^EXTERNAL SOURCES:/m)[1]?.split(/^\S/m)[0] ?? "";
  for (const match of externalSection.matchAll(/^ {2}([^:\s]+):\n {4}:path: "?([^"\n]+)"?/gm)) {
    external.set(match[1], match[2]);
  }
  const listed = listedPackages();
  const curated = components.native.flatMap((entry) => entry.pods ?? []);
  const problems = [];
  for (const pod of [...pods].sort()) {
    const path = external.get(pod);
    if (path && path.includes("node_modules/")) {
      const after = path.slice(path.lastIndexOf("node_modules/") + "node_modules/".length).split("/");
      const name = after[0].startsWith("@") ? `${after[0]}/${after[1]}` : after[0];
      if (!listed.has(name)) problems.push(`pod ${pod} is built from ${name}, which ${outputName} does not list`);
      continue;
    }
    if (path) continue; // first-party: app/modules/*
    if (curated.includes(pod) || components.npmProvided.pods.some((pattern) => globMatch(pattern, pod))) continue;
    problems.push(`pod ${pod} is not in scripts/app-licenses/components.json`);
  }
  return problems;
}

function checkMaven(depsPath) {
  const text = readFileSync(depsPath, "utf8");
  const groups = new Set();
  for (const match of text.matchAll(/[+\\]--- ([\w.-]+):([\w.-]+):/g)) groups.add(match[1]);
  const covered = [
    ...components.native.flatMap((entry) => entry.maven ?? []),
    ...components.npmProvided.maven,
  ];
  const problems = [];
  for (const group of [...groups].sort()) {
    if (covered.some((prefix) => group === prefix || group.startsWith(prefix + "."))) continue;
    problems.push(`Maven group ${group} is not in scripts/app-licenses/components.json`);
  }
  const listed = listedPackages();
  const gradleNames = new Set(
    [...listed].filter((name) => name.startsWith("@")).flatMap((name) => {
      const [scope, bare] = name.slice(1).split("/");
      return [`${scope}_${bare}`, `${scope}-${bare}`];
    }),
  );
  for (const match of text.matchAll(/[+\\]--- project :([\w@/.-]+)/g)) {
    const project = match[1];
    // Gradle names a scoped package "@scope/name" as "scope_name" or "scope-name".
    if (project === "app" || listed.has(project) || gradleNames.has(project)) continue;
    if (existsSync(join(root, "app", "modules", project))) continue;
    problems.push(`Gradle project ${project} is not a listed package`);
  }
  return [...new Set(problems)];
}

// ---- Main ----

const podsLock = option("--pods");
const mavenDeps = option("--maven");
if (podsLock || mavenDeps) {
  const problems = [...(podsLock ? checkPods(podsLock) : []), ...(mavenDeps ? checkMaven(mavenDeps) : [])];
  if (problems.length > 0) {
    fail(`Open-source licenses do not cover this native build:\n  ${problems.join("\n  ")}\nAdd them to scripts/app-licenses/components.json and run bun run licenses:generate.`);
  }
  console.log("Open-source licenses cover every native dependency.");
  process.exit(0);
}

let bundleDir = option("--bundle-dir");
let scratch = null;
if (!bundleDir) {
  scratch = mkdtempSync(join(process.env.TMPDIR || tmpdir(), "mewla-licenses-"));
  bundleDir = scratch;
  exportBundles(bundleDir);
}
try {
  const content = build(bundleDir);
  const current = existsSync(outputPath) ? readFileSync(outputPath, "utf8") : "";
  if (args.includes("--check")) {
    if (current !== content) fail(`stale: ${outputName} (run: bun run licenses:generate)`);
    console.log(`${outputName} is current`);
  } else if (current !== content) {
    writeFileSync(outputPath, content);
    console.log(`wrote ${outputName}`);
  } else {
    console.log(`${outputName} is current`);
  }
} finally {
  if (scratch) rmSync(scratch, { recursive: true, force: true });
}
