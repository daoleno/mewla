import { computed, effect, signal, Text, untracked, View, windowSize } from "nsh";
import { AlertDialog, DiffView, NavPage, type DiffLineRange, type DiffViewRef } from "nsh/base";
import { gitHunkAtLine, gitHunkSelection, parseGitPatch, splitGitPatchByFile, type GitParsedPatch, type GitPatchHunk, type GitPatchSide } from "../../../core/git/gitDiffHunks";
import { formatGitDiffBytes, gitDiffPatchText, planGitDiffReview, type GitDiffSection } from "../../../core/git/gitDiffReview";
import type { GitDiffPatchPayload } from "../../../core/git/gitDiff";
import { splitGitReviewPath, type GitReviewEntry, type GitReviewGroup } from "../../../core/git/gitReviewModel";
import { getGitPatch } from "../../../core/git/requests";
import { daemon } from "../daemon";
import { EmptyState } from "../kit/kit";
import { Segmented } from "../kit/Segmented";
import type { PageNav } from "../routes";
import { Current, FlashLine, IconButton, TextButton } from "./parts";
import { copyText, type GitReview } from "./state";

/** Split (old beside new) from this width; one over the other below it. */
export const SPLIT_WIDTH = 720;

export function diffLayout(width: number): "split" | "unified" {
  return width >= SPLIT_WIDTH ? "split" : "unified";
}

type Reader =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "empty" }
  | { kind: "too-large"; bytes: number }
  | { kind: "ready"; sections: GitDiffSection[]; payload: GitDiffPatchPayload | null };

// The reader's lines count from 0; the patch model numbers file lines from 1.
function toModel(range: DiffLineRange): { side: GitPatchSide; line: number } {
  return { side: range.side === "new" ? "additions" : "deletions", line: range.start + 1 };
}

function toReader(selection: { side: GitPatchSide; start: number; end: number }): DiffLineRange {
  return { side: selection.side === "additions" ? "new" : "old", start: selection.start - 1, end: selection.end - 1 };
}

export function stepHunk(current: number, count: number, direction: "next" | "previous"): number {
  if (direction === "next") return current + 1 >= count ? 0 : current + 1;
  return current - 1 < 0 ? count - 1 : current - 1;
}

/** Scrolls the reader to the change that holds a hunk of the patch (the reader may cut a hunk finer). */
async function revealHunk(view: DiffViewRef | null, hunk: GitPatchHunk): Promise<void> {
  if (!view) return;
  const changes = await view.hunks();
  const first = hunk.oldStart - 1;
  const firstNew = hunk.newStart - 1;
  const found = changes.find((change) => change.newStart + change.newLines >= firstNew && change.newStart <= firstNew + Math.max(hunk.newCount, 1))
    ?? changes.find((change) => change.oldStart + change.oldLines >= first && change.oldStart <= first + Math.max(hunk.oldCount, 1));
  if (found) view.goToHunk(found.index);
}

/** One patch in the reader, with hunk selection: the shared middle of the working and commit diffs. */
function PatchReader(props: {
  section: () => GitDiffSection | null;
  parsed: () => GitParsedPatch | null;
  path: string;
  wrap: () => boolean;
  cursor: { get: () => number | null; set: (hunk: number | null) => void };
  onRef: (ref: DiffViewRef) => void;
}) {
  return (
    <DiffView
      testID="git-diff-view"
      ref={props.onRef}
      class="flex-1"
      patch={props.section()?.patch ?? ""}
      language={props.path}
      layout={diffLayout(windowSize().width)}
      softWrap={props.wrap()}
      onLineSelectionChange={({ range }) => {
        const parsed = props.parsed();
        if (!range || !parsed) return props.cursor.set(null);
        const at = toModel(range);
        props.cursor.set(gitHunkAtLine(parsed, at.side, at.line)?.index ?? null);
      }}
    />
  );
}

function ReaderHeader(props: { path: string; position?: { index: number; total: number }; onNavigate?: (direction: "previous" | "next") => void; onMenu: () => void }) {
  const parts = () => splitGitReviewPath(props.path);
  return (
    <View class="flex-row items-center gap-1 border-b border-border bg-background px-2 py-1">
      <View class="min-w-0 flex-1 px-2">
        <Text testID="git-diff-name" class="font-mono text-sm font-semibold line-clamp-1">{parts().name}</Text>
        {parts().directory ? <Text class="font-mono text-xs text-muted-foreground line-clamp-1">{parts().directory}</Text> : null}
      </View>
      {props.position && props.onNavigate ? (
        <>
          <IconButton testID="git-diff-previous" icon="arrow-up" label="Previous file" disabled={props.position.index <= 0} onPress={() => props.onNavigate!("previous")} />
          <IconButton testID="git-diff-next" icon="arrow-down" label="Next file" disabled={props.position.index >= props.position.total - 1} onPress={() => props.onNavigate!("next")} />
        </>
      ) : null}
      <IconButton testID="git-diff-more" icon="more-vertical" label="More actions" onPress={props.onMenu} />
    </View>
  );
}

function ReaderState(props: { reader: Reader; onRetry: () => void; working: boolean }) {
  const r = props.reader;
  if (r.kind === "loading") return <EmptyState busy title="Loading diff" />;
  if (r.kind === "error") return <EmptyState testID="git-diff-error" icon="alert" title="Couldn't load this diff" detail={r.message} action="Retry" onAction={props.onRetry} />;
  if (r.kind === "empty") return <EmptyState title={props.working ? "Nothing left to review here" : "No changes"} />;
  if (r.kind === "too-large") return <EmptyState title="Diff too large to show" detail={formatGitDiffBytes(r.bytes)} />;
  return null;
}

/**
 * One changed file's diff in the working tree or the index. A hunk is chosen
 * by pressing its lines (or stepping with the arrows); the actions then apply
 * to it, else to the whole file. After any change the patch is read again, and
 * when the file leaves its group the reader moves to the next one there.
 */
export function DiffPage(props: { review: GitReview; entry: string; nav: PageNav }) {
  const review = props.review;
  const key = signal(props.entry);
  const reader = signal<Reader>({ kind: "loading" });
  const sectionIndex = signal(0);
  const cursor = signal<number | null>(null);
  const wrap = signal(false);
  let view: DiffViewRef | null = null;
  let epoch = 0;
  // The entry as last seen, so a file that left its group keeps its place.
  let last = null as { group: GitReviewGroup; index: number; path: string } | null;

  const entry = () => review.entry(key());
  const kind = computed(() => reader().kind);
  const section = computed<GitDiffSection | null>(() => {
    const r = reader();
    return r.kind === "ready" ? r.sections[Math.min(sectionIndex(), r.sections.length - 1)] ?? null : null;
  });
  const parsed = computed<GitParsedPatch | null>(() => {
    const s = section();
    const e = untracked(entry);
    if (!s || !e || e.file.untracked || e.file.binary || s.id.endsWith("untracked")) return null;
    return parseGitPatch(s.patch);
  });

  async function load(current: GitReviewEntry, again: boolean): Promise<void> {
    const d = daemon();
    if (!d || d.server.id !== review.serverId) return;
    const mine = ++epoch;
    if (!again) {
      reader.set({ kind: "loading" });
      sectionIndex.set(0);
    }
    cursor.set(null);
    try {
      const payload = await getGitPatch(d, review.target, current.file.path);
      if (mine !== epoch) return;
      const plan = planGitDiffReview(payload, current.file, current.group);
      if (plan.kind === "sections") reader.set({ kind: "ready", sections: plan.sections, payload });
      else reader.set(plan);
    } catch (reason) {
      if (mine === epoch) reader.set({ kind: "error", message: reason instanceof Error ? reason.message : "Could not load this diff." });
    }
  }

  // Reads the patch when the file or the status changes; follows a file out of its group.
  effect(() => {
    const groups = review.groups();
    const snapshot = review.status();
    untracked(() => {
      if (!snapshot) {
        void review.refresh();
        return;
      }
      const current = entry();
      if (current) {
        const again = last?.path === current.file.path && last.group === current.group && reader().kind !== "error";
        last = { group: current.group, index: groups[current.group].findIndex((e) => e.key === current.key), path: current.file.path };
        void load(current, again);
        return;
      }
      const group = last ? groups[last.group] : [];
      if (!last || !group.length) return props.nav.pop();
      key.set(group[Math.min(last.index, group.length - 1)].key);
      const next = entry()!;
      last = { group: next.group, index: groups[next.group].findIndex((e) => e.key === next.key), path: next.file.path };
      void load(next, false);
    });
  });

  const order = () => review.order();
  const position = () => ({ index: order().findIndex((e) => e.key === key()), total: order().length });
  function navigate(direction: "previous" | "next") {
    const list = order();
    const index = list.findIndex((e) => e.key === key());
    const next = list[index + (direction === "next" ? 1 : -1)];
    if (!next) return;
    key.set(next.key);
    last = { group: next.group, index: review.groups()[next.group].findIndex((e) => e.key === next.key), path: next.file.path };
    void load(next, false);
  }

  function step(direction: "previous" | "next") {
    const p = parsed();
    if (!p?.hunks.length) return;
    const index = stepHunk(cursor() ?? -1, p.hunks.length, direction);
    const range = gitHunkSelection(p.hunks[index]);
    cursor.set(index);
    if (range) view?.setSelectedLines(toReader(range));
    void revealHunk(view, p.hunks[index]);
  }

  function clearHunk() {
    cursor.set(null);
    view?.setSelectedLines(null);
  }

  const selectedHunk = () => {
    const index = cursor();
    return index === null ? null : parsed()?.hunks[index] ?? null;
  };

  function stageToggle() {
    const current = entry();
    if (!current) return;
    const hunk = selectedHunk();
    if (hunk) void review.hunk(current.file.path, current.group, current.group === "staged" ? "unstage" : "stage", hunk.text);
    else void review.toggleStaged(current);
  }

  function discard() {
    const current = entry();
    if (!current) return;
    const hunk = selectedHunk();
    if (hunk) void review.hunk(current.file.path, current.group, "discard", hunk.text);
    else void review.discardFile(current);
  }

  async function menu() {
    const current = entry();
    if (!current) return;
    const answer = await AlertDialog.show({
      title: current.file.path,
      presentation: "actionSheet",
      actions: [
        { id: "refresh", label: "Refresh" },
        { id: "wrap", label: wrap() ? "Don't wrap lines" : "Wrap lines" },
        ...(current.file.status === "deleted" ? [] : [{ id: "open", label: "Open working file" }]),
        { id: "copy-path", label: "Copy path" },
        { id: "copy-diff", label: "Copy diff" },
        { id: "cancel", label: "Cancel", role: "cancel" as const },
      ],
    });
    const r = reader();
    if (answer.action === "refresh") void review.refresh();
    else if (answer.action === "wrap") wrap.set(!wrap());
    else if (answer.action === "open") props.nav.push({ name: "git-file", targetId: review.target.targetId, cwd: review.target.cwd, path: current.file.path });
    else if (answer.action === "copy-path") void copyText(current.file.path, "Path copied");
    else if (answer.action === "copy-diff" && r.kind === "ready" && r.payload) void copyText(gitDiffPatchText(r.payload, current.group), "Diff copied");
  }

  return (
    <NavPage title="Diff" testID="git-diff-page">
      <Current review={review} class="flex-1 bg-background">{() => (
          <View class="flex-1 bg-background">
            <ReaderHeader path={entry()?.file.path ?? last?.path ?? ""} position={position()} onNavigate={navigate} onMenu={() => void menu()} />
            <View class="px-4"><FlashLine /></View>
            <View class="flex-1">
              {kind() === "ready" ? (
                <View class="flex-1">
                  {(() => {
                    const r = untracked(reader);
                    return r.kind === "ready" && r.sections.length > 1 ? (
                      <View class="px-4 py-2">
                        <Segmented testID="git-diff-section" value={String(sectionIndex())} onChange={(next) => { sectionIndex.set(Number(next)); cursor.set(null); }} options={r.sections.map((s, i) => ({ key: String(i), label: s.label ?? `Part ${i + 1}` }))} />
                      </View>
                    ) : null;
                  })()}
                  <PatchReader section={section} parsed={parsed} path={entry()?.file.path ?? ""} wrap={wrap} cursor={{ get: cursor, set: (h) => cursor.set(h) }} onRef={(ref) => (view = ref)} />
                </View>
              ) : (
                <View class="flex-1 px-4"><ReaderState reader={reader()} working onRetry={() => { const e = entry(); if (e) void load(e, false); }} /></View>
              )}
            </View>
            <ActionBar
              group={entry()?.group ?? null}
              count={parsed()?.hunks.length ?? 0}
              index={cursor()}
              fileLabel={entry()?.file.binary ? "Binary" : entry()?.file.untracked ? "New file" : null}
              busy={review.isBusy("hunk") || (!!entry() && review.isBusy(entry()!.key))}
              onStep={step}
              onClearHunk={clearHunk}
              onStageToggle={stageToggle}
              onDiscard={discard}
            />
          </View>
      )}</Current>
    </NavPage>
  );
}

/**
 * The reader's one action row. With a hunk selected the actions apply to it;
 * with none, to the whole file. Staged diffs offer Unstage; changes offer
 * Stage and Discard. A commit's diff (no group) only steps.
 */
function ActionBar(props: {
  group: GitReviewGroup | null;
  count: number;
  index: number | null;
  fileLabel: string | null;
  busy: boolean;
  onStep: (direction: "previous" | "next") => void;
  onClearHunk: () => void;
  onStageToggle: () => void;
  onDiscard: () => void;
}) {
  return (
    <View testID="git-diff-actions" class="min-h-[52px] flex-row items-center gap-1 border-t border-border bg-background pb-2 pl-1.5 pr-3 pt-1">
      {(() => {
        const hunkMode = props.index !== null;
        const unit = hunkMode ? "hunk" : "file";
        const label = hunkMode ? `${props.index! + 1}/${props.count}` : props.fileLabel ?? (props.count ? `${props.count} ${props.count === 1 ? "hunk" : "hunks"}` : "");
        return (
          <>
            {hunkMode ? <IconButton testID="git-hunk-clear" icon="close" label="Select the whole file" onPress={props.onClearHunk} /> : null}
            {props.count > 0 ? (
              <>
                <IconButton testID="git-hunk-previous" icon="chevron-up" label="Previous hunk" onPress={() => props.onStep("previous")} />
                <Text testID="git-hunk-label" class="min-w-[34px] text-center text-sm text-muted-foreground" style={{ fontVariant: ["tabular-nums"] }}>{label}</Text>
                <IconButton testID="git-hunk-next" icon="chevron-down" label="Next hunk" onPress={() => props.onStep("next")} />
              </>
            ) : label ? (
              <Text class="px-2 text-sm text-muted-foreground">{label}</Text>
            ) : null}
            <View class="flex-1" />
            {props.group === "changes" ? <IconButton testID="git-discard" icon="trash" label={`Discard ${unit}`} disabled={props.busy} onPress={props.onDiscard} /> : null}
            {props.group ? (
              <TextButton testID="git-stage-toggle" filled={props.group === "changes"} busy={props.busy} label={`${props.group === "staged" ? "Unstage" : "Stage"} ${unit}`} onPress={props.onStageToggle} />
            ) : null}
          </>
        );
      })()}
    </View>
  );
}

/** One file of a commit: its part of `git show`, read only. */
export function CommitFilePage(props: { review: GitReview; sha: string; path: string; nav: PageNav }) {
  const review = props.review;
  const path = signal(props.path);
  const cursor = signal<number | null>(null);
  const wrap = signal(false);
  let view: DiffViewRef | null = null;
  if (!review.commits()[props.sha]?.value && !review.commits()[props.sha]?.loading) void review.loadCommit(props.sha);
  const patch = computed(() => {
    const detail = review.commits()[props.sha]?.value;
    if (!detail) return null;
    return splitGitPatchByFile(detail.patch, detail.files).get(path()) ?? "";
  });
  const section = computed<GitDiffSection | null>(() => {
    const p = patch();
    return p ? { id: "0-commit", patch: `${p}\n`, expandable: false } : null;
  });
  const parsed = computed(() => (section() ? parseGitPatch(section()!.patch) : null));
  const state = computed<Reader>(() => {
    const loaded = review.commits()[props.sha];
    if (loaded?.error && !loaded.value) return { kind: "error", message: loaded.error };
    const p = patch();
    if (p === null) return { kind: "loading" };
    return p.trim() ? { kind: "ready", sections: [section()!], payload: null } : { kind: "empty" };
  });
  const kind = computed(() => state().kind);
  const files = () => review.commits()[props.sha]?.value?.files ?? [];
  const index = () => files().findIndex((f) => f.path === path());

  function step(direction: "previous" | "next") {
    const p = parsed();
    if (!p?.hunks.length) return;
    const next = stepHunk(cursor() ?? -1, p.hunks.length, direction);
    const range = gitHunkSelection(p.hunks[next]);
    cursor.set(next);
    if (range) view?.setSelectedLines(toReader(range));
    void revealHunk(view, p.hunks[next]);
  }

  async function menu() {
    const answer = await AlertDialog.show({
      title: path(),
      presentation: "actionSheet",
      actions: [
        { id: "wrap", label: wrap() ? "Don't wrap lines" : "Wrap lines" },
        { id: "copy-path", label: "Copy path" },
        { id: "copy-diff", label: "Copy diff" },
        { id: "cancel", label: "Cancel", role: "cancel" },
      ],
    });
    if (answer.action === "wrap") wrap.set(!wrap());
    else if (answer.action === "copy-path") void copyText(path(), "Path copied");
    else if (answer.action === "copy-diff" && patch()) void copyText(`${patch()}\n`, "Diff copied");
  }

  return (
    <NavPage title={props.sha.slice(0, 7)} testID="git-commit-file-page">
      <Current review={review} class="flex-1 bg-background">{() => (
          <View class="flex-1 bg-background">
            <ReaderHeader
              path={path()}
              position={{ index: index(), total: files().length }}
              onNavigate={(direction) => {
                const next = files()[index() + (direction === "next" ? 1 : -1)];
                if (!next) return;
                cursor.set(null);
                path.set(next.path);
              }}
              onMenu={() => void menu()}
            />
            <View class="flex-1">
              {kind() === "ready" ? (
                <PatchReader section={section} parsed={parsed} path={path()} wrap={wrap} cursor={{ get: cursor, set: (h) => cursor.set(h) }} onRef={(ref) => (view = ref)} />
              ) : (
                <View class="flex-1 px-4"><ReaderState reader={state()} working={false} onRetry={() => void review.loadCommit(props.sha)} /></View>
              )}
            </View>
            <ActionBar group={null} count={parsed()?.hunks.length ?? 0} index={cursor()} fileLabel={null} busy={false} onStep={step} onClearHunk={() => { cursor.set(null); view?.setSelectedLines(null); }} onStageToggle={() => {}} onDiscard={() => {}} />
          </View>
      )}</Current>
    </NavPage>
  );
}

