#!/usr/bin/env bun
/**
 * Writes app/components/icons/phosphorGlyphs.ts from a @phosphor-icons/core
 * package: the path data of every glyph the app's Icon vocabulary borrows.
 *
 *   npm pack @phosphor-icons/core@2.1.1 && tar -xzf phosphor-icons-core-2.1.1.tgz
 *   bun scripts/vendor-phosphor-icons.ts package
 *
 * The app ships no Phosphor runtime: Icon draws these paths with
 * react-native-svg next to the hand-drawn Mewla glyphs (mewlaGlyphs.ts).
 */
import { readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";

type Weight = "bold" | "fill";

/** Icon name → Phosphor glyph. Bold is the app's one weight; fill marks notices. */
const GLYPHS: Record<string, [string, Weight?]> = {
  add: ["plus"],
  "add-circle": ["plus-circle"],
  alert: ["exclamation-mark"],
  "alert-circle": ["warning-circle"],
  "alert-circle-fill": ["warning-circle", "fill"],
  apps: ["squares-four"],
  "arrow-down": ["arrow-down"],
  "arrow-forward": ["arrow-right"],
  "arrow-up": ["arrow-up"],
  at: ["at"],
  attach: ["paperclip"],
  book: ["book-open"],
  browser: ["globe-simple"],
  bug: ["bug"],
  bulb: ["lightbulb"],
  calendar: ["calendar-dots"],
  "calendar-blank": ["calendar-blank"],
  "calendar-fill": ["calendar-dots", "fill"],
  camera: ["camera"],
  chat: ["chat-circle"],
  "chat-dots": ["chat-circle-dots"],
  chatbox: ["chat-centered"],
  "chatbox-dots": ["chat-centered-dots"],
  check: ["check"],
  "check-circle": ["check-circle"],
  "check-circle-fill": ["check-circle", "fill"],
  checkbox: ["check-square"],
  checks: ["checks"],
  "chevron-down": ["caret-down"],
  "chevron-left": ["caret-left"],
  "chevron-right": ["caret-right"],
  "chevron-up": ["caret-up"],
  chip: ["cpu"],
  circle: ["circle"],
  clipboard: ["clipboard"],
  close: ["x"],
  "close-circle": ["x-circle"],
  "close-circle-fill": ["x-circle", "fill"],
  "cloud-download": ["cloud-arrow-down"],
  "cloud-offline": ["cloud-slash"],
  "cloud-upload": ["cloud-arrow-up"],
  code: ["code"],
  construct: ["wrench"],
  contrast: ["circle-half"],
  copy: ["copy"],
  cube: ["cube"],
  cut: ["scissors"],
  desktop: ["desktop"],
  document: ["file"],
  "document-text": ["file-text"],
  dot: ["circle", "fill"],
  download: ["download-simple"],
  edit: ["pencil-simple"],
  enter: ["sign-in"],
  exit: ["sign-out"],
  expand: ["arrows-out"],
  eye: ["eye"],
  flag: ["flag"],
  flash: ["lightning"],
  flask: ["flask"],
  folder: ["folder"],
  "folder-open": ["folder-open"],
  "git-branch": ["git-branch"],
  "git-compare": ["git-diff"],
  "git-network": ["git-fork"],
  hand: ["hand"],
  happy: ["smiley"],
  help: ["question"],
  hourglass: ["hourglass"],
  image: ["image"],
  info: ["info"],
  "info-fill": ["info", "fill"],
  key: ["key"],
  keyboard: ["keyboard"],
  keypad: ["dots-nine"],
  layers: ["stack"],
  library: ["books"],
  link: ["link"],
  list: ["list-bullets"],
  lock: ["lock"],
  "lock-open": ["lock-open"],
  map: ["map-trifold"],
  memory: ["memory"],
  mic: ["microphone"],
  moon: ["moon"],
  "more-circle": ["dots-three-circle"],
  "more-horizontal": ["dots-three"],
  "more-vertical": ["dots-three-vertical"],
  navigate: ["navigation-arrow"],
  "notifications-off": ["bell-slash"],
  "open-external": ["arrow-square-out"],
  options: ["sliders-horizontal"],
  "paper-plane": ["paper-plane-tilt"],
  pause: ["pause"],
  "pause-circle": ["pause-circle"],
  paw: ["paw-print"],
  people: ["users"],
  person: ["user"],
  "person-circle": ["user-circle"],
  "person-remove": ["user-minus"],
  phone: ["device-mobile"],
  "play-forward": ["fast-forward"],
  plugins: ["plug"],
  power: ["power"],
  pulse: ["pulse"],
  puzzle: ["puzzle-piece"],
  "qr-code": ["qr-code"],
  radio: ["broadcast"],
  "radio-on": ["radio-button"],
  reader: ["article"],
  refresh: ["arrow-clockwise"],
  remove: ["minus"],
  "remove-circle": ["minus-circle"],
  reorder: ["list"],
  resources: ["gauge"],
  "return-forward": ["arrow-elbow-down-right"],
  scan: ["scan"],
  search: ["magnifying-glass"],
  server: ["hard-drives"],
  sessions: ["terminal-window"],
  settings: ["gear-six"],
  share: ["export"],
  "shield-check": ["shield-check"],
  skills: ["stack"],
  square: ["square"],
  stats: ["chart-bar"],
  stop: ["stop"],
  "stop-circle": ["stop-circle"],
  sun: ["sun"],
  "swap-horizontal": ["arrows-left-right"],
  "swap-vertical": ["arrows-down-up"],
  sync: ["arrows-clockwise"],
  terminal: ["terminal-window"],
  text: ["text-aa"],
  time: ["clock"],
  trash: ["trash"],
  undo: ["arrow-u-up-left"],
  warning: ["warning"],
  "warning-fill": ["warning", "fill"],
};

const packageDir = resolve(process.argv[2] ?? "");
const manifest = JSON.parse(readFileSync(join(packageDir, "package.json"), "utf8")) as { name: string; version: string };
if (manifest.name !== "@phosphor-icons/core") throw new Error(`${packageDir} is not @phosphor-icons/core`);

const lines = Object.entries(GLYPHS).map(([name, [glyph, weight = "bold"]]) => {
  const svg = readFileSync(join(packageDir, "assets", weight, `${glyph}-${weight}.svg`), "utf8");
  const paths = [...svg.matchAll(/<path d="([^"]+)"\/>/g)].map((match) => match[1]);
  const body = svg.replace(/^<svg[^>]*>|<\/svg>\s*$/g, "").replace(/<path d="[^"]+"\/>/g, "");
  if (!paths.length || body.trim()) throw new Error(`${glyph}-${weight}: expected plain <path> elements only`);
  return `  ${JSON.stringify(name)}: ${JSON.stringify(paths)},`;
});

const output = `// Generated by scripts/vendor-phosphor-icons.ts from ${manifest.name}@${manifest.version}. Do not edit.
//
// Phosphor Icons, https://phosphoricons.com
// MIT License, Copyright (c) 2023 Phosphor Icons
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

/** Filled path data on a 256 grid, keyed by icon name. */
export const PHOSPHOR_GLYPHS = {
${lines.join("\n")}
} as const satisfies Record<string, readonly string[]>;
`;

const target = resolve(import.meta.dir, "../app/components/icons/phosphorGlyphs.ts");
writeFileSync(target, output);
console.log(`wrote ${Object.keys(GLYPHS).length} glyphs to ${target}`);
