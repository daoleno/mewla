import { Clipboard, effect, signal, untracked, type Signal } from "nsh";
import type { Daemon } from "../../../core/more/daemon";
import type { GitBranchesPayload, GitCommitDetail, GitCommitSummary, GitDiffStatusSnapshot, GitDiscardUndo, GitHunkAction, GitRepoFileContentPayload, GitRepoPathsPayload } from "../../../core/git/gitDiff";
import { buildGitReviewGroups, gitReviewOrder, gitReviewPaths, type GitReviewEntry, type GitReviewGroup } from "../../../core/git/gitReviewModel";
import { splitGitReviewPath } from "../../../core/git/gitReviewModel";
import {
  getGitBranches,
  getGitCommit,
  getGitLog,
  getGitRepoFile,
  getGitRepoPaths,
  getGitStatus,
  runGitAction,
  type GitAction,
  type GitTarget,
} from "../../../core/git/requests";
import { agentKindFromProvider } from "../../../core/sessions/workerPresentation";
import { supportsChatInterface } from "../../../core/session/sessionRoute";
import { outbox, workers } from "../../session";
import { daemon } from "../daemon";
import { confirmDestructive } from "../kit/kit";

/**
 * Git review of one Session's repository on the current computer: its status,
 * what is in flight, the last discard (for Undo), history and the repository's
 * files. One review per (computer, Session, folder), shared by every Git page; a
 * computer switch drops them all, and an answer from a computer that is no
 * longer current changes nothing.
 */

export interface Flash {
  title: string;
  detail?: string;
  tone: "success" | "error" | "neutral";
  action?: { label: string; run: () => void };
}

/** The one passing line at the top of a Git page: what just happened, and Undo when it can be. */
export const gitFlash = signal<Flash | null>(null);
let flashTimer: ReturnType<typeof setTimeout> | null = null;

export function say(next: Flash): void {
  gitFlash.set(next);
  if (flashTimer) clearTimeout(flashTimer);
  flashTimer = setTimeout(() => gitFlash.set(null), next.action ? 6_000 : 4_000);
}

function failed(title: string, reason: unknown): void {
  say({ title, detail: reason instanceof Error && reason.message ? reason.message : undefined, tone: "error" });
}

export interface History {
  commits: GitCommitSummary[];
  hasMore: boolean;
  /** The first page has answered. */
  loaded: boolean;
  loading: boolean;
  error: string;
  /** A failure after the first page: the list stays. */
  moreError: string;
}

export interface Loaded<T> {
  value: T | null;
  loading: boolean;
  error: string;
}

export class GitReview {
  readonly status: Signal<GitDiffStatusSnapshot | null> = signal(null);
  readonly loading = signal(false);
  readonly error = signal("");
  /** Keys of what is in flight (`staged:path`, `bulk:changes`, `sync`, `hunk`): only those controls spin. */
  readonly busy = signal<ReadonlySet<string>>(new Set());
  readonly lastDiscard = signal<(() => Promise<void>) | null>(null);
  readonly tree = signal(rememberedTree);
  readonly query = signal("");
  readonly searchOpen = signal(false);
  readonly collapsed = signal<ReadonlySet<string>>(new Set());
  readonly history = signal<History>({ commits: [], hasMore: false, loaded: false, loading: false, error: "", moreError: "" });
  readonly commits = signal<Record<string, Loaded<GitCommitDetail>>>({});
  readonly paths = signal<Loaded<GitRepoPathsPayload>>({ value: null, loading: false, error: "" });
  readonly files = signal<Record<string, Loaded<GitRepoFileContentPayload>>>({});
  private epoch = 0;

  constructor(
    readonly serverId: string,
    readonly target: GitTarget,
  ) {}

  private daemon(): Daemon | null {
    const current = daemon();
    return current && current.server.id === this.serverId ? current : null;
  }

  /** This review's computer is the current one (reactive). */
  current(): boolean {
    return daemon()?.server.id === this.serverId;
  }

  private live(): boolean {
    return reviews.get(reviewKey(this.serverId, this.target)) === this;
  }

  groups() {
    return buildGitReviewGroups(this.status()?.files ?? []);
  }

  order(): GitReviewEntry[] {
    return gitReviewOrder(this.groups());
  }

  entry(key: string): GitReviewEntry | null {
    return this.order().find((entry) => entry.key === key) ?? null;
  }

  repoRoot(): string {
    return this.status()?.repo_root ?? "";
  }

  /** Reads the status again; the newest answer wins. */
  async refresh(): Promise<void> {
    const d = this.daemon();
    if (!d) return;
    const mine = ++this.epoch;
    this.loading.set(true);
    try {
      const next = await getGitStatus(d, this.target);
      if (mine === this.epoch && this.live()) {
        this.status.set(next);
        this.error.set("");
      }
    } catch (reason) {
      if (mine === this.epoch && this.live()) this.error.set(reason instanceof Error ? reason.message : "Could not load Git status.");
    } finally {
      if (mine === this.epoch) this.loading.set(false);
    }
  }

  /**
   * Opening the review reads the status once (no polling while it is open: what
   * the person reads is a snapshot until they refresh). Every opening also
   * fetches, for a true ahead and behind. A fetch failure is not news here.
   */
  open(): void {
    void this.refresh().then(() => {
      const d = this.daemon();
      if (d && this.status()?.available && this.status()?.upstream) {
        runGitAction(d, this.target, this.repoRoot(), "fetch")
          .then(() => this.refresh())
          .catch(() => {});
      }
    });
  }

  /** One change; the status is read again after it, done or not. Rejects with Git's own message. */
  async change<T = Record<string, unknown>>(action: GitAction, fields: Record<string, unknown> = {}): Promise<T> {
    const d = this.daemon();
    if (!d) throw new Error("Server disconnected. Reconnect to check the result.");
    try {
      return await runGitAction<T>(d, this.target, this.repoRoot(), action, fields);
    } finally {
      await this.refresh();
    }
  }

  isBusy(key: string): boolean {
    return this.busy().has(key);
  }

  async withBusy(key: string, run: () => Promise<void>, failure: string): Promise<void> {
    if (this.busy().has(key)) return;
    this.busy.set(new Set(this.busy()).add(key));
    try {
      await run();
    } catch (reason) {
      failed(failure, reason);
    } finally {
      const next = new Set(this.busy());
      next.delete(key);
      this.busy.set(next);
    }
  }

  toggleStaged(entry: GitReviewEntry): Promise<void> {
    const staged = entry.group === "staged";
    return this.withBusy(entry.key, async () => void (await this.change(staged ? "unstage" : "stage", { paths: gitReviewPaths([entry]) })), staged ? "Couldn't unstage" : "Couldn't stage");
  }

  bulk(group: GitReviewGroup): Promise<void> {
    const paths = gitReviewPaths(this.groups()[group]);
    if (!paths.length) return Promise.resolve();
    return this.withBusy(`bulk:${group}`, async () => void (await this.change(group === "staged" ? "unstage" : "stage", { paths })), group === "staged" ? "Couldn't unstage" : "Couldn't stage");
  }

  private offerUndo(title: string, run: () => Promise<void>, failure = "Couldn't undo the discard"): void {
    this.lastDiscard.set(run);
    say({
      title,
      tone: "neutral",
      action: {
        label: "Undo",
        run: () => {
          this.lastDiscard.set(null);
          gitFlash.set(null);
          run().catch((reason) => failed(failure, reason));
        },
      },
    });
  }

  undoLastDiscard(): void {
    const undo = this.lastDiscard();
    if (!undo) return;
    this.lastDiscard.set(null);
    undo().catch((reason) => failed("Couldn't undo the discard", reason));
  }

  private async discardEntries(entries: GitReviewEntry[], title: string, message: string): Promise<boolean> {
    if (!(await confirmDestructive(title, message, "Discard"))) return false;
    try {
      const result = await this.change<{ undo?: GitDiscardUndo[] }>("discard", { paths: gitReviewPaths(entries), scope: "working" });
      const undo = result.undo ?? [];
      if (undo.length) this.offerUndo(entries.length === 1 ? "Changes discarded" : `${entries.length} files discarded`, async () => void (await this.change("discard_undo", { undo })));
      else say({ title: entries.length === 1 ? "Changes discarded" : `${entries.length} files discarded`, tone: "neutral" });
      return true;
    } catch (reason) {
      failed("Couldn't discard", reason);
      return false;
    }
  }

  discardFile(entry: GitReviewEntry): Promise<boolean> {
    const name = splitGitReviewPath(entry.file.path).name;
    return this.discardEntries([entry], `Discard changes to ${name}?`, entry.file.untracked ? `${name} is new and will be deleted.` : "Your edits to the file will be lost.");
  }

  discardAll(): Promise<boolean> {
    const entries = this.groups().changes;
    if (!entries.length) return Promise.resolve(false);
    const untracked = entries.filter((entry) => entry.file.untracked).length;
    return this.discardEntries(
      entries,
      `Discard ${entries.length} ${entries.length === 1 ? "file" : "files"}?`,
      untracked ? `Edits are reverted and ${untracked} new ${untracked === 1 ? "file is" : "files are"} deleted. Staged changes stay.` : "Edits are reverted. Staged changes stay.",
    );
  }

  /** Stages, unstages or discards one hunk; discarding asks first and offers Undo. */
  async hunk(path: string, group: GitReviewGroup, action: GitHunkAction, patch: string): Promise<void> {
    const scope = group === "staged" ? "staged" : "unstaged";
    if (action === "discard") {
      if (!(await confirmDestructive("Discard this hunk?", `Your edit to these lines in ${splitGitReviewPath(path).name} will be lost.`, "Discard"))) return;
      await this.withBusy("hunk", async () => {
        await this.change("hunk", { path, scope: "unstaged", action: "discard", patch });
        this.offerUndo("Hunk discarded", async () => void (await this.change("hunk", { path, scope: "unstaged", action: "apply", patch })), "Couldn't undo");
      }, "Couldn't discard the hunk");
      return;
    }
    await this.withBusy("hunk", async () => void (await this.change("hunk", { path, scope, action, patch })), action === "unstage" ? "Couldn't unstage the hunk" : "Couldn't stage the hunk");
  }

  /** Commits what is staged; pushes too when asked. Rejects (for the sheet) only when the commit failed. */
  async commit(message: string, push: boolean): Promise<void> {
    const result = await this.change<{ sha?: string; subject?: string }>("commit", { message });
    const label = result.sha ? result.sha.slice(0, 7) : "Committed";
    const detail = `${label} ${result.subject ?? ""}`.trim();
    this.reloadHistory();
    if (!push) return say({ title: "Committed", detail, tone: "success" });
    try {
      await this.change("push");
      say({ title: "Committed and pushed", detail, tone: "success" });
    } catch (reason) {
      failed(`Committed ${label}, but the push failed`, reason);
    }
  }

  sync(kind: "push" | "pull" | "fetch"): Promise<void> {
    return this.withBusy(
      "sync",
      async () => {
        const result = await this.change<{ message?: string }>(kind);
        if (kind !== "fetch") say({ title: kind === "push" ? "Pushed" : "Pulled", detail: result.message || undefined, tone: "success" });
        this.reloadHistory();
      },
      kind === "push" ? "Couldn't push" : kind === "pull" ? "Couldn't pull" : "Couldn't fetch",
    );
  }

  branches(): Promise<GitBranchesPayload> {
    const d = this.daemon();
    if (!d) return Promise.reject(new Error("Could not load branches."));
    return getGitBranches(d, this.target);
  }

  async switchBranch(branch: string, create: boolean): Promise<void> {
    await this.change("switch", { branch, create });
    this.reloadHistory();
    say({ title: create ? "Branch created" : "Switched branch", detail: branch, tone: "success" });
  }

  private historyEpoch = 0;

  reloadHistory(): void {
    if (this.history().loaded || this.history().loading || this.history().error) void this.loadHistory(0);
  }

  async loadHistory(skip: number): Promise<void> {
    const d = this.daemon();
    if (!d) return;
    const mine = ++this.historyEpoch;
    const before = this.history();
    this.history.set({ ...before, loading: true, ...(skip === 0 ? { error: "" } : { moreError: "" }) });
    try {
      const page = await getGitLog(d, this.target, skip);
      if (mine !== this.historyEpoch || !this.live()) return;
      const commits = skip === 0 ? page.commits : [...this.history().commits, ...page.commits.filter((c) => !this.history().commits.some((o) => o.sha === c.sha))];
      this.history.set({ commits, hasMore: page.has_more, loaded: true, loading: false, error: "", moreError: "" });
    } catch (reason) {
      if (mine !== this.historyEpoch) return;
      const message = reason instanceof Error ? reason.message : "Could not load history.";
      this.history.set({ ...this.history(), loading: false, ...(skip === 0 ? { error: message } : { moreError: message }) });
    }
  }

  async loadCommit(sha: string): Promise<void> {
    const d = this.daemon();
    if (!d) return;
    this.commits.set({ ...this.commits(), [sha]: { value: this.commits()[sha]?.value ?? null, loading: true, error: "" } });
    try {
      const detail = await getGitCommit(d, this.target, sha);
      if (this.live()) this.commits.set({ ...this.commits(), [sha]: { value: detail, loading: false, error: "" } });
    } catch (reason) {
      if (this.live()) this.commits.set({ ...this.commits(), [sha]: { value: null, loading: false, error: reason instanceof Error ? reason.message : "Could not load the commit." } });
    }
  }

  async loadPaths(): Promise<void> {
    const d = this.daemon();
    if (!d) return;
    this.paths.set({ ...this.paths(), loading: true, error: "" });
    try {
      const value = await getGitRepoPaths(d, this.target);
      if (this.live()) this.paths.set({ value, loading: false, error: "" });
    } catch (reason) {
      if (this.live()) this.paths.set({ value: this.paths().value, loading: false, error: reason instanceof Error ? reason.message : "Could not load repository files." });
    }
  }

  async loadFile(path: string): Promise<void> {
    const d = this.daemon();
    if (!d) return;
    this.files.set({ ...this.files(), [path]: { value: this.files()[path]?.value ?? null, loading: true, error: "" } });
    try {
      const value = await getGitRepoFile(d, this.target, path);
      if (this.live()) this.files.set({ ...this.files(), [path]: { value, loading: false, error: "" } });
    } catch (reason) {
      if (this.live()) this.files.set({ ...this.files(), [path]: { value: null, loading: false, error: reason instanceof Error ? reason.message : "Could not load the file." } });
    }
  }

  setTree(tree: boolean): void {
    rememberedTree = tree;
    this.tree.set(tree);
  }

  /** Collapses or expands a group (`section:staged`), a folder of the tree, or anything else keyed. */
  toggleCollapsed(key: string): void {
    const next = new Set(this.collapsed());
    if (next.has(key)) next.delete(key);
    else next.add(key);
    this.collapsed.set(next);
  }

  /** The Session runs an agent with a chat, which a review comment can go to (reactive). */
  canAskAgent(): boolean {
    if (!this.current()) return false;
    const worker = workers().find((w) => w.id === this.target.targetId);
    return !!worker && supportsChatInterface(agentKindFromProvider(worker.provider), worker.capabilities);
  }

  /**
   * Sends a review comment to the Session's agent as a chat message: the app's one send
   * queue (`outbox`) delivers it over this computer's connection, as a message typed in
   * the Session's chat would be.
   */
  askAgent(text: string): void {
    if (!this.current() || !text.trim()) return;
    outbox.enqueue({ serverId: this.serverId, workerId: this.target.targetId, text });
  }
}

// The list/tree choice outlives the review for the rest of the app session.
let rememberedTree = false;

const reviews = new Map<string, GitReview>();

function reviewKey(serverId: string, target: GitTarget): string {
  return `${serverId}\u0000${target.targetId}\u0000${target.cwd}`;
}

/** The review of one Session's repository on the current computer, or null without one. */
export function gitReview(target: GitTarget): GitReview | null {
  const current = untracked(daemon);
  if (!current) return null;
  const key = reviewKey(current.server.id, target);
  let review = reviews.get(key);
  if (!review) {
    review = new GitReview(current.server.id, { targetId: target.targetId, cwd: target.cwd });
    reviews.set(key, review);
  }
  return review;
}

// Another computer's repositories are not this one's.
let boundServer: string | null = null;
effect(() => {
  const id = daemon()?.server.id ?? null;
  untracked(() => {
    if (id === boundServer) return;
    boundServer = id;
    reviews.clear();
    gitFlash.set(null);
  });
});

/** Copies `text`; says `title` (with `detail`) when it went, "Couldn't copy" when it did not. */
export async function copyText(text: string, title: string, detail?: string): Promise<void> {
  try {
    await Clipboard.writeText(text);
    say({ title, detail, tone: "success" });
  } catch (reason) {
    failed("Couldn't copy", reason);
  }
}

/** Copy diff: the patch text, "Diff copied" with its line count, or "Nothing to copy". */
export function copyDiff(text: string): Promise<void> {
  if (!text.trim()) {
    say({ title: "Nothing to copy", tone: "neutral" });
    return Promise.resolve();
  }
  const lines = text.replace(/\n$/, "").split("\n").length;
  return copyText(text, "Diff copied", `${lines} ${lines === 1 ? "line" : "lines"}`).catch(() => {});
}
