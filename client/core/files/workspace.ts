import { sessionFileMediaKindForPath } from "./sessionFileMedia";

/** Brain's workspace on the current computer: the folder its notes and files live in. */

export interface BrainWorkspaceEntry {
  name: string;
  path: string;
  kind: "directory" | "file" | string;
  size?: number;
  modified_at?: string;
  children: BrainWorkspaceEntry[];
}

export interface BrainWorkspaceTree {
  workspace?: string;
  path?: string;
  generated_at?: string;
  entries: BrainWorkspaceEntry[];
}

export interface BrainWorkspaceFile {
  /** Pictures come back whole as a data URL. */
  data_url?: string;
  name: string;
  path: string;
  kind: "file" | string;
  language: "markdown" | "text" | string;
  content: string;
  size?: number;
  modified_at?: string;
}

type Raw = Record<string, unknown> | null | undefined;
const text = (value: unknown): string | undefined => (typeof value === "string" ? value : undefined);

export function normalizeBrainWorkspaceTree(raw: unknown): BrainWorkspaceTree {
  const source = (raw && typeof raw === "object" ? raw : {}) as Record<string, unknown>;
  return {
    workspace: text(source.workspace),
    path: text(source.path),
    generated_at: text(source.generated_at),
    entries: Array.isArray(source.entries) ? source.entries.map(normalizeBrainWorkspaceEntry).filter((entry) => entry.name) : [],
  };
}

export function normalizeBrainWorkspaceEntry(raw: unknown): BrainWorkspaceEntry {
  const source = (raw && typeof raw === "object" ? raw : {}) as Record<string, unknown>;
  return {
    name: text(source.name) ?? "",
    path: text(source.path) ?? "",
    kind: text(source.kind) ?? "file",
    size: typeof source.size === "number" ? source.size : undefined,
    modified_at: text(source.modified_at),
    children: Array.isArray(source.children) ? source.children.map(normalizeBrainWorkspaceEntry).filter((entry) => entry.name) : [],
  };
}

export function normalizeBrainWorkspaceFile(raw: unknown): BrainWorkspaceFile {
  const source = ((raw && typeof raw === "object" ? raw : {}) as Raw)!;
  return {
    data_url: text(source.data_url),
    name: text(source.name) ?? "",
    path: text(source.path) ?? "",
    kind: text(source.kind) ?? "file",
    language: text(source.language) ?? "text",
    content: text(source.content) ?? "",
    size: typeof source.size === "number" ? source.size : undefined,
    modified_at: text(source.modified_at),
  };
}

/** Folders first, then by name. */
export function sortWorkspaceEntries(entries: readonly BrainWorkspaceEntry[]): BrainWorkspaceEntry[] {
  return [...entries].sort((a, b) => (a.kind === "directory") === (b.kind === "directory") ? a.name.localeCompare(b.name) : a.kind === "directory" ? -1 : 1);
}

/** The folder at `trail` (entry paths, outermost first), or the root's entries. */
export function workspaceFolder(tree: BrainWorkspaceTree, trail: readonly string[]): BrainWorkspaceEntry[] {
  let entries = tree.entries;
  for (const path of trail) {
    const next = entries.find((entry) => entry.path === path && entry.kind === "directory");
    if (!next) return [];
    entries = next.children;
  }
  return sortWorkspaceEntries(entries);
}

export function brainWorkspaceMarkdownPath(path: string): boolean {
  return /\.(md|markdown)$/i.test(path);
}

export function brainWorkspaceEntryAccessibilityLabel(kind: string, name: string): string {
  return `${kind === "directory" ? "Open folder" : "Open file"} ${name}`;
}

export function brainWorkspaceEntryIconName(kind: string, path: string): "folder" | "document-text" | "document" | "play" {
  if (kind === "directory") return "folder";
  if (sessionFileMediaKindForPath(path)) return "play";
  return brainWorkspaceMarkdownPath(path) ? "document-text" : "document";
}

/**
 * A link inside a workspace Markdown file, as a workspace path relative to the
 * file's folder; null for links that leave the workspace (absolute, `~/`, a URL).
 */
export function workspaceLinkPath(fromFile: string, href: string): string | null {
  const target = href.trim().split("#", 1)[0]!.split("?", 1)[0]!;
  if (!target || /^[a-z][a-z0-9+.-]*:/i.test(target) || target.startsWith("/") || target.startsWith("~")) return null;
  const parts = fromFile.split("/").slice(0, -1);
  for (const segment of decodeURIComponent(target).split("/")) {
    if (!segment || segment === ".") continue;
    if (segment === "..") {
      if (!parts.length) return null;
      parts.pop();
    } else parts.push(segment);
  }
  return parts.join("/") || null;
}
