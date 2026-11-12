import { cleanup, Clipboard, effect, Scroll, signal, Text, untracked, View } from "nsh";
import type { JSX } from "nsh";
import { Button, TextView } from "nsh/base";
import { listDetail } from "../../shell/layout";
import { NavPage } from "../../shell/NavPage";
import { getBrainWorkspaceFile, getBrainWorkspaceTree, getSessionFileMetadata } from "../../../core/files/requests";
import {
  bindSessionFileRequestToGeneration,
  buildSessionFileBinarySource,
  sessionFileTooLargeMessage,
  type SessionFileBinarySource,
  type SessionFileIdentity,
} from "../../../core/files/sessionFilePreview";
import { sessionFileMediaKindForPath } from "../../../core/files/sessionFileMedia";
import {
  brainWorkspaceEntryAccessibilityLabel,
  brainWorkspaceEntryIconName,
  brainWorkspaceMarkdownPath,
  workspaceFolderEntries,
  workspaceFolderKey,
  workspaceLinkPath,
  type BrainWorkspaceFile,
  type BrainWorkspaceTree,
} from "../../../core/files/workspace";
import { failureText } from "../../../core/more/daemon";
import { daemon } from "../daemon";
import { Icon } from "../../kit/Icon";
import { DisplayTitle, EmptyState, Row, Section } from "../../kit/kit";
import { formatBytes } from "../../../core/resources/resourceTelemetry";
import type { PageNav } from "../routes";
import { ZoomImage } from "./FilePreview";
import { MediaPlayer } from "./Media";

// Each folder the workspace showed, per (computer, folder), while the app runs: going back into one reads nothing.
const trees = new Map<string, BrainWorkspaceTree>();
// Each computer's workspace folder, for the media its Brain Session plays.
const roots = new Map<string, string>();

/** The workspace folder with `~` for the home folder. */
function compact(path: string): string {
  return path.replace(/^\/(Users|home)\/[^/]+/, "~");
}

/** Where a page of the workspace is, as the old viewer's subtitle said: the workspace path, then the folder or file in it. */
export function workspaceLocation(root: string | undefined, path: string): string {
  const base = root ? compact(root).replace(/\/$/, "") : "";
  if (!path) return base;
  return base ? `${base}/${path}` : path;
}

/** Brain's workspace on the current computer: folders, then a file. A computer switch closes it. */
export function WorkspacePage(props: { trail?: string[]; file?: string; hostSession?: SessionFileIdentity; nav: PageNav }) {
  const serverId = daemon()?.server.id ?? "";
  effect(() => {
    const id = daemon()?.server.id ?? "";
    untracked(() => {
      if (id !== serverId) props.nav.pop();
    });
  });
  const name = props.file ? props.file.split("/").at(-1)! : props.trail?.length ? props.trail.at(-1)!.split("/").at(-1)! : "Workspace";
  // A folder's display title is its name, under its path; the nav bar names it once that has scrolled away.
  const titleAway = signal(Boolean(props.file));
  // On wide with room (SPEC §14.2) a file opens beside its folder; else on its own page.
  const chosen = signal<string | null>(null);
  const openFile = (path: string) => {
    if (listDetail()) return chosen.set(path);
    props.nav.push({ name: "files-workspace", file: path, hostSession: props.hostSession });
  };
  if (props.file) {
    return (
      <NavPage title={titleAway() ? name : ""} testID="workspace">
        <WorkspaceFile path={props.file} hostSession={props.hostSession} nav={props.nav} />
      </NavPage>
    );
  }
  return (
    <NavPage title={titleAway() ? name : ""} testID="workspace" column={listDetail() ? "full" : "page"}>
      <View class="flex-1 flex-row bg-background">
        <View testID="workspace-list" class={listDetail() ? "w-[380px] border-r border-border" : "flex-1"}>
          <WorkspaceFolder trail={props.trail ?? []} name={name} hostSession={props.hostSession} nav={props.nav} onOpenFile={openFile} selected={() => (listDetail() ? chosen() : null)} onTitleAway={(away) => titleAway.set(away)} />
        </View>
        {listDetail() ? (
          <View testID="workspace-reader" class="flex-1 bg-background pt-2">
            {chosen()
              ? <WorkspaceFile path={chosen()!} hostSession={props.hostSession} nav={props.nav} />
              : <View class="flex-1 px-4"><EmptyState testID="workspace-no-file" icon="document" title="No file selected" /></View>}
          </View>
        ) : null}
      </View>
    </NavPage>
  );
}

/** Where this folder or file is: the path, as the eyebrow over its name. */
function location(path: string): string {
  return workspaceLocation(roots.get(daemon()?.server.id ?? ""), path);
}

function WorkspaceFolder(props: { trail: string[]; name: string; hostSession?: SessionFileIdentity; nav: PageNav; onOpenFile: (path: string) => void; selected: () => string | null; onTitleAway: (away: boolean) => void }) {
  const id = daemon()?.server.id ?? "";
  const path = props.trail.at(-1) ?? "";
  const tree = signal<BrainWorkspaceTree | null>(trees.get(workspaceFolderKey(id, path)) ?? null);
  const error = signal("");
  const busy = signal(false);
  // Each folder is its own `brain_workspace_tree {path}`: the daemon lists one level at a time.
  const read = async () => {
    const d = daemon();
    if (!d) return error.set("Connect to this computer to browse Brain's workspace.");
    busy.set(true);
    try {
      const next = await getBrainWorkspaceTree(d, path);
      trees.set(workspaceFolderKey(d.server.id, path), next);
      if (next.workspace) roots.set(d.server.id, next.workspace);
      tree.set(next);
      error.set("");
    } catch (failure) {
      error.set(failureText(failure, props.trail.length ? "Failed to load Brain workspace folder." : "Failed to load Brain workspace."));
    } finally {
      busy.set(false);
    }
  };
  if (!tree()) void read();

  return (
    <Scroll keyboardShouldPersistTaps="handled" class="flex-1 bg-background" contentClass="px-4 pb-10 pt-2" refreshing={busy() && !!tree()} onRefresh={() => void read()}
      onScroll={(e) => props.onTitleAway(e.offset.y > 56)} scrollEventThrottle={64}>
      <View class="w-full max-w-[680px] self-center">
        {/* Read with the tree: the workspace's own folder comes with its first answer. */}
        <DisplayTitle testID="workspace-location" eyebrow={(tree() ? location(path) : "") || undefined} title={props.name} />
        <>{content()}</>
      </View>
    </Scroll>
  );

  function content(): JSX.Element {
    const t = tree();
    if (!t) {
      return error()
        ? <EmptyState testID="workspace-error" icon="alert-circle" title={error()} action="Try again" onAction={() => void read()} />
        : <EmptyState testID="workspace-loading" busy title="Loading workspace" />;
    }
    const entries = workspaceFolderEntries(t);
    // Folders first, a group of rows that push; then the files with their sizes (SPEC §15 File browser).
    const row = (entry: (typeof entries)[number]) => (
      <Row
        testID={`workspace-${entry.path}`}
        title={entry.name}
        icon={brainWorkspaceEntryIconName(entry.kind, entry.path)}
        value={entry.kind === "directory" || entry.size === undefined ? undefined : formatBytes(entry.size)}
        chevron={entry.kind === "directory"}
        accessibilityLabel={brainWorkspaceEntryAccessibilityLabel(entry.kind, entry.name)}
        selected={entry.kind !== "directory" && props.selected() === entry.path}
        onPress={() =>
          entry.kind === "directory"
            ? props.nav.push({ name: "files-workspace", trail: [...props.trail, entry.path], hostSession: props.hostSession })
            : props.onOpenFile(entry.path)
        }
      />
    );
    const folders = entries.filter((entry) => entry.kind === "directory");
    const files = entries.filter((entry) => entry.kind !== "directory");
    return (
      <View>
        {folders.length ? <Section title="Folders">{folders.map(row)}</Section> : null}
        {files.length ? <Section title="Files">{files.map(row)}</Section> : null}
        {entries.length ? null : <EmptyState testID="workspace-empty" icon="folder-open" title="This folder is empty." />}
      </View>
    );
  }
}

function WorkspaceFile(props: { path: string; hostSession?: SessionFileIdentity; nav: PageNav }) {
  const media = sessionFileMediaKindForPath(props.path);
  const file = signal<BrainWorkspaceFile | null>(null);
  const stream = signal<SessionFileBinarySource | null>(null);
  const error = signal("");
  const copied = signal(false);
  let copiedTimer: ReturnType<typeof setTimeout> | null = null;
  let sign: (() => Promise<SessionFileBinarySource>) | null = null;
  cleanup(() => copiedTimer && clearTimeout(copiedTimer));
  const read = async () => {
    const d = daemon();
    if (!d) return error.set("Connect to this computer to open the file.");
    error.set("");
    stream.set(null);
    try {
      if (media) {
        if (!props.hostSession) throw new Error("Brain isn't running, so this file can't be played yet.");
        const root = roots.get(d.server.id);
        const request = { ...props.hostSession, path: root ? `${root.replace(/\/$/, "")}/${props.path}` : props.path };
        const metadata = await getSessionFileMetadata(d, request);
        if (metadata.kind !== "video" && metadata.kind !== "audio") throw new Error("This file isn't a video or audio file Mewla can play.");
        if (metadata.tooLarge) throw new Error(sessionFileTooLargeMessage(metadata));
        const server = d.server;
        sign = () => buildSessionFileBinarySource(server, bindSessionFileRequestToGeneration(request, metadata));
        stream.set(await sign());
      } else {
        file.set(await getBrainWorkspaceFile(d, props.path));
      }
    } catch (failure) {
      error.set(failureText(failure, "Failed to load Brain workspace file."));
    }
  };
  void read();

  const copy = () => {
    const f = file();
    if (!f?.content) return;
    void Clipboard.writeText(f.content).then(
      () => {
        copied.set(true);
        if (copiedTimer) clearTimeout(copiedTimer);
        copiedTimer = setTimeout(() => copied.set(false), 1_600);
      },
      () => {},
    );
  };

  // A link in a note opens the workspace file it names; links that leave the workspace do nothing.
  const openLink = (href: string) => {
    const next = workspaceLinkPath(props.path, href);
    if (next) props.nav.push({ name: "files-workspace", file: next, hostSession: props.hostSession });
  };

  return (
    <View class="flex-1 bg-background">
      {/* The file's name over its path, Copy as an icon button, then the content on a surface card. */}
      <View class="min-h-[60px] flex-row items-center gap-2 px-4">
        <View class="min-w-0 flex-1">
          <Text class="text-title font-bold line-clamp-1" role="header">{props.path.split("/").at(-1)!}</Text>
          {location(props.path) ? <Text testID="workspace-location" class="font-mono text-caption text-muted-foreground line-clamp-1">{location(props.path)}</Text> : null}
        </View>
        {file()?.content && !file()?.data_url ? (
          <Button testID="workspace-copy" accessibilityLabel={copied() ? "Copied" : "Copy"} onPress={copy} class="size-11 items-center justify-center rounded-full active:bg-accent">
            <Icon name={copied() ? "check" : "copy"} size={20} tone="ink" />
          </Button>
        ) : null}
      </View>
      <View class="mx-4 mb-4 flex-1 overflow-hidden rounded-(--radius-group) bg-card">{content()}</View>
    </View>
  );

  function content(): JSX.Element {
    if (error()) return <EmptyState testID="workspace-file-error" icon="alert-circle" title={error()} action="Try again" onAction={() => void read()} />;
    const signed = stream();
    if (signed && media) {
      return <MediaPlayer testID="workspace-media" kind={media} source={signed} resolve={() => (sign ? sign() : Promise.reject(new Error("Could not authorize the stream again.")))} onFail={(message) => error.set(message)} />;
    }
    const f = file();
    if (!f) return <EmptyState testID="workspace-file-loading" busy title="Loading file" />;
    if (f.data_url) return <ZoomImage testID="workspace-image" src={f.data_url} name={f.name} />;
    if (!f.content) return <EmptyState testID="workspace-file-empty" icon="document" title="Empty file." />;
    if (brainWorkspaceMarkdownPath(f.path || props.path)) {
      return (
        <Scroll keyboardShouldPersistTaps="handled" class="flex-1" contentClass="px-4 pb-10 pt-3">
          <TextView testID="workspace-markdown" markdown={f.content} selectable onLinkPress={(event) => openLink(event.url)} />
        </Scroll>
      );
    }
    return (
      <Scroll keyboardShouldPersistTaps="handled" class="flex-1" contentClass="pb-10">
        <Scroll keyboardShouldPersistTaps="handled" horizontal class="w-full" contentClass="px-4 py-3">
          <View selectable>
            <Text testID="workspace-text" class="font-mono text-code">{f.content}</Text>
          </View>
        </Scroll>
      </Scroll>
    );
  }
}
