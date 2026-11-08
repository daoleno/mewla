import { useMemo } from "react";
import type {
  GitBranchesPayload,
  GitCommitDetail,
  GitDiscardUndo,
  GitHunkAction,
  GitLogPayload,
} from "../../services/gitDiff";
import { messageOutbox } from "../../services/messageOutboxRuntime";
import { wsClient } from "../../services/websocket";

/**
 * Everything the review sheet can do to the Session's repository. Each change
 * resolves after the daemon has done it (the owner refreshes the status then)
 * and rejects with Git's own message.
 */
export interface GitReviewActions {
  stage(paths: string[]): Promise<void>;
  unstage(paths: string[]): Promise<void>;
  /** `working` throws away unstaged edits; `all` also drops what is staged. */
  discard(paths: string[], scope: "working" | "all"): Promise<GitDiscardUndo[]>;
  undoDiscard(undo: GitDiscardUndo[]): Promise<void>;
  hunk(path: string, scope: "staged" | "unstaged", action: GitHunkAction, patch: string): Promise<void>;
  commit(message: string): Promise<{ sha: string; subject: string }>;
  push(): Promise<string>;
  pull(): Promise<string>;
  /** Updates the remote-tracking branches, so ahead and behind are true. */
  fetch(): Promise<void>;
  branches(): Promise<GitBranchesPayload>;
  switchBranch(branch: string, create?: boolean): Promise<void>;
  log(skip: number): Promise<GitLogPayload>;
  show(sha: string): Promise<GitCommitDetail>;
  /** Null when this Session has no agent to ask (a plain shell). */
  askAgent: ((text: string) => void) | null;
}

export const LOG_PAGE = 40;

export function useGitReviewActions({
  serverId,
  workerId,
  cwd,
  repoRoot,
  canAskAgent,
  refresh,
}: {
  serverId: string;
  workerId: string;
  cwd: string;
  repoRoot: string | undefined;
  canAskAgent: boolean;
  /** Reloads the status (and with it the open diff) after a change. */
  refresh(): Promise<void>;
}): GitReviewActions {
  return useMemo(() => {
    const target = { targetId: workerId, cwd, repoRoot };
    const change = async <T>(action: string, fields?: Record<string, unknown>, timeoutMs?: number) => {
      try {
        return await wsClient.runGitAction<T>(serverId, action, { ...target, fields, timeoutMs });
      } finally {
        await refresh();
      }
    };
    return {
      stage: async (paths) => void (await change("stage", { paths })),
      unstage: async (paths) => void (await change("unstage", { paths })),
      discard: async (paths, scope) => {
        const result = await change<{ undo?: GitDiscardUndo[] }>("discard", { paths, scope });
        return result.undo ?? [];
      },
      undoDiscard: async (undo) => void (await change("discard_undo", { undo })),
      hunk: async (path, scope, action, patch) => void (await change("hunk", { path, scope, action, patch })),
      commit: async (message) => {
        const result = await change<{ sha?: string; subject?: string }>("commit", { message });
        return { sha: result.sha ?? "", subject: result.subject ?? "" };
      },
      push: async () => (await change<{ message?: string }>("push", undefined, 90000)).message ?? "",
      pull: async () => (await change<{ message?: string }>("pull", undefined, 90000)).message ?? "",
      fetch: async () => void (await change("fetch", undefined, 90000)),
      branches: () => wsClient.getGitBranches(serverId, target),
      switchBranch: async (branch, create) => void (await change("switch", { branch, create: Boolean(create) })),
      log: (skip) => wsClient.getGitLog(serverId, { ...target, limit: LOG_PAGE, skip }),
      show: (sha) => wsClient.getGitShow(serverId, { ...target, sha }),
      askAgent: canAskAgent
        ? (text) => {
            messageOutbox.enqueue({ serverId, workerId, text });
          }
        : null,
    };
  }, [serverId, workerId, cwd, repoRoot, canAskAgent, refresh]);
}
