import type { Token, Tokens } from "marked";
import { markdownInlineTokens } from "./markdownInline";

export type MarkdownUrlOpener = (url: string) => Promise<unknown>;

export type MarkdownLinkPart = {
  text: string;
  url?: string;
};

export type MarkdownLinkTarget =
  | { kind: "external"; url: string }
  | { kind: "file"; reference: string }
  | { kind: "blocked" };

export interface MarkdownLinkOptions {
  /** Directory of the Markdown document, for links relative to it. */
  baseDir?: string;
  /** Whether this surface can open a file; without it file links are text. */
  canOpenFile?(reference: string): boolean;
}

/** A link that would do nothing when pressed is shown as plain text instead. */
export function isMarkdownLinkOpenable(
  value: string,
  { baseDir, canOpenFile }: MarkdownLinkOptions = {},
) {
  const target = resolveMarkdownLink(value, baseDir);
  if (target.kind === "file") {
    return canOpenFile?.(target.reference) ?? false;
  }
  return target.kind === "external";
}

export interface MarkdownLinkActions {
  openUrl: MarkdownUrlOpener;
  /** Opens a workspace or Session file; returns false when none can open it. */
  openFile?(reference: string): boolean;
  /** Directory of the Markdown document, for links relative to it. */
  baseDir?: string;
}

const EXTERNAL_PROTOCOLS = new Set(["http:", "https:", "mailto:", "tel:"]);
const SCHEME = /^[a-z][a-z0-9+.-]*:/i;
const DRIVE_PATH = /^[a-z]:[\\/]/i;
const SESSION_FILE_PATH = "/session-file";

/**
 * Decides what a Markdown destination means. Web links and mail open
 * outside the app, a destination without a scheme is a file, and any other
 * scheme (javascript:, data:, intent:, …) never runs.
 */
export function resolveMarkdownLink(
  value: string,
  baseDir?: string,
): MarkdownLinkTarget {
  const href = value.trim();
  if (!href || href.startsWith("#") || href.includes("\u0000")) {
    return { kind: "blocked" };
  }
  if (/^file:/i.test(href)) {
    return fileTarget(fileUrlPath(href), baseDir);
  }
  if (SCHEME.test(href) && !DRIVE_PATH.test(href)) {
    let url: URL;
    try {
      url = new URL(href);
    } catch {
      return { kind: "blocked" };
    }
    if (!EXTERNAL_PROTOCOLS.has(url.protocol)) {
      return { kind: "blocked" };
    }
    const served = url.searchParams.get("path");
    if (url.pathname === SESSION_FILE_PATH && served) {
      return fileTarget(served, baseDir);
    }
    return { kind: "external", url: href };
  }
  return fileTarget(href, baseDir);
}

export async function openMarkdownLink(
  value: string,
  actions: MarkdownLinkActions,
) {
  const target = resolveMarkdownLink(value, actions.baseDir);
  if (target.kind === "file") {
    return actions.openFile?.(target.reference) ?? false;
  }
  if (target.kind !== "external") {
    return false;
  }
  try {
    await actions.openUrl(target.url);
    return true;
  } catch {
    return false;
  }
}

function fileUrlPath(href: string): string | null {
  try {
    const url = new URL(href);
    return url.host && url.host !== "localhost" ? null : url.pathname;
  } catch {
    return null;
  }
}

function fileTarget(
  raw: string | null,
  baseDir: string | undefined,
): MarkdownLinkTarget {
  let value = raw ?? "";
  value = value.replace(/[?#].*$/s, "");
  try {
    value = decodeURIComponent(value);
  } catch {
    return { kind: "blocked" };
  }
  value = value.replace(/:\d+(?::\d+)?$/, "").trim();
  if (!value || value.length > 4096 || value.includes("\u0000")) {
    return { kind: "blocked" };
  }
  return { kind: "file", reference: resolveAgainst(baseDir, value) };
}

/** Anchors a relative file reference at a directory; absolute ones stay as they are. */
export function resolveAgainst(baseDir: string | undefined, reference: string) {
  const absolute =
    reference.startsWith("/") || reference.startsWith("~/") || DRIVE_PATH.test(reference);
  return absolute || !baseDir ? reference : joinPath(baseDir, reference);
}

function joinPath(baseDir: string, relative: string) {
  const rooted = baseDir.startsWith("/");
  const segments: string[] = [];
  for (const segment of `${baseDir}/${relative}`.split("/")) {
    if (!segment || segment === ".") continue;
    if (segment === "..") {
      segments.pop();
      continue;
    }
    segments.push(segment);
  }
  return `${rooted ? "/" : ""}${segments.join("/")}`;
}

export function dirnameOf(path: string) {
  const index = path.lastIndexOf("/");
  return index <= 0 ? (index === 0 ? "/" : "") : path.slice(0, index);
}

/**
 * Splits source text around its links for the plain-text fallback, which
 * keeps every other character of the Markdown as written.
 */
export function tokenizeMarkdownLinks(value: string): MarkdownLinkPart[] {
  const links: { start: number; end: number; text: string; url: string }[] = [];
  const visit = (tokens: Token[], parent: string, base: number) => {
    let cursor = 0;
    for (const token of tokens) {
      const offset = parent.indexOf(token.raw, cursor);
      if (offset < 0 || !token.raw) continue;
      cursor = offset + token.raw.length;
      const start = base + offset;
      if (token.type === "link") {
        const link = token as Tokens.Link;
        links.push({ start, end: start + link.raw.length, text: link.text, url: link.href });
      } else if ("tokens" in token && Array.isArray(token.tokens)) {
        visit(token.tokens, token.raw, start);
      }
    }
  };
  visit(markdownInlineTokens(value), value, 0);

  const parts: MarkdownLinkPart[] = [];
  let lastIndex = 0;
  for (const link of links) {
    if (link.start > lastIndex) {
      parts.push({ text: value.slice(lastIndex, link.start) });
    }
    parts.push({ text: link.text, url: link.url });
    lastIndex = link.end;
  }
  if (lastIndex < value.length) {
    parts.push({ text: value.slice(lastIndex) });
  }
  return parts;
}
