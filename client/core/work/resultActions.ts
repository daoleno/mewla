/**
 * A Work result's next steps (daemon WIRE §4): what the result truthfully offers, typed. The labels
 * are the client's; the daemon orders them (pull requests, Git review, files, Brain's files, the worklog).
 */
import type { ResultAction } from "./types";

export function parseResultActions(raw: unknown): ResultAction[] | undefined {
  if (!Array.isArray(raw)) return undefined;
  const text = (value: unknown) => (typeof value === "string" && value.trim() ? value.trim() : undefined);
  const out: ResultAction[] = [];
  for (const item of raw) {
    if (!item || typeof item !== "object") continue;
    const a = item as Record<string, unknown>;
    const url = text(a.url), session = text(a.session_id), cwd = text(a.cwd), path = text(a.path);
    if (a.kind === "pull_request" && url && /^https?:\/\//.test(url)) out.push({ kind: "pull_request", url });
    else if (a.kind === "git_review" && session && cwd) out.push({ kind: "git_review", session_id: session, cwd });
    else if (a.kind === "file" && session && cwd && path) out.push({ kind: "file", session_id: session, cwd, path });
    else if (a.kind === "brain_file" && path) out.push({ kind: "brain_file", path });
    else if (a.kind === "brain_folder" && path) out.push({ kind: "brain_folder", path });
  }
  return out.length ? out : undefined;
}

const base = (path: string) => path.replace(/\/+$/, "").split("/").at(-1) || path;

/** A button's words and glyph: "Pull request #4242", "Review changes", "NOTES.md", "Worklog". */
export function resultActionLabel(action: ResultAction): { label: string; icon: "git-compare" | "git-branch" | "document-text" | "folder-open" } {
  switch (action.kind) {
    case "pull_request": {
      const number = action.url.match(/\/(?:pull|merge_requests)\/(\d+)/)?.[1];
      return { label: number ? `Pull request #${number}` : "Pull request", icon: "git-compare" };
    }
    case "git_review":
      return { label: "Review changes", icon: "git-branch" };
    case "file":
    case "brain_file":
      return { label: base(action.path), icon: "document-text" };
    case "brain_folder":
      return { label: "Worklog", icon: "folder-open" };
  }
}

/** Stable across updates: the thread's row compares them. */
export function resultActionKey(action: ResultAction): string {
  return action.kind === "pull_request" ? `pr:${action.url}` : action.kind === "git_review" ? `git:${action.session_id}:${action.cwd}` : action.kind === "file" ? `file:${action.session_id}:${action.cwd}:${action.path}` : `${action.kind}:${action.path}`;
}

export function sameResultActions(left: readonly ResultAction[] | undefined, right: readonly ResultAction[] | undefined): boolean {
  if (left === right) return true;
  if ((left?.length ?? 0) !== (right?.length ?? 0)) return false;
  return (left ?? []).every((action, index) => resultActionKey(action) === resultActionKey(right![index]!));
}
