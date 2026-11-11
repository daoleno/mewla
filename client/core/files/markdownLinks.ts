/**
 * What a link in a previewed Markdown file means (the Expo app's
 * components/markdown/markdownLinks.ts): web links and mail open outside the
 * app, a destination without a scheme is a file of the same Session (relative
 * to the previewed file's folder), and any other scheme never runs.
 */

export type MarkdownLinkTarget =
  | { kind: "external"; url: string }
  | { kind: "file"; reference: string }
  | { kind: "blocked" };

const EXTERNAL_PROTOCOLS = new Set(["http:", "https:", "mailto:", "tel:"]);
const SCHEME = /^[a-z][a-z0-9+.-]*:/i;
const DRIVE_PATH = /^[a-z]:[\\/]/i;
const SESSION_FILE_PATH = "/session-file";

export function resolveMarkdownLink(value: string, baseDir?: string): MarkdownLinkTarget {
  const href = value.trim();
  if (!href || href.startsWith("#") || href.includes("\u0000")) return { kind: "blocked" };
  if (/^file:/i.test(href)) return fileTarget(fileUrlPath(href), baseDir);
  if (SCHEME.test(href) && !DRIVE_PATH.test(href)) {
    let url: URL;
    try {
      url = new URL(href);
    } catch {
      return { kind: "blocked" };
    }
    if (!EXTERNAL_PROTOCOLS.has(url.protocol)) return { kind: "blocked" };
    const served = url.searchParams.get("path");
    if (url.pathname === SESSION_FILE_PATH && served) return fileTarget(served, baseDir);
    return { kind: "external", url: href };
  }
  return fileTarget(href, baseDir);
}

function fileUrlPath(href: string): string | null {
  try {
    const url = new URL(href);
    return url.host && url.host !== "localhost" ? null : url.pathname;
  } catch {
    return null;
  }
}

function fileTarget(raw: string | null, baseDir: string | undefined): MarkdownLinkTarget {
  let value = (raw ?? "").replace(/[?#][\s\S]*$/, "");
  try {
    value = decodeURIComponent(value);
  } catch {
    return { kind: "blocked" };
  }
  value = value.replace(/:\d+(?::\d+)?$/, "").trim();
  if (!value || value.length > 4096 || value.includes("\u0000")) return { kind: "blocked" };
  return { kind: "file", reference: resolveAgainst(baseDir, value) };
}

/** Anchors a relative file reference at a folder; absolute ones stay as they are. */
export function resolveAgainst(baseDir: string | undefined, reference: string): string {
  const absolute = reference.startsWith("/") || reference.startsWith("~/") || DRIVE_PATH.test(reference);
  return absolute || !baseDir ? reference : joinPath(baseDir, reference);
}

function joinPath(baseDir: string, relative: string): string {
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

export function dirnameOf(path: string): string {
  const index = path.lastIndexOf("/");
  return index <= 0 ? (index === 0 ? "/" : "") : path.slice(0, index);
}
