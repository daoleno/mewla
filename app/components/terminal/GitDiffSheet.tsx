import React from "react";
import { Modal, StatusBar, StyleSheet, Text, useWindowDimensions, View } from "react-native";
import { SafeAreaView, useSafeAreaInsets } from "react-native-safe-area-context";
import { isLightTerminalTheme, type TerminalThemePalette } from "../../constants/terminalThemes";
import { TypeScale, UiTextMetrics } from "../../constants/tokens";
import type {
  GitCommitDetail,
  GitCommitFile,
  GitCommitSummary,
  GitDiffFileContentPayload,
  GitDiffPatchPayload,
  GitDiffStatusSnapshot,
  GitDiscardUndo,
  GitRepoFileContentPayload,
  GitRepoPathsPayload,
} from "../../services/gitDiff";
import { pathBasename } from "../../services/pathDisplay";
import { ActionMenu, type ActionMenuItem } from "../ui/ActionMenu";
import { EmptyState } from "../ui/EmptyState";
import { InlineNotice } from "../ui/InlineNotice";
import { MobileSingleLineInput } from "../ui/MobileSingleLineInput";
import { ToastProvider, useToast } from "../ui/Toast";
import { confirmDestructive } from "../ui/confirmDestructive";
import { Icon } from "../icons/Icon";
import { GitDiffRepoBrowser } from "./GitDiffRepoBrowser";
import { GitReviewDiff, type GitReviewSource } from "./GitReviewDiff";
import { GitReviewCommitFiles, GitReviewHistory } from "./GitReviewHistory";
import { GitReviewList } from "./GitReviewList";
import { GitIconButton, GitTabs, GitTextButton } from "./GitReviewParts";
import { GitBranchSheet, GitCommitSheet } from "./GitReviewSheets";
import { useGitDiffClipboard } from "./gitDiffClipboard";
import { resolveGitDiffBack, type GitDiffViewMode } from "./gitDiffNavigation";
import { splitGitPatchByFile } from "./gitDiffHunks";
import { useGitDiffChrome } from "./gitDiffSurface";
import {
  adjacentGitReviewEntry,
  buildGitReviewGroups,
  gitReviewOrder,
  gitReviewPaths,
  gitReviewSummary,
  gitReviewSync,
  gitSyncAction,
  type GitReviewEntry,
  type GitReviewGroup,
} from "./gitReviewModel";
import type { GitReviewActions } from "./gitReviewActions";

const WIDE_BREAKPOINT = 720;

type Tab = "changes" | "history";

// The list/tree choice outlives the sheet for the rest of the app session.
let rememberedTree = false;

interface GitDiffSheetProps {
  visible: boolean;
  theme: TerminalThemePalette;
  snapshot: GitDiffStatusSnapshot | null;
  loading: boolean;
  error: string | null;
  loadPatch(path: string): Promise<GitDiffPatchPayload>;
  loadFileContent(path: string): Promise<GitDiffFileContentPayload>;
  actions: GitReviewActions;
  refreshKey: number;
  ownerKey: string;
  repoPaths: GitRepoPathsPayload | null;
  repoPathsLoading: boolean;
  repoPathsError: string | null;
  repoFilePath: string | null;
  repoFileLoadingPath: string | null;
  repoFileError: string | null;
  repoFileByPath: Record<string, GitRepoFileContentPayload | undefined>;
  onClose(): void;
  onRefresh(): void;
  onLoadRepoPaths(): void;
  onOpenRepoFile(path: string): void;
  onCloseRepoFile(): void;
}

export function GitDiffSheet(props: GitDiffSheetProps) {
  // Android hardware back resolves through the body's back stack. The toast
  // host lives inside the Modal because the app-level host renders beneath it.
  const backRef = React.useRef<() => void>(props.onClose);
  return (
    <Modal visible={props.visible} animationType="slide" onRequestClose={() => backRef.current()} statusBarTranslucent>
      <ToastProvider>
        <GitDiffSheetBody {...props} backRef={backRef} />
      </ToastProvider>
    </Modal>
  );
}

function GitDiffSheetBody({
  visible,
  theme,
  snapshot,
  loading,
  error,
  loadPatch,
  loadFileContent,
  actions,
  refreshKey,
  ownerKey,
  repoPaths,
  repoPathsLoading,
  repoPathsError,
  repoFilePath,
  repoFileLoadingPath,
  repoFileError,
  repoFileByPath,
  onClose,
  onRefresh,
  onLoadRepoPaths,
  onOpenRepoFile,
  onCloseRepoFile,
  backRef,
}: GitDiffSheetProps & { backRef: React.MutableRefObject<() => void> }) {
  const chrome = useGitDiffChrome(theme);
  const toast = useToast();
  const { width } = useWindowDimensions();
  const insets = useSafeAreaInsets();
  const wide = width >= WIDE_BREAKPOINT;
  const bottomInset = Math.max(insets.bottom, 0);
  const { copyPath, copyPatch, copyText } = useGitDiffClipboard(loadPatch);

  const [tab, setTab] = React.useState<Tab>("changes");
  const [view, setView] = React.useState<GitDiffViewMode>("changes");
  const [fileOrigin, setFileOrigin] = React.useState<GitDiffViewMode>("files");
  const [openKey, setOpenKey] = React.useState<string | null>(null);
  const [tree, setTree] = React.useState(rememberedTree);
  const [searchOpen, setSearchOpen] = React.useState(false);
  const [query, setQuery] = React.useState("");
  const [collapsed, setCollapsed] = React.useState<ReadonlySet<string>>(new Set());
  const [busyKeys, setBusyKeys] = React.useState<ReadonlySet<string>>(new Set());
  const [commitOpen, setCommitOpen] = React.useState(false);
  const [branchesOpen, setBranchesOpen] = React.useState(false);
  // The last discard, kept for the menu's Undo after its toast is gone.
  const [lastDiscard, setLastDiscard] = React.useState<(() => Promise<void>) | null>(null);
  const [menu, setMenu] = React.useState<{ title?: string; items: ActionMenuItem[] } | null>(null);
  const [menuVisible, setMenuVisible] = React.useState(false);
  const [commit, setCommit] = React.useState<{
    summary: GitCommitSummary;
    detail: GitCommitDetail | null;
    error: string | null;
    loading: boolean;
    filePath: string | null;
  } | null>(null);
  const [historyKey, setHistoryKey] = React.useState(0);

  const files = React.useMemo(() => snapshot?.files ?? [], [snapshot?.files]);
  const groups = React.useMemo(() => buildGitReviewGroups(files), [files]);
  const order = React.useMemo(() => gitReviewOrder(groups), [groups]);
  const openEntry = React.useMemo(() => order.find((entry) => entry.key === openKey) ?? null, [order, openKey]);
  const orderIndex = openEntry ? order.indexOf(openEntry) : -1;
  const lastIndex = React.useRef(-1);
  if (orderIndex >= 0) lastIndex.current = orderIndex;

  const repoFileContent = repoFilePath ? repoFileByPath[repoFilePath] : undefined;
  const repoFileLoading = Boolean(repoFilePath && repoFileLoadingPath === repoFilePath && !repoFileContent);
  const repoTitle = snapshot?.repo_name || pathBasename((snapshot?.repo_root ?? "").replace(/\/+$/, "")) || "repo";
  const summary = gitReviewSummary(snapshot);
  const sync = gitSyncAction(snapshot);

  const resetTransientState = React.useCallback(() => {
    setTab("changes");
    setView("changes");
    setOpenKey(null);
    setSearchOpen(false);
    setQuery("");
    setCommitOpen(false);
    setBranchesOpen(false);
    setMenuVisible(false);
    setLastDiscard(null);
    setCommit(null);
    setBusyKeys(new Set());
  }, []);
  React.useEffect(() => {
    if (!visible) resetTransientState();
  }, [resetTransientState, visible]);
  React.useEffect(() => {
    resetTransientState();
  }, [ownerKey, resetTransientState]);

  // A file that left its group (staged, committed, discarded, or changed by the
  // agent meanwhile) moves the reader on to the file now at the same place.
  React.useEffect(() => {
    if (!openKey || openEntry || !snapshot?.available || loading) return;
    const next = order[Math.min(Math.max(lastIndex.current, 0), order.length - 1)];
    setOpenKey(next ? next.key : null);
    if (!next) lastIndex.current = -1;
  }, [openKey, openEntry, snapshot?.available, loading, order]);

  // The remote moves on its own (the agent or a teammate pushes), so opening the
  // review fetches once for a true ahead and behind. A failure is not news here.
  const actionsRef = React.useRef(actions);
  actionsRef.current = actions;
  const available = Boolean(snapshot?.available);
  React.useEffect(() => {
    if (visible && available) actionsRef.current.fetch().catch(() => {});
  }, [visible, available, ownerKey]);

  const fail = React.useCallback(
    (title: string, reason: unknown) => {
      toast.show({ tone: "error", title, detail: reason instanceof Error ? reason.message : undefined });
    },
    [toast],
  );

  const withBusy = React.useCallback(async (key: string, run: () => Promise<void>, failure: string) => {
    setBusyKeys((current) => new Set(current).add(key));
    try {
      await run();
    } catch (reason) {
      fail(failure, reason);
    } finally {
      setBusyKeys((current) => {
        const next = new Set(current);
        next.delete(key);
        return next;
      });
    }
  }, [fail]);

  const toggleStaged = React.useCallback(
    (entry: GitReviewEntry) =>
      withBusy(
        entry.key,
        () => (entry.group === "staged" ? actions.unstage(gitReviewPaths([entry])) : actions.stage(gitReviewPaths([entry]))),
        entry.group === "staged" ? "Couldn't unstage" : "Couldn't stage",
      ),
    [actions, withBusy],
  );

  const bulk = React.useCallback(
    (group: GitReviewGroup) => {
      const paths = gitReviewPaths(groups[group]);
      if (!paths.length) return;
      void withBusy(`bulk:${group}`, () => (group === "staged" ? actions.unstage(paths) : actions.stage(paths)), group === "staged" ? "Couldn't unstage" : "Couldn't stage");
    },
    [actions, groups, withBusy],
  );

  const offerUndo = React.useCallback(
    (title: string, undo: GitDiscardUndo[]) => {
      if (!undo.length) return;
      const run = () => actions.undoDiscard(undo);
      setLastDiscard(() => run);
      toast.show({
        title,
        action: {
          label: "Undo",
          onPress: () => {
            setLastDiscard(null);
            run().catch((reason) => fail("Couldn't undo the discard", reason));
          },
        },
      });
    },
    [actions, fail, toast],
  );

  const discardEntries = React.useCallback(
    (entries: GitReviewEntry[], title: string, message: string) => {
      confirmDestructive({
        title,
        message,
        confirmLabel: "Discard",
        onConfirm: async () => {
          try {
            const undo = await actions.discard(gitReviewPaths(entries), "working");
            offerUndo(entries.length === 1 ? "Changes discarded" : `${entries.length} files discarded`, undo);
          } catch (reason) {
            fail("Couldn't discard", reason);
          }
        },
      });
    },
    [actions, fail, offerUndo],
  );

  const discardFile = React.useCallback(
    (entry: GitReviewEntry) => {
      const name = pathBasename(entry.file.path);
      discardEntries(
        [entry],
        `Discard changes to ${name}?`,
        entry.file.untracked ? `${name} is new and will be deleted.` : "Your edits to the file will be lost.",
      );
    },
    [discardEntries],
  );

  const discardAll = React.useCallback(() => {
    const entries = groups.changes;
    if (!entries.length) return;
    const untracked = entries.filter((entry) => entry.file.untracked).length;
    discardEntries(
      entries,
      `Discard ${entries.length} ${entries.length === 1 ? "file" : "files"}?`,
      untracked ? `Edits are reverted and ${untracked} new ${untracked === 1 ? "file is" : "files are"} deleted. Staged changes stay.` : "Edits are reverted. Staged changes stay.",
    );
  }, [discardEntries, groups.changes]);

  const submitCommit = React.useCallback(
    async (message: string, push: boolean) => {
      const result = await actions.commit(message);
      setCommitOpen(false);
      const label = result.sha ? result.sha.slice(0, 7) : "Committed";
      if (!push) {
        toast.show({ tone: "success", title: "Committed", detail: `${label} ${result.subject}`.trim() });
        return;
      }
      try {
        await actions.push();
        toast.show({ tone: "success", title: "Committed and pushed", detail: `${label} ${result.subject}`.trim() });
      } catch (reason) {
        fail(`Committed ${label}, but the push failed`, reason);
      }
    },
    [actions, fail, toast],
  );

  const runSync = React.useCallback(
    (kind: "push" | "pull") =>
      withBusy(
        "sync",
        async () => {
          const message = kind === "push" ? await actions.push() : await actions.pull();
          toast.show({ tone: "success", title: kind === "push" ? "Pushed" : "Pulled", detail: message || undefined });
          setHistoryKey((value) => value + 1);
        },
        kind === "push" ? "Couldn't push" : "Couldn't pull",
      ),
    [actions, toast, withBusy],
  );

  const switchBranch = React.useCallback(
    async (branch: string, create: boolean) => {
      await actions.switchBranch(branch, create);
      setOpenKey(null);
      setCommit(null);
      setHistoryKey((value) => value + 1);
      toast.show({ tone: "success", title: create ? "Branch created" : "Switched branch", detail: branch });
    },
    [actions, toast],
  );

  const openCommit = React.useCallback(
    (summaryRow: GitCommitSummary) => {
      setCommit({ summary: summaryRow, detail: null, error: null, loading: true, filePath: null });
      actions.show(summaryRow.sha).then(
        (detail) => setCommit((current) => (current?.summary.sha === summaryRow.sha ? { ...current, detail, loading: false } : current)),
        (reason) =>
          setCommit((current) =>
            current?.summary.sha === summaryRow.sha
              ? { ...current, loading: false, error: reason instanceof Error ? reason.message : "Could not load the commit." }
              : current,
          ),
      );
    },
    [actions],
  );

  const commitPatches = React.useMemo(
    () => (commit?.detail ? splitGitPatchByFile(commit.detail.patch, commit.detail.files) : null),
    [commit?.detail],
  );
  const commitFile: GitCommitFile | null = commit?.detail?.files.find((file) => file.path === commit.filePath) ?? null;

  const source: GitReviewSource | null = React.useMemo(() => {
    if (tab === "changes") return openEntry ? { kind: "working", entry: openEntry } : null;
    if (commit && commitFile && commitPatches) {
      return { kind: "commit", sha: commit.summary.sha, file: commitFile, patch: commitPatches.get(commitFile.path) ?? "" };
    }
    return null;
  }, [tab, openEntry, commit, commitFile, commitPatches]);

  const position = React.useMemo(() => {
    if (tab === "changes") return openEntry ? { index: orderIndex, total: order.length } : null;
    if (commit?.detail && commitFile) return { index: commit.detail.files.indexOf(commitFile), total: commit.detail.files.length };
    return null;
  }, [tab, openEntry, orderIndex, order.length, commit?.detail, commitFile]);

  const navigate = React.useCallback(
    (direction: "next" | "previous") => {
      if (tab === "changes") {
        const next = adjacentGitReviewEntry(order, openKey ?? "", direction);
        if (next) setOpenKey(next.key);
      } else if (commit?.detail && commitFile) {
        const index = commit.detail.files.indexOf(commitFile) + (direction === "next" ? 1 : -1);
        const next = commit.detail.files[index];
        if (next) setCommit({ ...commit, filePath: next.path });
      }
    },
    [commit, commitFile, openKey, order, tab],
  );

  const diffOpen = source !== null;
  // On the list level, Back leaves a commit for the history, or the diff for the list.
  const commitBackToList = tab === "history" && commit !== null && (wide || !diffOpen);
  const leadingBack = commitBackToList || (!wide && diffOpen);
  const handleBack = React.useCallback(() => {
    const action = resolveGitDiffBack({
      view,
      hasOpenDiff: diffOpen,
      hasOpenCommit: tab === "history" && commit !== null,
      hasBrowserFile: Boolean(repoFilePath),
      fileOrigin,
      searchOpen,
    });
    switch (action) {
      case "close":
        onClose();
        break;
      case "close-search":
        setSearchOpen(false);
        setQuery("");
        break;
      case "close-diff":
        if (tab === "changes") setOpenKey(null);
        else setCommit((current) => (current ? { ...current, filePath: null } : current));
        break;
      case "close-commit":
        setCommit(null);
        break;
      case "close-browser-file-to-reader":
        onCloseRepoFile();
        setView("changes");
        break;
      case "close-browser-file-to-browser":
        onCloseRepoFile();
        break;
      case "browser-to-changes":
        setView("changes");
        break;
    }
  }, [commit, diffOpen, fileOrigin, onClose, onCloseRepoFile, repoFilePath, searchOpen, tab, view]);
  backRef.current = handleBack;

  const handleBrowseFiles = React.useCallback(() => {
    setView("files");
    if (!repoPaths) onLoadRepoPaths();
  }, [onLoadRepoPaths, repoPaths]);
  const handleOpenWorkingFile = React.useCallback(
    (path: string) => {
      setFileOrigin("changes");
      setView("files");
      onOpenRepoFile(path);
    },
    [onOpenRepoFile],
  );
  const handleRefresh = React.useCallback(() => {
    onRefresh();
    setHistoryKey((value) => value + 1);
    if (view === "files") {
      if (repoFilePath) onOpenRepoFile(repoFilePath);
      else onLoadRepoPaths();
    }
  }, [onLoadRepoPaths, onOpenRepoFile, onRefresh, repoFilePath, view]);

  const openMenu = React.useCallback((title: string | undefined, items: ActionMenuItem[]) => {
    setMenu({ title, items });
    setMenuVisible(true);
  }, []);

  const entryMenu = React.useCallback(
    (entry: GitReviewEntry) => {
      const items: ActionMenuItem[] = [
        { key: "view", label: "View diff", icon: "git-compare", onPress: () => setOpenKey(entry.key) },
        {
          key: "stage",
          label: entry.group === "staged" ? "Unstage" : "Stage",
          icon: entry.group === "staged" ? "remove-circle" : "add-circle",
          onPress: () => void toggleStaged(entry),
        },
      ];
      if (entry.group === "changes") {
        items.push({ key: "discard", label: "Discard changes", icon: "trash", destructive: true, onPress: () => discardFile(entry) });
      }
      items.push(
        { key: "open", label: "Open working file", icon: "document-text", disabled: entry.file.status === "deleted", onPress: () => handleOpenWorkingFile(entry.file.path) },
        { key: "copy-path", label: "Copy path", icon: "link", onPress: () => void copyPath(entry.file.path) },
      );
      openMenu(entry.file.path, items);
    },
    [copyPath, discardFile, handleOpenWorkingFile, openMenu, toggleStaged],
  );

  const overflow = React.useCallback(() => {
    const items: ActionMenuItem[] = [{ key: "refresh", label: "Refresh", icon: "refresh", disabled: loading, onPress: handleRefresh }];
    if (view === "changes" && snapshot?.available) {
      items.push({ key: "browse", label: "Browse files", icon: "folder-open", onPress: handleBrowseFiles });
      if (snapshot.upstream) {
        items.push(
          { key: "fetch", label: "Fetch", icon: "cloud-download", onPress: () => void withBusy("sync", async () => { await actions.fetch(); }, "Couldn't fetch") },
          { key: "pull", label: "Pull", icon: "arrow-down", onPress: () => void runSync("pull") },
          { key: "push", label: "Push", icon: "arrow-up", onPress: () => void runSync("push") },
        );
      }
      if (lastDiscard) {
        items.push({
          key: "undo",
          label: "Undo last discard",
          icon: "undo",
          onPress: () => {
            const undo = lastDiscard;
            setLastDiscard(null);
            undo().catch((reason) => fail("Couldn't undo the discard", reason));
          },
        });
      }
      if (groups.changes.length) {
        items.push({ key: "discard-all", label: "Discard all changes", icon: "trash", destructive: true, onPress: discardAll });
      }
      if (snapshot.repo_root) {
        const root = snapshot.repo_root;
        items.push({ key: "copy-root", label: "Copy repository path", icon: "link", onPress: () => void copyText(root, "Path copied", root) });
      }
      if (snapshot.branch) {
        const branch = snapshot.branch;
        items.push({ key: "copy-branch", label: "Copy branch name", icon: "git-branch", onPress: () => void copyText(branch, "Branch copied", branch) });
      }
    }
    openMenu(snapshot?.branch ? `${repoTitle} · ${snapshot.branch}` : repoTitle, items);
  }, [actions, copyText, discardAll, fail, groups.changes.length, handleBrowseFiles, handleRefresh, lastDiscard, loading, openMenu, repoTitle, runSync, snapshot, view, withBusy]);

  const statePad = [styles.contentPad, { paddingBottom: bottomInset + 16 }];
  const stagedCount = groups.staged.length;
  const changesCount = groups.changes.length;
  const clean = Boolean(snapshot?.clean) && files.length === 0;

  const commitBar =
    tab === "changes" && snapshot?.available && (stagedCount > 0 || changesCount > 0) ? (
      <View style={[styles.commitBar, { backgroundColor: chrome.appBackground, borderTopColor: chrome.border, paddingBottom: bottomInset + 8 }]}>
        {stagedCount > 0 ? (
          <GitTextButton
            label={`Commit ${stagedCount} ${stagedCount === 1 ? "file" : "files"}`}
            tone="primary"
            chrome={chrome}
            onPress={() => setCommitOpen(true)}
            style={styles.commitButton}
          />
        ) : (
          <GitTextButton label={`Stage all ${changesCount}`} tone="tinted" chrome={chrome} busy={busyKeys.has("bulk:changes")} onPress={() => bulk("changes")} style={styles.commitButton} />
        )}
      </View>
    ) : null;

  const listHeader = (
    <View>
      <View style={styles.topBar}>
        <GitIconButton
          icon={leadingBack ? "chevron-left" : "close"}
          label={leadingBack ? "Back" : "Close Git review"}
          chrome={chrome}
          onPress={() => (leadingBack ? (commitBackToList ? setCommit(null) : handleBack()) : onClose())}
        />
        <GitTabs
          tabs={[
            { value: "changes", label: "Changes" },
            { value: "history", label: "History" },
          ]}
          value={tab}
          chrome={chrome}
          onChange={(next) => {
            setTab(next);
            setSearchOpen(false);
            setQuery("");
          }}
        />
        <View style={styles.flex} />
        {tab === "changes" ? (
          <>
            <GitIconButton icon="search" label="Search changed files" chrome={chrome} size={40} selected={searchOpen} onPress={() => (searchOpen ? (setSearchOpen(false), setQuery("")) : setSearchOpen(true))} />
            <GitIconButton
              icon={tree ? "list" : "git-network"}
              label={tree ? "Show as list" : "Show as folders"}
              chrome={chrome}
              size={40}
              onPress={() => {
                rememberedTree = !tree;
                setTree(!tree);
              }}
            />
          </>
        ) : null}
        <GitIconButton icon="more-vertical" label="More actions" chrome={chrome} size={40} onPress={overflow} />
      </View>
      {snapshot?.available ? (
        <View style={styles.strip}>
          <GitTextButton
            label={snapshot.detached ? `Detached ${snapshot.branch ?? ""}`.trim() : snapshot.branch || "No branch"}
            icon="git-branch"
            chrome={chrome}
            tone="tinted"
            accessibilityLabel={`Branch ${snapshot.branch ?? ""}. Switch branch`}
            onPress={() => setBranchesOpen(true)}
            style={styles.branch}
          />
          <Text numberOfLines={1} style={[styles.summary, { color: chrome.textSubtle }]} accessibilityLiveRegion="polite">
            {tab === "changes" ? summary : gitReviewSync(snapshot)}
          </Text>
          {sync ? <GitTextButton label={sync.label} chrome={chrome} tone="tinted" busy={busyKeys.has("sync")} onPress={() => void runSync(sync.kind)} /> : null}
          {snapshot.operation ? <Text style={[styles.summary, { color: chrome.danger }]}>{`${snapshot.operation} in progress`}</Text> : null}
        </View>
      ) : null}
      {searchOpen && tab === "changes" ? (
        <View style={[styles.search, { backgroundColor: chrome.surfaceMuted }]}>
          <Icon name="search" size={16} color={chrome.textSubtle} />
          <MobileSingleLineInput
            accessibilityLabel="Search changed files"
            placeholder="Filter files"
            value={query}
            onChangeText={setQuery}
            autoFocus
            autoCapitalize="none"
            autoCorrect={false}
            containerStyle={styles.searchInput}
            inputStyle={{ color: chrome.text }}
            placeholderTextColor={chrome.textSubtle}
          />
        </View>
      ) : null}
    </View>
  );

  const emptyChanges = clean ? (
    <EmptyState
      icon="check-circle"
      title="Working tree clean"
      detail={snapshot?.branch ?? null}
      action={{ label: "Browse files", icon: "folder-open", onPress: handleBrowseFiles }}
    />
  ) : (
    <EmptyState size="inline" title="No changes" />
  );

  const listBody =
    tab === "changes" ? (
      <GitReviewList
        groups={groups}
        tree={tree}
        query={query}
        collapsed={collapsed}
        selectedKey={openKey}
        busyKeys={busyKeys}
        theme={theme}
        chrome={chrome}
        bottomInset={commitBar ? 0 : bottomInset}
        refreshing={loading}
        onRefresh={handleRefresh}
        onOpen={(entry) => setOpenKey(entry.key)}
        onToggleStaged={(entry) => void toggleStaged(entry)}
        onMenu={entryMenu}
        onToggleCollapsed={(key) =>
          setCollapsed((current) => {
            const next = new Set(current);
            if (!next.delete(key)) next.add(key);
            return next;
          })
        }
        onBulk={bulk}
        empty={emptyChanges}
      />
    ) : commit ? (
      <GitReviewCommitFiles
        commit={commit.summary}
        detail={commit.detail}
        loading={commit.loading}
        error={commit.error}
        selectedPath={commit.filePath}
        theme={theme}
        chrome={chrome}
        bottomInset={bottomInset}
        onRetry={() => openCommit(commit.summary)}
        onOpen={(file) => setCommit({ ...commit, filePath: file.path })}
      />
    ) : (
      <GitReviewHistory actions={actions} refreshKey={historyKey + refreshKey} chrome={chrome} bottomInset={bottomInset} onOpen={openCommit} />
    );

  const reader = (
    <GitReviewDiff
      source={source}
      position={position}
      loadPatch={loadPatch}
      loadFileContent={loadFileContent}
      refreshKey={refreshKey}
      actions={actions}
      theme={theme}
      chrome={chrome}
      split={wide}
      bottomInset={bottomInset}
      onBack={wide ? undefined : handleBack}
      onNavigate={navigate}
      onStageFile={(entry) => void toggleStaged(entry)}
      onDiscardFile={discardFile}
      onHunkDiscarded={(undo) => setLastDiscard(() => undo)}
      onOpenFile={handleOpenWorkingFile}
      onCopyPath={(path) => void copyPath(path)}
      onCopyPatch={(path, src) => {
        if (src.kind === "working") void copyPatch(path, src.entry.group);
        else void copyText(src.patch, "Diff copied", path);
      }}
      onRefresh={handleRefresh}
    />
  );

  const filesHeader = (
    <View style={styles.topBar}>
      <GitIconButton icon="chevron-left" label={repoFilePath ? "Back" : "Changes"} chrome={chrome} onPress={handleBack} />
      <Text numberOfLines={1} ellipsizeMode="head" style={[styles.filesTitle, { color: chrome.text }]}>
        {repoFilePath ?? "Files"}
      </Text>
      <GitIconButton icon="more-vertical" label="More actions" chrome={chrome} size={40} onPress={() => openMenu(repoTitle, [{ key: "refresh", label: "Refresh", icon: "refresh", onPress: handleRefresh }])} />
    </View>
  );

  let content: React.ReactNode;
  if (error && !snapshot?.available) {
    content = (
      <View style={statePad}>
        <EmptyState tone="danger" icon="warning-fill" title="Couldn't load Git status" detail={error} action={{ label: "Retry", icon: "refresh", onPress: handleRefresh, loading }} />
      </View>
    );
  } else if (loading && !snapshot) {
    content = (
      <View style={statePad}>
        <EmptyState busy title="Inspecting repository" />
      </View>
    );
  } else if (!snapshot?.available) {
    content = (
      <View style={statePad}>
        <EmptyState
          icon="git-branch"
          title={snapshot?.reason === "no_cwd" ? "No working directory yet" : "Not a Git repository"}
          detail={snapshot?.reason === "no_cwd" ? "This terminal hasn't reported a folder yet." : "Move this terminal into a Git repository, then refresh."}
          action={{ label: "Refresh", icon: "refresh", onPress: handleRefresh, loading }}
        />
      </View>
    );
  } else if (view === "files") {
    content = (
      <View style={styles.body}>
        {filesHeader}
        <GitDiffRepoBrowser
          repoTitle={repoTitle}
          repoPaths={repoPaths}
          repoPathsLoading={repoPathsLoading}
          repoPathsError={repoPathsError}
          repoFilePath={repoFilePath}
          repoFileContent={repoFileContent}
          repoFileLoading={repoFileLoading}
          repoFileError={repoFileError}
          changedFiles={files}
          theme={theme}
          chrome={chrome}
          bottomInset={bottomInset}
          onRetryPaths={onLoadRepoPaths}
          onOpenRepoFile={(path) => {
            setFileOrigin("files");
            onOpenRepoFile(path);
          }}
          onLongPressEntry={(entry) =>
            openMenu(entry.path, [
              ...(entry.kind === "file"
                ? [{ key: "open", label: "Open file", icon: "document-text" as const, onPress: () => { setFileOrigin("files"); onOpenRepoFile(entry.path); } }]
                : []),
              { key: "copy-path", label: "Copy path", icon: "link" as const, onPress: () => void copyPath(entry.path) },
            ])
          }
        />
      </View>
    );
  } else {
    content = (
      <View style={[styles.body, wide ? styles.rootWide : null]}>
        <View
          style={[styles.listPane, wide ? [styles.listPaneWide, { borderRightColor: chrome.border }] : null, { backgroundColor: chrome.appBackground }]}
          importantForAccessibility={diffOpen && !wide ? "no-hide-descendants" : "auto"}
          accessibilityElementsHidden={diffOpen && !wide}
        >
          {listHeader}
          {error ? (
            <InlineNotice tone="danger" title="Couldn't refresh Git status" detail={error} action={{ label: "Retry", onPress: handleRefresh, disabled: loading }} style={styles.notice} />
          ) : null}
          <View style={styles.flex}>{listBody}</View>
          {commitBar}
        </View>
        {wide ? (
          <View style={[styles.detailPane, { backgroundColor: chrome.surface }]}>
            {reader}
            {source ? null : (
              <View style={styles.idleDetail} pointerEvents="none">
                <EmptyState icon="git-compare" title="No file selected" />
              </View>
            )}
          </View>
        ) : (
          // The reader is one long-lived WebView: mounted and unseen under the
          // list, so opening a file never pays its cold start.
          <View
            style={[styles.overlay, { backgroundColor: chrome.surface }, source ? null : styles.warm]}
            pointerEvents={source ? "auto" : "none"}
            importantForAccessibility={source ? "auto" : "no-hide-descendants"}
            accessibilityElementsHidden={!source}
          >
            {reader}
          </View>
        )}
      </View>
    );
  }

  return (
    <SafeAreaView style={[styles.root, { backgroundColor: chrome.appBackground }]} edges={["top", "left", "right"]}>
      <StatusBar barStyle={isLightTerminalTheme(theme) ? "dark-content" : "light-content"} />
      {content}
      <ActionMenu visible={menuVisible} title={menu?.title} items={menu?.items ?? []} onClose={() => setMenuVisible(false)} />
      <GitCommitSheet
        visible={commitOpen}
        fileCount={stagedCount}
        canPush={!snapshot?.detached}
        onClose={() => setCommitOpen(false)}
        onCommit={submitCommit}
      />
      <GitBranchSheet visible={branchesOpen} load={actions.branches} onClose={() => setBranchesOpen(false)} onSwitch={switchBranch} />
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  flex: { flex: 1 },
  body: { flex: 1 },
  rootWide: { flexDirection: "row" },
  listPane: { flex: 1, minWidth: 0 },
  // An explicit basis: on web `flex: 0` alone resolves to a 0% basis and
  // collapses the master pane.
  listPaneWide: { flex: 0, flexBasis: 360, width: 360, borderRightWidth: StyleSheet.hairlineWidth },
  detailPane: { flex: 1, minWidth: 0 },
  overlay: { position: "absolute", top: 0, left: 0, right: 0, bottom: 0 },
  // Mounted but unseen: full size, so the WebView lays out and loads.
  warm: { opacity: 0 },
  idleDetail: { ...StyleSheet.absoluteFill, justifyContent: "center" },
  topBar: { minHeight: 48, flexDirection: "row", alignItems: "center", paddingRight: 4, gap: 4 },
  strip: { minHeight: 40, flexDirection: "row", alignItems: "center", gap: 8, paddingHorizontal: 12, paddingBottom: 6 },
  branch: { maxWidth: "50%" },
  summary: { flex: 1, ...TypeScale.caption, ...UiTextMetrics, fontVariant: ["tabular-nums"] },
  search: { flexDirection: "row", alignItems: "center", gap: 8, marginHorizontal: 12, marginBottom: 6, paddingLeft: 10, borderRadius: 10, minHeight: 38 },
  searchInput: { flex: 1, minWidth: 0 },
  notice: { marginHorizontal: 12, marginBottom: 6 },
  commitBar: { paddingHorizontal: 12, paddingTop: 8, borderTopWidth: StyleSheet.hairlineWidth },
  commitButton: { minHeight: 44, borderRadius: 22 },
  filesTitle: { flex: 1, ...TypeScale.heading, fontSize: 15, ...UiTextMetrics },
  contentPad: { flex: 1, justifyContent: "center", paddingHorizontal: 16, paddingVertical: 16 },
});
