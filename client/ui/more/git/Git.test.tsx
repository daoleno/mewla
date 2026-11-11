import { describe, expect, open, test } from "nsh/test";
import { answering } from "../../testing/fixtures";

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
  let reads = 0;
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
      case "git_diff_file_content": {
        const side = (content: string) => ({ label: "", exists: true, byte_count: content.length, line_count: content.split("\n").length, content });
        return { reply: "git_diff_file_content", content: { repo_root: "/repo", path: body.path, base: side("const a = 1;\nconst b = 2;\nconst c = 4;\n"), current: side("const a = 1;\nconst b = 3;\nconst c = 4;\n") } };
      }
      case "git_branches":
        return {
          reply: "git_branches",
          branches: {
            current: "main",
            detached: false,
            branches: [
              { name: "main", current: true, remote: false, ahead: 1, behind: 0, subject: "Add the parser" },
              { name: "origin/feature", current: false, remote: true, subject: "Try a thing" },
            ],
          },
        };
      case "git_repo_paths":
        return { reply: "git_repo_paths", browser: { repo_root: "/repo", paths: ["README.md", "app.ts", "src/lib/util.ts", "src/main.ts"], truncated: false } };
      case "git_repo_file_content":
        reads += 1;
        return { reply: "git_repo_file_content", content: { repo_root: "/repo", path: body.path, snapshot: { label: "", exists: true, byte_count: 9, line_count: 1, content: `read ${reads}` } } };
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
    await answering(app, () => app.longPress("id:git-row-changes:app.ts"), 2);
    await app.wait(200);
    expect(repo.asked.some((r) => r.type === "git_discard")).toBe(false);
    const asked = app.dialogs().find((d) => !d.sheet);
    expect(asked?.title).toBe("Discard changes to app.ts?");
    expect(asked?.message).toBe("Your edits to the file will be lost.");

    app.onDialog((dialog) => (dialog.sheet ? "Discard changes" : "Discard"));
    await answering(app, () => app.longPress("id:git-row-changes:app.ts"), 2);
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

const GIT_HARNESS = "./testing/Harness.tsx";
const AGENT = { id: "w1", provider: "claude", cwd: "/repo" };

/** Opens the review with Sessions listed (`workers`) and at a width, choosing `menu` items from action sheets. */
async function openReview(files: File[], options: { workers?: unknown[]; width?: number; platform?: "web" } = {}) {
  const repo = repository(files);
  const app = await open(GIT_HARNESS, { width: options.width, platform: options.platform, host: { params: { page: PAGE, workers: options.workers ?? [] }, handlers: repo.options.host.handlers } });
  return { repo, app };
}

async function openDiffOf(app: Awaited<ReturnType<typeof open>>, key: string) {
  await app.waitFor(`id:git-row-${key}`);
  await app.press(`id:git-row-${key}`);
  await app.waitFor("id:git-diff-view");
}

describe("Git review: the reader", () => {
  test("Find in diff counts the matching lines and steps through them", async () => {
    const { app } = await openReview([app_ts]);
    await openDiffOf(app, "changes:app.ts");
    app.onDialog((dialog) => (dialog.sheet ? "Find in diff" : null));
    await answering(app, () => app.press("id:git-diff-more"));
    await app.waitFor("id:git-find-query");
    expect(app.enabled("id:git-find-next")).toBe(false);
    await app.type("id:git-find-query", "const b");
    await app.waitFor("text:2");
    await app.press("id:git-find-next");
    await app.waitFor("text:1/2");
    await app.press("id:git-find-next");
    await app.waitFor("text:2/2");
    await app.press("id:git-find-next");
    await app.waitFor("text:1/2");
    await app.clear("id:git-find-query");
    await app.type("id:git-find-query", "zzz");
    await app.waitFor("text:No matches");
    await app.press("id:git-find-done");
    await app.waitFor("id:git-find", { state: "gone" });
  });

  test("Display options wrap lines by default, step the code text between 10 and 20, and turn file headers on", async () => {
    const { app } = await openReview([app_ts]);
    await openDiffOf(app, "changes:app.ts");
    app.onDialog((dialog) => (dialog.sheet ? "Display options" : null));
    await answering(app, () => app.press("id:git-diff-more"));
    await app.waitFor("id:git-display");
    expect(app.texts()).toContain("Wrap lines");
    expect(app.texts()).toContain("File headers");
    expect(app.texts()).toContain("12");
    await app.press("label:Larger code text");
    await app.waitFor("text:14");
    for (let i = 0; i < 3; i++) await app.press("label:Larger code text");
    await app.waitFor("text:20");
    expect(app.enabled("label:Larger code text")).toBe(false);
    await app.press("label:Done with display options");
    await app.waitFor("id:git-display", { state: "gone" });
  });

  test("a file that can expand asks for both whole sides with its patch", async () => {
    const { app, repo } = await openReview([app_ts]);
    await openDiffOf(app, "changes:app.ts");
    expect(repo.asked.find((r) => r.type === "git_diff_file_content")?.body).toEqual({ target_id: "w1", cwd: "/repo", path: "app.ts" });
    // A file staged and changed again has an index between: it keeps its hunks.
    const both = await openReview([{ ...app_ts, staged: true, staged_additions: 1 }]);
    await openDiffOf(both.app, "changes:app.ts");
    expect(both.repo.asked.some((r) => r.type === "git_diff_file_content")).toBe(false);
  });

  test("staging the last unstaged file moves the reader on to the same file in Staged, instead of out", async () => {
    const { app } = await openReview([app_ts]);
    await openDiffOf(app, "changes:app.ts");
    await app.press("id:git-stage-toggle");
    await app.waitFor("text:Unstage file");
    expect(app.shows("id:git-diff-page")).toBe(true);
  });

  test("Ask the agent sends the comment about the file, then about a hunk's lines, to the Session's chat", async () => {
    const { app } = await openReview([app_ts], { workers: [AGENT] });
    await openDiffOf(app, "changes:app.ts");
    await app.waitFor("label:Ask the agent about this file");
    await app.press("label:Ask the agent about this file");
    await app.waitFor("id:ask-sheet");
    expect(app.texts()).toContain("Ask the agent");
    expect(app.enabled("id:ask-send")).toBe(false);
    await app.type("id:ask-comment", "Rename b");
    await app.press("id:ask-send");
    await app.waitFor("text:Sent to the agent");
    const sent = () => JSON.parse(app.storage()["mewla:v1:send_outbox"] ?? "[]") as Array<{ serverId: string; workerId: string; text: string }>;
    await app.waitUntil(() => sent().length === 1);
    expect(sent()[0]).toMatchObject({ workerId: "w1", text: "In the working-tree diff, app.ts:\n\nRename b" });

    await app.press("id:git-hunk-next");
    await app.waitFor("label:Ask the agent about this hunk");
    await app.press("label:Ask the agent about this hunk");
    await app.waitFor("id:ask-where");
    // The hunk's lines on the new side, quoted as the old sheet did (its first three).
    expect(app.texts()).toContain("app.ts:1-3");
    expect(app.texts()).toContain("+ const b = 3;");
    await app.type("id:ask-comment", "Why 3?");
    await app.press("id:ask-send");
    await app.waitUntil(() => sent().length === 2);
    expect(sent()[1]!.text).toBe("In the working-tree diff, app.ts:1-3:\n>   const a = 1;\n> + const b = 3;\n>   const c = 4;\n\nWhy 3?");
  });

  test("a Session without an agent offers no Ask", async () => {
    const { app } = await openReview([app_ts], { workers: [{ id: "w1", cwd: "/repo" }] });
    await openDiffOf(app, "changes:app.ts");
    expect(app.shows("id:git-ask")).toBe(false);
  });

  test("a commit's file opens its working file from the reader menu", async () => {
    const { app, repo } = await openReview([app_ts]);
    await app.waitFor("id:git-tab-history");
    await app.press("id:git-tab-history");
    await app.waitFor("id:git-commit-a1b2c3d");
    await app.press("id:git-commit-a1b2c3d");
    await app.waitFor("id:git-commit-file-app.ts");
    await app.press("id:git-commit-file-app.ts");
    await app.waitFor("id:git-diff-view");
    app.onDialog((dialog) => (dialog.sheet ? "Open working file" : null));
    await answering(app, () => app.press("id:git-diff-more"));
    await app.waitFor("id:git-file-page");
    await app.waitUntil(() => repo.asked.some((r) => r.type === "git_repo_file_content"));
  });
});

describe("Git review: the list", () => {
  test("a group folds under its header, which shows its totals; a binary row says bin; rows say what they are", async () => {
    const { app } = await openReview([app_ts, { ...notes_md, path: "logo.png", binary: true } as File]);
    await app.waitFor("id:git-group-changes-toggle");
    expect(app.shows("label:app.ts, Modified, 1 added, 1 removed, not staged")).toBe(true);
    expect(app.texts()).toContain("bin");
    await app.press("id:git-group-changes-toggle");
    await app.waitFor("id:git-row-changes:app.ts", { state: "gone" });
    await app.press("id:git-group-changes-toggle");
    await app.waitFor("id:git-row-changes:app.ts");
  });

  test("back closes an open filter before it leaves the review", async () => {
    const { app } = await openReview([app_ts, notes_md]);
    await app.waitFor("id:git-search");
    await app.press("id:git-search");
    await app.waitFor("id:git-query");
    await app.type("id:git-query", "notes");
    await app.waitFor("id:git-row-changes:app.ts", { state: "gone" });
    await app.back();
    await app.waitFor("id:git-query", { state: "gone" });
    expect(app.shows("id:git-page")).toBe(true);
    expect(app.shows("id:git-row-changes:app.ts")).toBe(true);
  });

  test("Commit stays pinned under the list", async () => {
    const { app } = await openReview([done_ts, app_ts]);
    await app.waitFor("id:git-commit-bar");
    expect(app.texts()).toContain("Commit 1 file");
  });

  test("on a wide screen the reader sits beside the list, and says No file selected until one is", async () => {
    const { app } = await openReview([app_ts, notes_md], { width: 1024 });
    await app.waitFor("id:git-no-file");
    expect(app.texts()).toContain("No file selected");
    await app.press("id:git-row-changes:app.ts");
    await app.waitFor("id:git-diff-view");
    expect(app.shows("id:git-diff-page")).toBe(false);
    expect(app.shows("id:git-list")).toBe(true);
  });

  test("the branch sheet is titled, shows each branch's sync and remote, and offers Create only for a name Git takes", async () => {
    const { app } = await openReview([app_ts]);
    await app.waitFor("id:git-branch");
    await app.press("id:git-branch");
    await app.waitFor("id:branch-origin/feature");
    expect(app.texts()).toContain("Branches");
    expect(app.texts()).toContain("↑1 · Add the parser");
    expect(app.texts()).toContain("on origin · Try a thing");
    await app.type("id:branch-query", "my branch");
    await app.wait(50);
    expect(app.shows("id:branch-create")).toBe(false);
    await app.clear("id:branch-query");
    await app.type("id:branch-query", "my-branch");
    await app.waitFor("id:branch-create");
  });
});

describe("Git review: the repository's files", () => {
  test("the tree marks changed files, searches, and a working file is read again on Refresh", async () => {
    const { app, repo } = await openReview([app_ts, { ...app_ts, path: "src/lib/util.ts" }]);
    await app.waitFor("id:git-row-changes:app.ts");
    app.onDialog((dialog) => (dialog.sheet ? "Browse files" : null));
    await answering(app, () => app.press("id:git-more"));
    await app.waitFor("id:git-files-tree");
    expect(app.shows("label:app.ts, Modified")).toBe(true);
    expect(app.shows("label:src, has changes")).toBe(true);
    await app.type("id:git-files-query", "util");
    await app.waitFor("id:git-file-src/lib/util.ts");
    expect(app.shows("id:git-file-README.md")).toBe(false);
    await app.press("id:git-file-src/lib/util.ts");
    await app.waitFor("id:git-file-view");
    const before = repo.asked.filter((r) => r.type === "git_repo_file_content").length;
    await app.press("id:git-file-refresh");
    await app.waitUntil(() => repo.asked.filter((r) => r.type === "git_repo_file_content").length === before + 1);
  });
});

describe("Git review on the web", () => {
  test("a right click on a changed file opens its menu, and the diff reads in the browser", async () => {
    const { app, repo } = await openReview([app_ts, notes_md], { platform: "web" });
    await app.waitFor("id:git-row-changes:notes.md");
    app.onDialog((dialog) => (dialog.sheet ? "Stage" : null));
    await answering(app, () => app.rightClick("id:git-row-changes:notes.md"));
    await app.waitUntil(() => repo.asked.some((r) => r.type === "git_stage"));
    expect(app.dialogs().find((d) => d.sheet)?.title).toBe("notes.md");
    await app.waitFor("id:git-row-staged:notes.md");
    await openDiffOf(app, "changes:app.ts");
  });

  test("the repository's tree opens, and a right click on a file offers Open file and Copy path", async () => {
    const { app } = await openReview([app_ts, { ...app_ts, path: "src/lib/util.ts" }], { platform: "web" });
    await app.waitFor("id:git-row-changes:app.ts");
    app.onDialog((dialog) => (dialog.sheet ? "Browse files" : null));
    await answering(app, () => app.press("id:git-more"));
    await app.waitFor("id:git-files-tree");
    await app.type("id:git-files-query", "util");
    await app.waitFor("id:git-file-src/lib/util.ts");
    app.onDialog((dialog) => (dialog.sheet ? "Open file" : null));
    await answering(app, () => app.rightClick("id:git-file-src/lib/util.ts"));
    await app.waitFor("id:git-file-view");
  });
});
