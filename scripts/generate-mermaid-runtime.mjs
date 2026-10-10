#!/usr/bin/env node
// Regenerate the offline Mermaid runtime that the Markdown WebView inlines.
//
// The WebView loads no remote scripts, so the app ships mermaid.min.js as a
// string constant. This writes that constant and its version/SHA-256 metadata
// from the installed `mermaid` devDependency (app/package.json pins the exact
// version).
//
// Usage:
//   bun install
//   bun run mermaid:generate           # rewrite both files
//   bun run mermaid:generate --check   # fail if either file is stale

import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const require = createRequire(join(root, "app", "package.json"));
const packageDir = dirname(require.resolve("mermaid/package.json"));
const { version, license } = JSON.parse(readFileSync(join(packageDir, "package.json"), "utf8"));
const runtime = readFileSync(join(packageDir, "dist", "mermaid.min.js"), "utf8");
const sha256 = createHash("sha256").update(runtime).digest("hex");

const outputs = {
  "app/components/markdown/mermaidRuntimeSource.js":
    `export const MERMAID_RUNTIME_SOURCE = ${JSON.stringify(runtime)};\n`,
  "app/components/markdown/mermaidRuntimeMeta.ts": [
    `export const MERMAID_VERSION = ${JSON.stringify(version)};`,
    `export const MERMAID_RUNTIME_SHA256 = ${JSON.stringify(sha256)};`,
    `export const MERMAID_LICENSE = ${JSON.stringify(license)};`,
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
    console.error(`stale: ${relative} (run: bun run mermaid:generate)`);
    stale = true;
  } else {
    writeFileSync(path, content);
    console.log(`wrote ${relative}`);
  }
}
if (stale) process.exit(1);
console.log(`mermaid ${version} sha256 ${sha256}`);
