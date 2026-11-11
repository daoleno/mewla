/**
 * An SVG a Session showed, checked before it is drawn (the Expo app's
 * services/svgPreviewValidation.ts, without `sax`): a strict XML reading that
 * keeps only drawing elements of the SVG namespace, no scripts, styles, event
 * handlers, document declarations or references outside the file, and bounded
 * size, nodes, depth and area. What passes is drawn in a web view that runs no
 * scripts and loads nothing (`svgPreviewHtml`).
 */

export const MAX_SVG_BYTES = 2 << 20;

const SVG_NS = "http://www.w3.org/2000/svg";
const XLINK_NS = "http://www.w3.org/1999/xlink";
const XMLNS_NS = "http://www.w3.org/2000/xmlns/";
const XML_NS = "http://www.w3.org/XML/1998/namespace";

const DRAWING_TAGS = new Set([
  "svg", "g", "defs", "symbol", "use", "path", "rect", "circle", "ellipse",
  "line", "polyline", "polygon", "text", "tspan", "textPath", "clipPath",
  "mask", "pattern", "linearGradient", "radialGradient", "stop", "marker",
  "filter", "feBlend", "feColorMatrix", "feComponentTransfer", "feComposite",
  "feConvolveMatrix", "feDiffuseLighting", "feDisplacementMap", "feDistantLight",
  "feDropShadow", "feFlood", "feGaussianBlur", "feMerge", "feMergeNode",
  "feMorphology", "feOffset", "fePointLight", "feSpecularLighting", "feTile",
  "feTurbulence",
]);

const UNSUPPORTED = "Unsupported SVG content.";
const EXTERNAL = "External SVG references are unavailable.";
const MALFORMED = "The SVG could not be read.";
const NAME = /^[A-Za-z_][\w.-]*(?::[A-Za-z_][\w.-]*)?$/;
const PREDEFINED: Record<string, string> = { lt: "<", gt: ">", amp: "&", quot: '"', apos: "'" };

export interface SvgPreview {
  xml: string;
  /** Width over height, from the viewBox or the size. */
  ratio: number;
}

function decodeEntities(raw: string): string {
  if (raw.includes("<")) throw new Error(MALFORMED);
  return raw.replace(/&([^;\s&]*);?/g, (match, body: string) => {
    if (!match.endsWith(";")) throw new Error(MALFORMED);
    if (body in PREDEFINED) return PREDEFINED[body]!;
    const code = /^#x[0-9a-f]+$/i.test(body) ? parseInt(body.slice(2), 16) : /^#\d+$/.test(body) ? parseInt(body.slice(1), 10) : NaN;
    if (!Number.isFinite(code) || code <= 0 || code > 0x10ffff) throw new Error(MALFORMED);
    return String.fromCodePoint(code);
  });
}

interface Attribute {
  name: string;
  prefix: string;
  local: string;
  value: string;
}

/** `name="value"` pairs of a start tag, strictly: quoted values, no repeats. */
function readAttributes(source: string): Attribute[] {
  const attributes: Attribute[] = [];
  const seen = new Set<string>();
  let rest = source;
  for (;;) {
    rest = rest.replace(/^\s+/, "");
    if (!rest) return attributes;
    const match = /^([^\s=/>"']+)\s*=\s*(?:"([^"]*)"|'([^']*)')/.exec(rest);
    if (!match || !NAME.test(match[1]!) || seen.has(match[1]!)) throw new Error(MALFORMED);
    // Attributes are separated by whitespace.
    const after = rest.slice(match[0].length);
    if (after && !/^\s/.test(after)) throw new Error(MALFORMED);
    seen.add(match[1]!);
    const [prefix, local] = match[1]!.includes(":") ? match[1]!.split(":") as [string, string] : ["", match[1]!];
    attributes.push({ name: match[1]!, prefix, local, value: decodeEntities(match[2] ?? match[3] ?? "") });
    rest = after;
  }
}

/** Checks an SVG for the preview; throws what the person reads when it cannot be shown. */
export function validateSvgPreview(xml: string): SvgPreview {
  if (new TextEncoder().encode(xml).length > MAX_SVG_BYTES) throw new Error("SVG exceeds the preview limit.");
  let count = 0;
  let roots = 0;
  let width = 0;
  let height = 0;
  // Open elements, each with the namespaces in scope inside it.
  const stack: Array<{ name: string; ns: Record<string, string> }> = [];
  let at = 0;
  const text = xml.replace(/^﻿/, "");
  while (at < text.length) {
    const lt = text.indexOf("<", at);
    const chunk = text.slice(at, lt === -1 ? text.length : lt);
    if (chunk) {
      // Text only inside the root, and well formed.
      if (!stack.length && chunk.trim()) throw new Error(MALFORMED);
      decodeEntities(chunk);
    }
    if (lt === -1) break;
    if (text.startsWith("<!--", lt)) {
      const end = text.indexOf("-->", lt + 4);
      if (end === -1) throw new Error(MALFORMED);
      at = end + 3;
      continue;
    }
    if (/^<!DOCTYPE/i.test(text.slice(lt, lt + 9)) || text.startsWith("<!ENTITY", lt)) throw new Error("SVG document declarations are unavailable.");
    if (text.startsWith("<![CDATA[", lt) || text.startsWith("<!", lt)) throw new Error(UNSUPPORTED);
    if (text.startsWith("<?", lt)) {
      const end = text.indexOf("?>", lt + 2);
      if (end === -1) throw new Error(MALFORMED);
      const target = /^<\?([^\s?]+)/.exec(text.slice(lt, end + 2))?.[1] ?? "";
      // Only the XML declaration, and only before anything else.
      if (target.toLowerCase() !== "xml" || count || roots || lt !== text.length - text.trimStart().length) throw new Error(UNSUPPORTED);
      at = end + 2;
      continue;
    }
    const gt = findTagEnd(text, lt + 1);
    const inner = text.slice(lt + 1, gt);
    at = gt + 1;
    if (inner.startsWith("/")) {
      const name = inner.slice(1).trim();
      const open = stack.pop();
      if (!open || open.name !== name) throw new Error(MALFORMED);
      continue;
    }
    const selfClosing = inner.endsWith("/");
    const body = selfClosing ? inner.slice(0, -1) : inner;
    const nameMatch = /^([^\s/>]+)/.exec(body);
    if (!nameMatch || !NAME.test(nameMatch[1]!)) throw new Error(MALFORMED);
    const name = nameMatch[1]!;
    const attributes = readAttributes(body.slice(name.length));
    if (++count > 10_000 || stack.length + 1 > 64) throw new Error(UNSUPPORTED);
    if (!stack.length && ++roots !== 1) throw new Error(UNSUPPORTED);
    const ns: Record<string, string> = { ...(stack.at(-1)?.ns ?? { xml: XML_NS }) };
    for (const attribute of attributes) {
      if (attribute.name === "xmlns") ns[""] = attribute.value;
      else if (attribute.prefix === "xmlns") ns[attribute.local] = attribute.value;
    }
    const [prefix, local] = name.includes(":") ? name.split(":") as [string, string] : ["", name];
    if (!DRAWING_TAGS.has(local) || ns[prefix] !== SVG_NS) throw new Error(UNSUPPORTED);
    if (!stack.length) {
      if (local !== "svg") throw new Error(UNSUPPORTED);
      const box = attributes.find((a) => a.name === "viewBox")?.value.trim().split(/[\s,]+/).map(Number);
      width = box?.length === 4 ? box[2]! : Number(attributes.find((a) => a.name === "width")?.value);
      height = box?.length === 4 ? box[3]! : Number(attributes.find((a) => a.name === "height")?.value);
    }
    for (const attribute of attributes) checkAttribute(attribute, ns);
    if (!selfClosing) stack.push({ name, ns });
  }
  if (stack.length) throw new Error(MALFORMED);
  if (!count || !Number.isFinite(width) || !Number.isFinite(height) || width <= 0 || height <= 0 || width * height > 40_000_000) {
    throw new Error("SVG dimensions are unavailable or exceed the preview limit.");
  }
  return { xml, ratio: width / height };
}

/** The `>` that ends a tag, past any `>` inside its quoted values. */
function findTagEnd(text: string, from: number): number {
  let quote = "";
  for (let i = from; i < text.length; i++) {
    const c = text[i]!;
    if (quote) {
      if (c === quote) quote = "";
    } else if (c === '"' || c === "'") quote = c;
    else if (c === "<") break;
    else if (c === ">") return i;
  }
  throw new Error(MALFORMED);
}

function checkAttribute(attribute: Attribute, ns: Record<string, string>): void {
  const { name, prefix, local, value } = attribute;
  const uri = name === "xmlns" || prefix === "xmlns" ? XMLNS_NS : prefix ? ns[prefix] : "";
  if (uri === undefined) throw new Error(UNSUPPORTED);
  if (/^on[a-z]/i.test(local) || local === "dangerouslySetInnerHTML" || local === "style" || (uri && uri !== XLINK_NS && uri !== SVG_NS && uri !== XMLNS_NS && uri !== XML_NS)) {
    throw new Error(UNSUPPORTED);
  }
  if ((local === "href" || name === "xlink:href") && !/^#[\w.-]+$/.test(value)) throw new Error(EXTERNAL);
  if (/url\s*\(/i.test(value)) {
    const refs = [...value.matchAll(/url\s*\(\s*(['"]?)(.*?)\1\s*\)/gi)];
    if (!refs.length || refs.some((match) => !/^#[\w.-]+$/.test(match[2]!))) throw new Error(EXTERNAL);
  }
  if (/(?:https?:|file:|content:|data:|javascript:)/i.test(value) && name !== "xmlns" && !name.startsWith("xmlns:")) throw new Error(EXTERNAL);
}

/**
 * The page a checked SVG is drawn on: no scripts, nothing loaded (a policy that
 * allows no source at all), the picture fitted to the view.
 */
export function svgPreviewHtml(preview: SvgPreview, background: string): string {
  const body = preview.xml.replace(/^﻿/, "").replace(/^\s*<\?xml[^?]*\?>/i, "");
  return [
    "<!doctype html><html><head>",
    `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'">`,
    `<meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=4">`,
    `<style>html,body{margin:0;height:100%;background:${background.replace(/[^#\w(),.% -]/g, "")}}body{display:flex;align-items:center;justify-content:center}svg{max-width:100%;max-height:100%;width:100%;height:auto}</style>`,
    "</head><body>",
    body,
    "</body></html>",
  ].join("");
}

/** Reads an SVG from the signed stream, at most `MAX_SVG_BYTES`, then checks it. */
export async function loadSvgPreview(source: { uri: string; headers: Record<string, string> }, fetchText: (uri: string, headers: Record<string, string>) => Promise<Response>): Promise<SvgPreview> {
  if (!/^https?:\/\//i.test(source.uri)) throw new Error("Unsupported SVG source.");
  const response = await fetchText(source.uri, source.headers);
  if (!response.ok || Number(response.headers.get("content-length")) > MAX_SVG_BYTES) throw new Error("SVG is unavailable or exceeds the preview limit.");
  const buffer = new Uint8Array(await response.arrayBuffer());
  if (buffer.byteLength > MAX_SVG_BYTES) throw new Error("SVG exceeds the preview limit.");
  let xml: string;
  try {
    xml = new TextDecoder("utf-8", { fatal: true }).decode(buffer);
  } catch {
    throw new Error(UNSUPPORTED);
  }
  return validateSvgPreview(xml);
}
