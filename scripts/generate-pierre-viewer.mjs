#!/usr/bin/env bun
// Regenerate the offline Git review viewers (app/components/terminal/pierre/
// runtime) from the installed `@pierre/diffs` and `@pierre/trees` devDependencies,
// which app/package.json pins.
//
// Two documents, each with two outputs, because the WebView loads no remote
// scripts and the web UI's CSP allows only same-origin scripts:
// - pierre{Diffs,Trees}Source.js: the native WebView document's script as a
//   string constant.
// - pierre{Diffs,Trees}Runtime.js: an ES module the web build imports directly.
//
// Shiki would bundle all ~700 grammars (11 MB) and every theme. The reader ships
// a fixed language set (LANGUAGES below; any other extension renders as plain
// text), and no bundled theme: it paints with one CSS-variable theme whose
// variables come from the app's terminal palette. Both are swapped in at build
// time by `trim`, so the library code itself is untouched.
//
// Usage:
//   bun install
//   bun run pierre:generate           # rewrite the outputs
//   bun run pierre:generate --check   # fail if any is stale

import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const outDir = "app/components/terminal/pierre";
const require = createRequire(join(root, "app", "package.json"));
const pkg = (name) => JSON.parse(readFileSync(join(dirname(require.resolve(`${name}/package.json`)), "package.json"), "utf8"));
const diffs = pkg("@pierre/diffs");
const trees = pkg("@pierre/trees");

// Grammar name -> @shikijs/langs module. Pierre's extension table also yields
// aliases (zsh for .sh, objective-cpp for .h, ...); ALIASES maps those onto a
// shipped grammar.
const LANGUAGES = [
  "typescript", "tsx", "javascript", "jsx", "json", "jsonc", "css", "scss", "html", "markdown", "yaml",
  "toml", "go", "rust", "python", "shellscript", "swift", "kotlin", "java", "c", "cpp", "ruby", "sql",
  "xml", "php", "dockerfile", "diff", "objective-c",
];
const ALIASES = {
  ts: "typescript", js: "javascript", md: "markdown", sh: "shellscript", bash: "shellscript", zsh: "shellscript",
  shell: "shellscript", py: "python", rb: "ruby", rs: "rust", yml: "yaml", kt: "kotlin", objc: "objective-c",
  "objective-cpp": "cpp", "c++": "cpp", mdx: "markdown", "angular-ts": "typescript", "angular-html": "html",
  svg: "xml", vue: "html", svelte: "html", astro: "html", less: "scss", sass: "scss", fish: "shellscript",
};

const langsModule = `
const shipped = {
${LANGUAGES.map((l) => `  ${JSON.stringify(l)}: () => import("@shikijs/langs/${l}"),`).join("\n")}
};
const aliases = ${JSON.stringify(ALIASES)};
// Any grammar Pierre asks for exists: unshipped ones are an empty grammar, so
// that file shows as plain text instead of failing to render.
const plain = (name) => () => Promise.resolve({ default: [{ name, scopeName: "text.plain." + name, patterns: [] }] });
export const bundledLanguages = new Proxy({}, {
  has: () => true,
  getOwnPropertyDescriptor: () => ({ value: true, enumerable: true, configurable: true }),
  get: (_, name) => typeof name !== "string" ? undefined : (shipped[name] ?? shipped[aliases[name]] ?? plain(name)),
});
`;

const stubs = {
  "shiki/langs": langsModule,
  "shiki/wasm": "export default null;",
  "shiki/engine/oniguruma": "export const createOnigurumaEngine = () => { throw new Error('oniguruma engine is not bundled'); };",
};
const emptyModule = "export default {}; export const bundledThemes = {};";

const trim = {
  name: "pierre-trim",
  setup(build) {
    build.onResolve({ filter: /^shiki\/(langs|wasm|engine\/oniguruma)$/ }, (args) => ({ path: args.path, namespace: "pierre-stub" }));
    build.onLoad({ filter: /.*/, namespace: "pierre-stub" }, (args) => ({ contents: stubs[args.path], loader: "js", resolveDir: join(root, "node_modules/@pierre/diffs") }));
    // Pierre's own themes, Shiki's themes and the WASM highlighter backend are
    // never used: the reader paints with the CSS-variable theme only.
    build.onResolve({ filter: /^(shiki\/themes|@shikijs\/themes($|\/)|@pierre\/theme($|\/)|@pierre\/highlights($|\/))/ }, (args) => ({ path: args.path, namespace: "pierre-empty" }));
    build.onResolve({ filter: /\/backends\/highlights(\.js)?$/ }, (args) => ({ path: args.path, namespace: "pierre-highlights" }));
    build.onResolve({ filter: /.*/ }, (args) => {
      if (/@shikijs[\\/]themes/.test(args.importer) || /@pierre[\\/](theme|highlights)[\\/]dist/.test(args.importer)) {
        return { path: args.path + "#" + args.importer, namespace: "pierre-empty" };
      }
      return undefined;
    });
    build.onLoad({ filter: /.*/, namespace: "pierre-empty" }, () => ({ contents: emptyModule, loader: "js" }));
    build.onLoad({ filter: /.*/, namespace: "pierre-highlights" }, () => ({ contents: "export const HighlightsHighlighter = { create() { throw new Error('highlights backend is not bundled'); } };", loader: "js" }));
  },
};

// A single non-ASCII character makes Hermes store the whole string constant as
// UTF-16, twice the size (867 such characters sit in 3.5 MB). Escaped, the value
// is plain ASCII and the code is unchanged: they only occur inside string,
// regex and comment text.
const asciiOnly = (code) => code.replace(/[\u0080-\uffff]/g, (ch) => "\\u" + ch.charCodeAt(0).toString(16).padStart(4, "0"));

async function bundle(entry, format) {
  const result = await Bun.build({
    entrypoints: [join(root, outDir, "runtime", entry)],
    target: "browser",
    format,
    minify: true,
    plugins: [trim],
  });
  if (!result.success) {
    for (const log of result.logs) console.error(log);
    process.exit(1);
  }
  const code = await result.outputs[0].text();
  if (process.env.PIERRE_DEBUG) writeFileSync(join(process.env.PIERRE_DEBUG, entry), code);
  if (/(?<![\w$.\\])import\(\s*(["'`]|[A-Za-z_$])/.test(code)) throw new Error(`${entry}: a dynamic import survived, so the document would fetch code at run time`);
  if (/\.wasm\b/.test(code)) throw new Error(`${entry}: a WASM module was bundled`);
  return code;
}

const diffsNative = asciiOnly(await bundle("diffsBootstrap.js", "iife"));
const diffsWeb = await bundle("diffs.js", "esm");
const treesNative = asciiOnly(await bundle("treesBootstrap.js", "iife"));
const treesWeb = await bundle("trees.js", "esm");
const sha256 = createHash("sha256").update(diffsNative).update(diffsWeb).update(treesNative).update(treesWeb).digest("hex");
const banner = (name, version, license) => `// Generated by scripts/generate-pierre-viewer.mjs from ${name} ${version} (${license}). Do not edit.\n`;
const diffsBanner = banner("@pierre/diffs", diffs.version, diffs.license);
const treesBanner = banner("@pierre/trees", trees.version, trees.license);

const outputs = {
  [`${outDir}/pierreDiffsSource.js`]: `${diffsBanner}export const PIERRE_DIFFS_SOURCE = ${JSON.stringify(diffsNative)};\n`,
  [`${outDir}/pierreDiffsRuntime.js`]: `${diffsBanner}${diffsWeb}\n`,
  [`${outDir}/pierreTreesSource.js`]: `${treesBanner}export const PIERRE_TREES_SOURCE = ${JSON.stringify(treesNative)};\n`,
  [`${outDir}/pierreTreesRuntime.js`]: `${treesBanner}${treesWeb}\n`,
  [`${outDir}/pierreMeta.ts`]: [
    `export const PIERRE_DIFFS_VERSION = ${JSON.stringify(diffs.version)};`,
    `export const PIERRE_TREES_VERSION = ${JSON.stringify(trees.version)};`,
    `export const PIERRE_LICENSE = ${JSON.stringify(diffs.license)};`,
    `export const PIERRE_VIEWER_SHA256 = ${JSON.stringify(sha256)};`,
    `export const PIERRE_LANGUAGES = ${JSON.stringify(LANGUAGES)};`,
    "",
  ].join("\n"),
};

const check = process.argv.includes("--check");
let stale = false;
for (const [relative, content] of Object.entries(outputs)) {
  const path = join(root, relative);
  let current = "";
  try {
    current = readFileSync(path, "utf8");
  } catch {}
  if (current === content) continue;
  if (check) {
    console.error(`stale: ${relative} (run: bun run pierre:generate)`);
    stale = true;
  } else {
    writeFileSync(path, content);
    console.log(`wrote ${relative} (${content.length} bytes)`);
  }
}
if (stale) process.exit(1);
console.log(`pierre viewers (diffs ${diffs.version}, trees ${trees.version}) sha256 ${sha256}`);
