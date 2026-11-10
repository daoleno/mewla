import { describe, expect, open, test } from "nsh/test";

const HARNESS = "../testing/Harness.tsx";
const PAGE = { name: "git", targetId: "w1", cwd: "/repo" };

interface File {
  path: string;
  status: string;
  staged: boolean;
  unstaged: boolean;
  untracked: boolean;
  staged_additions?: number;
  working_additions?: number;
  working_deletions?: number;
}

const PATCH = [
  "diff --git a/app.ts b/app.ts",
  "--- a/app.ts",
  "+++ b/app.ts",
  "@@ -1,3 +1,3 @@",
  " const a = 1;",
  "-const b = 2;",
  "+const b = 3;",
  " const c = 4;",
].join("\n");

const COMMITS = [
  { sha: "a1b2c3d4e5f6", short: "a1b2c3d", subject: "Add the parser", author: "Ada", date: Math.floor(Date.now() / 1000) - 3600, refs: ["main"] },
  { sha: "0f9e8d7c6b5a", short: "0f9e8d7", subject: "First commit", author: "Ada", date: Math.floor(Date.now() / 1000) - 86_400 },
];

/**
 * A computer with one repository: `files` is its status, changed by the Git
 * requests the page sends. Every request is recorded.
 */
function repository(initial: File[]) {
  let files = initial.map((f) => ({ ...f }));
  let discarded: File | null = null;
  const asked: Array<{ type: string; body: Record<string, unknown> }> = [];
  const action = (name: string, result: Record<string, unknown> = {}) => ({ reply: "git_action", action: name, result });
  const handler = ({ type, body }: { type: string; body: Record<string, unknown> }) => {
    asked.push({ type, body });
    const paths = (body.paths as string[] | undefined) ?? [];
    switch (type) {
      case "git_diff_status":
        return {
          reply: "git_diff_status",
          status: {
            available: true,
            repo_root: "/repo",
            repo_name: "repo",
            branch: "main",
            upstream: "origin/main",
            ahead: 0,
            behind: 0,
            has_commits: true,
            clean: files.length === 0,
            file_count: files.length,
            staged_file_count: files.filter((f) => f.staged).length,
            unstaged_file_count: files.filter((f) => f.unstaged).length,
            untracked_file_count: files.filter((f) => f.untracked).length,
            additions: 0,
            deletions: 0,
            files,
          },
        };
      case "git_fetch":
        return action("fetch");
      case "git_stage":
        files = files.map((f) => (paths.includes(f.path) ? { ...f, staged: true, unstaged: false, untracked: false, staged_additions: f.working_additions } : f));
        return action("stage");
      case "git_unstage":
        files = files.map((f) => (paths.includes(f.path) ? { ...f, staged: false, unstaged: true } : f));
        return action("unstage");
      case "git_discard":
        discarded = files.find((f) => paths.includes(f.path)) ?? null;
        files = files.filter((f) => !paths.includes(f.path));
        return action("discard", { undo: [{ path: paths[0], blob: "b10b", after: "", mode: "100644", staged: false }] });
      case "git_discard_undo":
        if (discarded) files = [...files, discarded];
        discarded = null;
        return action("discard_undo");
      case "git_hunk":
        return action("hunk");
      case "git_commit":
        files = files.filter((f) => !f.staged);
        return action("commit", { sha: "9999aaaabbbb", subject: body.message });
      case "git_diff_patch":
        return { reply: "git_diff_patch", patch: { repo_root: "/repo", path: body.path, sections: [{ scope: "unstaged", title: "Unstaged", patch: PATCH }] } };
      case "git_log":
        return { reply: "git_log", log: { commits: COMMITS, has_more: false } };
      case "git_show":
        return {
          reply: "git_show",
          commit: { ...COMMITS[0], body: "", patch: PATCH, files: [{ path: "app.ts", status: "modified", additions: 1, deletions: 1 }] },
        };
      default:
        return { error: `unexpected ${type}` };
    }
  };
  return { asked, handler, files: () => files, options: { host: { params: { page: PAGE }, handlers: { "app.daemon": handler } } } };
}

const app_ts: File = { path: "app.ts", status: "modified", staged: false, unstaged: true, untracked: false, working_additions: 1, working_deletions: 1 };
const notes_md: File = { path: "notes.md", status: "untracked", staged: false, unstaged: false, untracked: true, working_additions: 4 };
const done_ts: File = { path: "done.ts", status: "modified", staged: true, unstaged: false, untracked: false, staged_additions: 2 };

describe("Git review", () => {
  test("groups the changes into Staged and Changes with their counts, and reads the status once with one fetch", async () => {
    const repo = repository([done_ts, app_ts, notes_md]);
    const app = await open(HARNESS, repo.options);
    await app.waitFor("id:git-group-changes");
    expect(app.texts()).toContain("Staged · 1");
    expect(app.texts()).toContain("Changes · 2");
    expect(app.shows("id:git-row-staged:done.ts")).toBe(true);
    expect(app.shows("id:git-row-changes:app.ts")).toBe(true);
    expect(app.shows("id:git-row-changes:notes.md")).toBe(true);
    expect(app.texts()).toContain("Commit 1 file");
    await app.waitUntil(() => repo.asked.filter((r) => r.type === "git_diff_status").length === 2);
    expect(repo.asked.find((r) => r.type === "git_diff_status")?.body).toEqual({ target_id: "w1", cwd: "/repo" });
    expect(repo.asked.filter((r) => r.type === "git_fetch")).toHaveLength(1);
    // No polling while the page is open.
    await app.wait(500);
    expect(repo.asked.filter((r) => r.type === "git_diff_status")).toHaveLength(2);
  });

  test("staging a file sends its path and reads the status again", async () => {
    const repo = repository([app_ts, notes_md]);
    const app = await open(HARNESS, repo.options);
    await app.waitFor("id:git-toggle-changes:app.ts");
    const before = repo.asked.filter((r) => r.type === "git_diff_status").length;
    await app.press("id:git-toggle-changes:app.ts");
    await app.waitFor("id:git-row-staged:app.ts");
    const stage = repo.asked.find((r) => r.type === "git_stage");
    expect(stage?.body).toEqual({ target_id: "w1", cwd: "/repo", repo_root: "/repo", paths: ["app.ts"] });
    expect(repo.asked.filter((r) => r.type === "git_diff_status").length).toBeGreaterThan(before);
    expect(app.texts()).toContain("Staged · 1");
    expect(app.texts()).toContain("Changes · 1");

    // The group's button stages the rest.
    await app.press("id:git-bulk-changes");
    await app.waitFor("id:git-row-staged:notes.md");
    expect(repo.asked.filter((r) => r.type === "git_stage").at(-1)?.body.paths).toEqual(["notes.md"]);
  });

  test("discarding asks first, and Undo puts the file back", async () => {
    const repo = repository([app_ts]);
    const app = await open(HARNESS, repo.options);
    await app.waitFor("id:git-row-changes:app.ts");

    // Cancel changes nothing.
    app.onDialog((dialog) => (dialog.sheet ? "Discard changes" : "Cancel"));
    await app.longPress("id:git-row-changes:app.ts");
    await app.wait(200);
    expect(repo.asked.some((r) => r.type === "git_discard")).toBe(false);
    const asked = app.dialogs().find((d) => !d.sheet);
    expect(asked?.title).toBe("Discard changes to app.ts?");
    expect(asked?.message).toBe("Your edits to the file will be lost.");

    app.onDialog((dialog) => (dialog.sheet ? "Discard changes" : "Discard"));
    await app.longPress("id:git-row-changes:app.ts");
    await app.waitFor("id:git-flash-action");
    expect(repo.asked.find((r) => r.type === "git_discard")?.body).toEqual({ target_id: "w1", cwd: "/repo", repo_root: "/repo", paths: ["app.ts"], scope: "working" });
    expect(app.texts()).toContain("Changes discarded");
    await app.waitFor("id:git-clean");

    await app.press("id:git-flash-action");
    await app.waitFor("id:git-row-changes:app.ts");
    expect(repo.asked.find((r) => r.type === "git_discard_undo")?.body).toEqual({
      target_id: "w1",
      cwd: "/repo",
      repo_root: "/repo",
      undo: [{ path: "app.ts", blob: "b10b", after: "", mode: "100644", staged: false }],
    });
  });

  test("Commit sends the message and empties Staged", async () => {
    const repo = repository([done_ts, app_ts]);
    const app = await open(HARNESS, repo.options);
    await app.waitFor("id:git-commit-open");
    await app.press("id:git-commit-open");
    await app.waitFor("id:commit-message");
    expect(app.texts()).toContain("Commit & push");
    expect(app.enabled("id:commit-submit")).toBe(false);
    await app.type("id:commit-message", "Tidy the parser");
    await app.press("id:commit-submit");
    await app.waitFor("text:Committed");
    expect(repo.asked.find((r) => r.type === "git_commit")?.body).toEqual({ target_id: "w1", cwd: "/repo", repo_root: "/repo", message: "Tidy the parser" });
    expect(repo.asked.some((r) => r.type === "git_push")).toBe(false);
    await app.waitUntil(() => !app.shows("id:git-group-staged"));
    expect(app.texts()).toContain("Changes · 1");
  });

  test("a row opens its diff in the reader, and the hunk actions stage what is chosen", async () => {
    const repo = repository([app_ts, notes_md]);
    const app = await open(HARNESS, repo.options);
    await app.waitFor("id:git-row-changes:app.ts");
    await app.press("id:git-row-changes:app.ts");
    await app.waitFor("id:git-diff-view");
    expect(repo.asked.find((r) => r.type === "git_diff_patch")?.body).toEqual({ target_id: "w1", cwd: "/repo", path: "app.ts" });
    expect(app.texts()).toContain("1 hunk");
    expect(app.texts()).toContain("Stage file");
    expect(app.enabled("id:git-diff-previous")).toBe(false);
    expect(app.enabled("id:git-diff-next")).toBe(true);

    await app.press("id:git-hunk-next");
    await app.waitFor("text:Stage hunk");
    expect(app.texts()).toContain("1/1");
    await app.press("id:git-stage-toggle");
    await app.waitUntil(() => repo.asked.some((r) => r.type === "git_hunk"));
    const hunk = repo.asked.find((r) => r.type === "git_hunk")!.body;
    expect(hunk.path).toBe("app.ts");
    expect(hunk.scope).toBe("unstaged");
    expect(hunk.action).toBe("stage");
    expect(String(hunk.patch).startsWith("@@ -1,3 +1,3 @@")).toBe(true);

    // The next file is the untracked one.
    await app.press("id:git-diff-next");
    await app.waitUntil(() => repo.asked.filter((r) => r.type === "git_diff_patch").at(-1)?.body.path === "notes.md");
  });

  test("History lists the commits and a commit opens with its files and their diff", async () => {
    const repo = repository([app_ts]);
    const app = await open(HARNESS, repo.options);
    await app.waitFor("id:git-tab-history");
    await app.press("id:git-tab-history");
    await app.waitFor("text:Add the parser");
    expect(repo.asked.find((r) => r.type === "git_log")?.body).toEqual({ target_id: "w1", cwd: "/repo", limit: 40, skip: 0 });
    expect(app.texts()).toContain("First commit");
    await app.press("id:git-commit-a1b2c3d");
    await app.waitFor("id:git-commit-file-app.ts");
    expect(repo.asked.find((r) => r.type === "git_show")?.body).toEqual({ target_id: "w1", cwd: "/repo", sha: "a1b2c3d4e5f6" });
    await app.press("id:git-commit-file-app.ts");
    await app.waitFor("id:git-diff-view");
    expect(app.shows("id:git-stage-toggle")).toBe(false);
  });

  test("a switch to another computer drops this one's changes", async () => {
    const repo = repository([app_ts]);
    const app = await open(HARNESS, repo.options);
    await app.waitFor("id:git-row-changes:app.ts");
    await app.host.send("app.daemonSwitch", { id: "s2", name: "Laptop" });
    await app.waitFor("id:git-elsewhere");
    expect(app.shows("id:git-row-changes:app.ts")).toBe(false);
    expect(app.texts()).not.toContain("main");
  });
});
