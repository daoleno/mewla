// Messages between the Git review sheet and its file tree (runtime/trees.js).

export type TreeViewerVars = Record<string, string>;

export interface TreeViewerDecoration {
  additions: number;
  deletions: number;
  binary?: boolean;
}

export interface TreeViewerGitStatus {
  path: string;
  /** The daemon's status name; the tree maps it onto the ones it draws. */
  status: string;
}

export type TreeHostMessage =
  | {
      type: "style";
      vars: TreeViewerVars;
      dark: boolean;
      fontFamily: string;
      fontSize: number;
      itemHeight: number;
    }
  | {
      type: "paths";
      paths: string[];
      gitStatus: TreeViewerGitStatus[];
      decorations: Record<string, TreeViewerDecoration>;
      expand: "open" | "closed";
      /** Shows Trees' own search field above the rows. */
      search: boolean;
      selected?: string;
    }
  /** Moves the selection highlight without rebuilding the tree or its scroll position. */
  | { type: "select"; path: string | null }
  | { type: "reveal"; path: string };

export type TreeViewerMessage =
  | { type: "ready" }
  | { type: "open"; path: string }
  | { type: "menu"; path: string; kind: "file" | "directory" }
  | { type: "error"; message: string };

export interface TreeViewerHostProps {
  onMessage(raw: string): void;
  backgroundColor: string;
  accessibilityLabel: string;
}

export interface TreeViewerHostHandle {
  post(batch: TreeHostMessage[]): void;
}

const VIEWER_TYPES = new Set(["ready", "open", "menu", "error"]);
const MAX_MESSAGE_BYTES = 20_000;

/** Parses a tree message; anything unexpected is dropped. */
export function parseTreeViewerMessage(raw: string): TreeViewerMessage | null {
  if (typeof raw !== "string" || raw.length > MAX_MESSAGE_BYTES) return null;
  try {
    const value = JSON.parse(raw);
    if (!value || typeof value.type !== "string" || !VIEWER_TYPES.has(value.type)) return null;
    switch (value.type) {
      case "open":
        if (typeof value.path !== "string" || !value.path) return null;
        break;
      case "menu":
        if (typeof value.path !== "string" || !value.path) return null;
        if (value.kind !== "file" && value.kind !== "directory") return null;
        break;
      case "error":
        if (typeof value.message !== "string") return null;
        break;
    }
    return value as TreeViewerMessage;
  } catch {
    return null;
  }
}
