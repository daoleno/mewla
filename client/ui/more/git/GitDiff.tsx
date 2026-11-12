import { computed, effect, signal, Text, untracked, View, windowSize } from "nsh";
import type { Signal } from "nsh";
import { Button, DiffView, Input, type DiffFile, type DiffLineRange, type DiffViewRef } from "nsh/base";
import { Sheet } from "../../kit/Sheet";
import { NavPage } from "../../shell/NavPage";
import { gitHunkAtLine, gitHunkSelection, parseGitPatch, splitGitPatchByFile, type GitParsedPatch, type GitPatchHunk, type GitPatchSide } from "../../../core/git/gitDiffHunks";
import { canExpandGitDiffFile, formatGitDiffBytes, gitDiffContentScope, gitDiffExpansionSides, gitDiffPatchText, planGitDiffReview, type GitDiffExpansionSides, type GitDiffSection } from "../../../core/git/gitDiffReview";
import type { GitDiffPatchPayload } from "../../../core/git/gitDiff";
import { padGitDiffPatch, padGitDiffText } from "../../../core/git/gitDiffLinePad";
import { findGitPatchMatches, followGitReviewEntry, gitAskContext, gitFindLabel, GIT_CODE_TEXT, stepGitCodeText, stepGitFind, type GitFindMatch } from "../../../core/git/gitReader";
import { splitGitReviewPath, type GitReviewEntry, type GitReviewGroup } from "../../../core/git/gitReviewModel";
import { getGitFileContent, getGitPatch } from "../../../core/git/requests";
import { daemon } from "../daemon";
import { EmptyState, Field, PillButton, SwitchRow } from "../../kit/kit";
import { Segmented } from "../../kit/Segmented";
import type { PageNav } from "../routes";
import { Counts, Current, FlashLine, IconButton, TextButton } from "./parts";
import { copyDiff, copyText, say, type GitReview } from "./state";
import { showAlert } from "../../shell/alert";

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
  | { kind: "ready"; sections: GitDiffSection[]; payload: GitDiffPatchPayload | null; sides: GitDiffExpansionSides | null };

type Selection = { side: GitPatchSide; start: number; end: number };

// The reader's lines count from 0; the patch model numbers file lines from 1.
function toModel(range: DiffLineRange): Selection {
  return { side: range.side === "new" ? "additions" : "deletions", start: range.start + 1, end: range.end + 1 };
}

function toReader(selection: Selection): DiffLineRange {
  return { side: selection.side === "additions" ? "new" : "old", start: selection.start - 1, end: selection.end - 1 };
}

export function stepHunk(current: number, count: number, direction: "next" | "previous"): number {
  if (direction === "next") return current + 1 >= count ? 0 : current + 1;
  return current - 1 < 0 ? count - 1 : current - 1;
}

/** Scrolls the reader to the change that holds lines of the patch (the reader may cut a hunk finer). */
async function revealLines(view: DiffViewRef | null, hunk: Pick<GitPatchHunk, "oldStart" | "oldCount" | "newStart" | "newCount">): Promise<void> {
  if (!view) return;
  const changes = await view.hunks();
  const first = hunk.oldStart - 1;
  const firstNew = hunk.newStart - 1;
  const found = changes.find((change) => change.newStart + change.newLines >= firstNew && change.newStart <= firstNew + Math.max(hunk.newCount, 1))
    ?? changes.find((change) => change.oldStart + change.oldLines >= first && change.oldStart <= first + Math.max(hunk.oldCount, 1));
  if (found) view.goToHunk(found.index);
}

/** How the reader shows code: wrapped or not, the text size, file headers, and which bars are open. */
export interface ReaderDisplay {
  wrap: Signal<boolean>;
  fontSize: Signal<number>;
  headers: Signal<boolean>;
  optionsOpen: Signal<boolean>;
  findOpen: Signal<boolean>;
}

function readerDisplay(): ReaderDisplay {
  return { wrap: signal(true), fontSize: signal<number>(GIT_CODE_TEXT.initial), headers: signal(false), optionsOpen: signal(false), findOpen: signal(false) };
}

/** Find in the diff: the matching lines of the patch shown, stepped through one at a time. */
function createFind(parsed: () => GitParsedPatch | null, view: () => DiffViewRef | null) {
  const query = signal("");
  const index = signal(-1);
  const matches = computed<GitFindMatch[]>(() => findGitPatchMatches(parsed(), query()));
  return {
    query,
    index,
    matches,
    label: () => gitFindLabel(query(), matches().length, index()),
    set(next: string) {
      query.set(next);
      index.set(-1);
      view()?.setSelectedLines(null);
    },
    step(direction: "next" | "previous") {
      const list = matches();
      if (!list.length) return;
      const next = stepGitFind(index(), list.length, direction);
      index.set(next);
      const match = list[next]!;
      view()?.setSelectedLines(toReader({ side: match.side, start: match.line, end: match.line }));
      void revealLines(view(), match.side === "additions" ? { oldStart: 0, oldCount: 0, newStart: match.line, newCount: 1 } : { oldStart: match.line, oldCount: 1, newStart: 0, newCount: 0 });
    },
    clear() {
      query.set("");
      index.set(-1);
    },
  };
}

type Find = ReturnType<typeof createFind>;

function FindBar(props: { find: Find; onDone: () => void }) {
  const f = props.find;
  return (
    <View testID="git-find" class="flex-row items-center gap-1 border-b border-border bg-background px-2 py-1">
      <View class="min-w-0 flex-1 flex-row items-center gap-2 rounded-[14px] bg-card px-3">
        <Input
          testID="git-find-query"
          accessibilityLabel="Search this diff"
          placeholder="Find in diff"
          class="min-h-[44px] flex-1 py-2 text-body"
          value={f.query()}
          autoFocus
          autoCapitalize="none"
          autoCorrect={false}
          returnKeyType="search"
          onChange={({ value }) => f.set(value)}
          onSubmit={() => f.step("next")}
        />
        {f.label() ? <Text testID="git-find-count" android={{ accessibilityLiveRegion: "polite" }} class="text-sub text-muted-foreground" style={{ fontVariant: ["tabular-nums"] }}>{f.label()}</Text> : null}
      </View>
      <IconButton testID="git-find-previous" icon="chevron-up" label="Previous matching line" disabled={!f.matches().length} onPress={() => f.step("previous")} />
      <IconButton testID="git-find-next" icon="chevron-down" label="Next matching line" disabled={!f.matches().length} onPress={() => f.step("next")} />
      <IconButton testID="git-find-done" icon="close" label="Done finding" onPress={props.onDone} />
    </View>
  );
}

/** Wrap lines, the code text size, file headers. */
function DisplayOptions(props: { display: ReaderDisplay }) {
  const d = props.display;
  return (
    <View testID="git-display" class="border-b border-border bg-background px-4 pb-3 pt-2">
      <View class="flex-row items-center justify-between">
        <Text class="text-sub font-semibold text-muted-foreground" role="header">Display</Text>
        <Button testID="git-display-done" accessibilityLabel="Done with display options" onPress={() => d.optionsOpen.set(false)} class="px-2 py-1 active:opacity-60">
          <Text class="text-sub font-semibold">Done</Text>
        </Button>
      </View>
      <View class="mt-1.5 overflow-hidden rounded-(--radius-group) bg-card">
        <SwitchRow testID="git-display-wrap" title="Wrap lines" checked={d.wrap()} onChange={(checked) => d.wrap.set(checked)} />
        <View class="min-h-[52px] flex-row items-center gap-3 border-b border-border px-4 py-1">
          <Text class="flex-1 text-body">Text size</Text>
          <IconButton testID="git-display-smaller" icon="remove" label="Smaller code text" disabled={d.fontSize() <= GIT_CODE_TEXT.min} onPress={() => d.fontSize.set(stepGitCodeText(d.fontSize(), "smaller"))} />
          <Text testID="git-display-size" class="min-w-[28px] text-center text-body" style={{ fontVariant: ["tabular-nums"] }}>{String(d.fontSize())}</Text>
          <IconButton testID="git-display-larger" icon="add" label="Larger code text" disabled={d.fontSize() >= GIT_CODE_TEXT.max} onPress={() => d.fontSize.set(stepGitCodeText(d.fontSize(), "larger"))} />
        </View>
        <SwitchRow testID="git-display-headers" title="File headers" checked={d.headers()} onChange={(checked) => d.headers.set(checked)} />
      </View>
    </View>
  );
}

/** The one-row header above a file of the patch, when File headers is on: its path and counts; a press folds it. */
function FileHeader(props: { path: string; additions: number; deletions: number; binary?: boolean; onPress?: () => void }) {
  return (
    <View testID="git-file-header" role={props.onPress ? "button" : undefined} onPress={props.onPress} class="h-full flex-row items-center gap-2 border-b border-border bg-card px-4">
      <Text class="min-w-0 flex-1 font-mono text-code font-semibold line-clamp-1">{props.path}</Text>
      <Counts additions={props.additions} deletions={props.deletions} binary={props.binary} />
    </View>
  );
}

/** One patch in the reader, with hunk selection: the shared middle of the working and commit diffs. */
function PatchReader(props: {
  section: () => GitDiffSection | null;
  sides: () => GitDiffExpansionSides | null;
  path: string;
  counts?: { additions: number; deletions: number; binary?: boolean } | null;
  display: ReaderDisplay;
  onSelect: (selection: Selection | null) => void;
  onStep: (direction: "previous" | "next") => void;
  onRef: (ref: DiffViewRef) => void;
}) {
  let view: DiffViewRef | null = null;
  const d = props.display;
  return (
    <View
      class="mx-4 mb-2 flex-1 overflow-hidden rounded-(--radius-group) bg-card"
      accessibilityLabel="Diff"
      accessibilityActions={[{ name: "increment", label: "Next hunk" }, { name: "decrement", label: "Previous hunk" }]}
      onAccessibilityAction={(event) => props.onStep(event.actionName === "increment" ? "next" : "previous")}
    >
      {/* Two whole texts (collapsed context expands) have no file of their own: the app draws its header. */}
      {d.headers() && props.sides() ? <View class="h-8"><FileHeader path={props.path} additions={props.counts?.additions ?? 0} deletions={props.counts?.deletions ?? 0} binary={props.counts?.binary} /></View> : null}
      <DiffView
        testID="git-diff-view"
        ref={(ref) => {
          view = ref;
          props.onRef(ref);
        }}
        class="flex-1"
        // SPEC §15 Code review: added lines tinted `add` at 13 %, removed `del` at 11 %, line numbers `faint`, hunk headers soft mono.
        classes={DIFF_CLASSES}
        // Padded for NSH's Android line layout (R4#3, gitDiffLinePad.ts); the model keeps the text.
        patch={props.sides() ? null : padGitDiffPatch(props.section()?.patch ?? "")}
        oldValue={nullablePad(props.sides()?.oldContents)}
        newValue={nullablePad(props.sides()?.newContents)}
        language={props.path}
        layout={diffLayout(windowSize().width)}
        softWrap={d.wrap()}
        fontSize={d.fontSize()}
        lineHeight={Math.round(d.fontSize() * 1.6)}
        renderFileHeader={d.headers() ? (file: DiffFile) => <FileHeader path={file.path} additions={file.added} deletions={file.deleted} binary={file.binary} onPress={() => view?.toggleFile(file.path)} /> : undefined}
        onLineSelectionChange={({ range }) => props.onSelect(range ? toModel(range) : null)}
      />
    </View>
  );
}

const DIFF_CLASSES = {
  lineNumber: "text-faint",
  lineAdded: "bg-add/13",
  lineDeleted: "bg-del/11",
  wordAdded: "bg-add/25",
  wordDeleted: "bg-del/22",
  separator: "bg-background text-muted-foreground",
  fileHeader: "bg-card",
};

function nullablePad(text: string | null | undefined): string | null {
  return text == null ? null : padGitDiffText(text);
}

function ReaderHeader(props: {
  path: string;
  counts?: { additions: number; deletions: number; binary?: boolean } | null;
  position?: { index: number; total: number };
  onNavigate?: (direction: "previous" | "next") => void;
  onMenu: () => void;
}) {
  const parts = () => splitGitReviewPath(props.path);
  return (
    <View class="min-h-[60px] flex-row items-center gap-1 bg-background px-2 py-1">
      <View class="min-w-0 flex-1 px-2">
        <View class="flex-row items-center gap-2">
          <Text testID="git-diff-name" class="min-w-0 shrink font-mono text-body font-semibold line-clamp-1">{parts().name}</Text>
          {props.counts ? <Counts additions={props.counts.additions} deletions={props.counts.deletions} binary={props.counts.binary} /> : null}
        </View>
        {parts().directory ? <Text class="font-mono text-caption text-muted-foreground line-clamp-1">{parts().directory}</Text> : null}
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

/** The reader's menu: the bars, the working file, copying, and Refresh. */
async function readerMenu(input: {
  path: string;
  deleted: boolean;
  display: ReaderDisplay;
  onRefresh: () => void;
  onOpen: () => void;
  onCopyDiff: () => void;
}): Promise<void> {
  const d = input.display;
  const answer = await showAlert({
    title: input.path,
    presentation: "actionSheet",
    actions: [
      { id: "find", label: d.findOpen() ? "Hide find bar" : "Find in diff" },
      { id: "display", label: d.optionsOpen() ? "Hide display options" : "Display options" },
      { id: "open", label: "Open working file" },
      { id: "copy-path", label: "Copy path" },
      { id: "copy-diff", label: "Copy diff" },
      { id: "refresh", label: "Refresh" },
      { id: "cancel", label: "Cancel", role: "cancel" },
    ],
  });
  switch (answer.action) {
    case "find":
      d.findOpen.set(!d.findOpen());
      break;
    case "display":
      d.optionsOpen.set(!d.optionsOpen());
      break;
    case "open":
      if (input.deleted) say({ title: "Deleted from the working tree", detail: input.path, tone: "neutral" });
      else input.onOpen();
      break;
    case "copy-path":
      void copyText(input.path, "Path copied", input.path);
      break;
    case "copy-diff":
      input.onCopyDiff();
      break;
    case "refresh":
      input.onRefresh();
      break;
  }
}

/**
 * One changed file's diff in the working tree or the index, as a page. A hunk is
 * chosen by pressing its lines (or stepping with the arrows); the actions then apply
 * to it, else to the whole file. After any change the patch is read again, and when
 * the file leaves its group the reader moves to the entry now at its place.
 */
export function DiffPage(props: { review: GitReview; entry: string; nav: PageNav }) {
  return (
    <NavPage title="Diff" testID="git-diff-page" column="full">
      <Current review={props.review} class="flex-1 bg-background">{() => <DiffReader review={props.review} entry={props.entry} nav={props.nav} onEmpty={() => props.nav.pop()} />}</Current>
    </NavPage>
  );
}

/** The reader itself, on its own page or beside the list on a wide screen. `onEmpty`: nothing is left to read. */
export function DiffReader(props: { review: GitReview; entry: string; nav: PageNav; onEmpty: () => void; onShow?: (key: string) => void }) {
  const review = props.review;
  const key = signal(props.entry);
  const reader = signal<Reader>({ kind: "loading" });
  const sectionIndex = signal(0);
  const cursor = signal<number | null>(null);
  const selection = signal<Selection | null>(null);
  const askOpen = signal(false);
  const display = readerDisplay();
  let view: DiffViewRef | null = null;
  let epoch = 0;
  // The entry as last seen, so a file that left its group keeps its place in the whole order.
  let last = null as { group: GitReviewGroup; orderIndex: number; path: string } | null;

  const entry = () => review.entry(key());
  const kind = computed(() => reader().kind);
  const section = computed<GitDiffSection | null>(() => {
    const r = reader();
    return r.kind === "ready" ? r.sections[Math.min(sectionIndex(), r.sections.length - 1)] ?? null : null;
  });
  const sides = computed(() => {
    const r = reader();
    return r.kind === "ready" ? r.sides : null;
  });
  // Hunks for the actions: tracked text files only.
  const parsed = computed<GitParsedPatch | null>(() => {
    const s = section();
    const e = untracked(entry);
    if (!s || !e || e.file.untracked || e.file.binary || s.id.endsWith("untracked")) return null;
    return parseGitPatch(s.patch);
  });
  // Find reads every line shown, a new file's too.
  const findable = computed<GitParsedPatch | null>(() => (section() ? parseGitPatch(section()!.patch) : null));
  const find = createFind(findable, () => view);

  function remember(current: GitReviewEntry): void {
    last = { group: current.group, orderIndex: review.order().findIndex((e) => e.key === current.key), path: current.file.path };
    props.onShow?.(current.key);
  }

  async function load(current: GitReviewEntry, again: boolean): Promise<void> {
    const d = daemon();
    if (!d || d.server.id !== review.serverId) return;
    const mine = ++epoch;
    if (!again) {
      reader.set({ kind: "loading" });
      sectionIndex.set(0);
    }
    cursor.set(null);
    selection.set(null);
    try {
      const scope = gitDiffContentScope(current.file, current.group);
      const [payload, content] = await Promise.all([
        getGitPatch(d, review.target, current.file.path),
        // Both whole sides, when collapsed context can expand: asked with the patch, so the reader opens once.
        // They also carry the syntax colors: a bare patch was drawn plain (FINAL-VERIFY gitfiles D4).
        canExpandGitDiffFile(current.file) ? getGitFileContent(d, review.target, current.file.path, scope).catch(() => null) : Promise.resolve(null),
      ]);
      if (mine !== epoch) return;
      const plan = planGitDiffReview(payload, current.file, current.group);
      if (plan.kind === "sections") {
        const expandable = plan.sections.length === 1 && plan.sections[0]!.expandable;
        reader.set({ kind: "ready", sections: plan.sections, payload, sides: expandable && content ? gitDiffExpansionSides(content, current.file, scope) : null });
      } else reader.set(plan);
    } catch (reason) {
      if (mine === epoch) reader.set({ kind: "error", message: reason instanceof Error ? reason.message : "Could not load this diff." });
    }
  }

  // Reads the patch when the file or the status changes; follows a file out of its group.
  effect(() => {
    review.groups();
    const snapshot = review.status();
    untracked(() => {
      if (!snapshot) {
        void review.refresh();
        return;
      }
      const current = entry();
      if (current) {
        const again = last?.path === current.file.path && last.group === current.group && reader().kind !== "error";
        remember(current);
        void load(current, again);
        return;
      }
      const next = last ? followGitReviewEntry(review.order(), last.orderIndex) : null;
      if (!next) return props.onEmpty();
      key.set(next.key);
      remember(next);
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
    find.clear();
    remember(next);
    void load(next, false);
  }

  function step(direction: "previous" | "next") {
    const p = parsed();
    if (!p?.hunks.length) return;
    const index = stepHunk(cursor() ?? -1, p.hunks.length, direction);
    const range = gitHunkSelection(p.hunks[index]!);
    cursor.set(index);
    selection.set(range);
    if (range) view?.setSelectedLines(toReader(range));
    void revealLines(view, p.hunks[index]!);
  }

  function select(next: Selection | null) {
    const p = parsed();
    if (!next || !p) {
      cursor.set(null);
      selection.set(null);
      return;
    }
    const hunk = gitHunkAtLine(p, next.side, next.start);
    cursor.set(hunk?.index ?? null);
    selection.set(hunk ? next : null);
  }

  function clearHunk() {
    cursor.set(null);
    selection.set(null);
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

  const askContext = () => {
    const current = entry();
    return current ? gitAskContext({ path: current.file.path, group: current.group, hunk: selectedHunk(), selection: selection() }) : null;
  };

  function menu() {
    const current = entry();
    if (!current) return;
    void readerMenu({
      path: current.file.path,
      deleted: current.file.status === "deleted",
      display,
      onRefresh: () => void review.refresh(),
      onOpen: () => props.nav.push({ name: "git-file", targetId: review.target.targetId, cwd: review.target.cwd, path: current.file.path }),
      onCopyDiff: () => {
        const r = reader();
        void copyDiff(r.kind === "ready" && r.payload ? gitDiffPatchText(r.payload, current.group) : "");
      },
    });
  }

  return (
    <View class="flex-1 bg-background">
      <ReaderHeader path={entry()?.file.path ?? last?.path ?? ""} counts={entry() ? { additions: entry()!.additions, deletions: entry()!.deletions, binary: entry()!.file.binary } : null} position={position()} onNavigate={navigate} onMenu={menu} />
      {display.optionsOpen() ? <DisplayOptions display={display} /> : null}
      {display.findOpen() ? <FindBar find={find} onDone={() => { display.findOpen.set(false); find.clear(); }} /> : null}
      <View class="px-4"><FlashLine /></View>
      <View class="flex-1">
        {kind() === "ready" ? (
          <View class="flex-1">
            {(() => {
              const r = untracked(reader);
              return r.kind === "ready" && r.sections.length > 1 ? (
                <View class="px-4 py-2">
                  <Segmented testID="git-diff-section" value={String(sectionIndex())} onChange={(next) => { sectionIndex.set(Number(next)); clearHunk(); find.clear(); }} options={r.sections.map((s, i) => ({ key: String(i), label: s.label ?? `Part ${i + 1}` }))} />
                </View>
              ) : null;
            })()}
            <PatchReader section={section} sides={sides} path={entry()?.file.path ?? ""} counts={entry() ? { additions: entry()!.additions, deletions: entry()!.deletions } : null} display={display} onSelect={select} onStep={step} onRef={(ref) => (view = ref)} />
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
        canAsk={kind() === "ready" && review.canAskAgent()}
        askLabel={`Ask ${review.agentName()}`}
        onStep={step}
        onClearHunk={clearHunk}
        onStageToggle={stageToggle}
        onDiscard={discard}
        onAsk={() => askOpen.set(true)}
      />
      <AskSheet
        open={askOpen()}
        title={`Ask ${review.agentName()}`}
        context={askContext}
        onClose={() => askOpen.set(false)}
        onSend={(text, where) => {
          review.askAgent(text);
          askOpen.set(false);
          say({ title: "Sent to the agent", detail: where, tone: "success" });
        }}
      />
    </View>
  );
}

/** A comment on a hunk, lines or the file, sent to the Session's agent with where it is and what it quotes. */
function AskSheet(props: { open: boolean; title: string; context: () => ReturnType<typeof gitAskContext> | null; onClose: () => void; onSend: (text: string, where: string) => void }) {
  return (
    <Sheet open={() => props.open} onClose={props.onClose} title={props.title}>
      <AskForm context={props.context} onSend={props.onSend} />
    </Sheet>
  );
}

function AskForm(props: { context: () => ReturnType<typeof gitAskContext> | null; onSend: (text: string, where: string) => void }) {
  const comment = signal("");
  // What it is about when it opened: a later selection does not change a comment being written.
  const context = untracked(props.context);
  return (
    <View testID="ask-sheet" class="gap-2 px-4">
      <Text testID="ask-where" class="px-1 font-mono text-caption text-muted-foreground line-clamp-1">{context?.where ?? ""}</Text>
      {context?.quote.length ? (
        <View class="rounded-(--radius-control) bg-sheet-surface px-3 py-2">
          {context.quote.map((line) => <Text class="font-mono text-code line-clamp-1">{line}</Text>)}
        </View>
      ) : null}
      <View class="-mx-4">
        <Field testID="ask-comment" accessibilityLabel="Comment for the agent" placeholder="What should change?" multiline autoFocus value={comment()} onChange={(value) => comment.set(value)} />
      </View>
      <PillButton testID="ask-send" title="Send to agent" filled disabled={!comment().trim() || !context} onPress={() => context && comment().trim() && props.onSend(context.build(comment()), context.where)} />
    </View>
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
  canAsk?: boolean;
  askLabel?: string;
  onStep: (direction: "previous" | "next") => void;
  onClearHunk: () => void;
  onStageToggle: () => void;
  onDiscard: () => void;
  onAsk?: () => void;
}) {
  return (
    <View testID="git-diff-actions" class="min-h-[52px] flex-row flex-wrap items-center gap-1.5 bg-background pb-3 pl-1.5 pr-4 pt-1">
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
                <Text testID="git-hunk-label" android={{ accessibilityLiveRegion: "polite" }} class="min-w-[34px] text-center text-sub text-muted-foreground" style={{ fontVariant: ["tabular-nums"] }}>{label}</Text>
                <IconButton testID="git-hunk-next" icon="chevron-down" label="Next hunk" onPress={() => props.onStep("next")} />
              </>
            ) : label ? (
              <Text class="px-2 text-sub text-muted-foreground">{label}</Text>
            ) : null}
            <View class="flex-1" />
            {/* Small ghost buttons (SPEC §15 Code review): Ask {agent}, Discard (it confirms), Stage or Unstage. */}
            {props.canAsk && props.group ? <TextButton testID="git-ask" label={props.askLabel ?? "Ask"} accessibilityLabel={`${props.askLabel ?? "Ask the agent"} about this ${unit}`} onPress={() => props.onAsk?.()} /> : null}
            {props.group === "changes" ? <TextButton testID="git-discard" label="Discard" accessibilityLabel={`Discard ${unit}`} disabled={props.busy} onPress={props.onDiscard} /> : null}
            {props.group ? (
              <TextButton testID="git-stage-toggle" busy={props.busy} label={`${props.group === "staged" ? "Unstage" : "Stage"} ${unit}`} onPress={props.onStageToggle} />
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
  const display = readerDisplay();
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
  const find = createFind(parsed, () => view);
  const state = computed<Reader>(() => {
    const loaded = review.commits()[props.sha];
    if (loaded?.error && !loaded.value) return { kind: "error", message: loaded.error };
    const p = patch();
    if (p === null) return { kind: "loading" };
    return p.trim() ? { kind: "ready", sections: [section()!], payload: null, sides: null } : { kind: "empty" };
  });
  const kind = computed(() => state().kind);
  const files = () => review.commits()[props.sha]?.value?.files ?? [];
  const index = () => files().findIndex((f) => f.path === path());
  const file = () => files()[index()];

  function step(direction: "previous" | "next") {
    const p = parsed();
    if (!p?.hunks.length) return;
    const next = stepHunk(cursor() ?? -1, p.hunks.length, direction);
    const range = gitHunkSelection(p.hunks[next]!);
    cursor.set(next);
    if (range) view?.setSelectedLines(toReader(range));
    void revealLines(view, p.hunks[next]!);
  }

  function menu() {
    void readerMenu({
      path: path(),
      deleted: file()?.status === "deleted",
      display,
      onRefresh: () => void review.loadCommit(props.sha),
      onOpen: () => props.nav.push({ name: "git-file", targetId: review.target.targetId, cwd: review.target.cwd, path: path() }),
      onCopyDiff: () => void copyDiff(patch() ? `${patch()}\n` : ""),
    });
  }

  return (
    <NavPage title={props.sha.slice(0, 7)} testID="git-commit-file-page" column="full">
      <Current review={review} class="flex-1 bg-background">{() => (
          <View class="flex-1 bg-background">
            <ReaderHeader
              path={path()}
              counts={file() ? { additions: file()!.additions, deletions: file()!.deletions, binary: file()!.binary } : null}
              position={{ index: index(), total: files().length }}
              onNavigate={(direction) => {
                const next = files()[index() + (direction === "next" ? 1 : -1)];
                if (!next) return;
                cursor.set(null);
                find.clear();
                path.set(next.path);
              }}
              onMenu={menu}
            />
            {display.optionsOpen() ? <DisplayOptions display={display} /> : null}
            {display.findOpen() ? <FindBar find={find} onDone={() => { display.findOpen.set(false); find.clear(); }} /> : null}
            <View class="px-4"><FlashLine /></View>
            <View class="flex-1">
              {kind() === "ready" ? (
                <PatchReader
                  section={section}
                  sides={() => null}
                  path={path()}
                  display={display}
                  onSelect={(range) => {
                    const p = parsed();
                    cursor.set(range && p ? gitHunkAtLine(p, range.side, range.start)?.index ?? null : null);
                  }}
                  onStep={step}
                  onRef={(ref) => (view = ref)}
                />
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
