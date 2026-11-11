import { effect, Image, Scroll, signal, Text, untracked, Video, View } from "nsh";
import type { JSX } from "nsh";
import { NavPage } from "../../shell/NavPage";
import { getBrainWorkspaceFile, getBrainWorkspaceTree, getSessionFileMetadata } from "../../../core/files/requests";
import { bindSessionFileRequestToGeneration, buildSessionFileBinarySource, type SessionFileIdentity } from "../../../core/files/sessionFilePreview";
import { sessionFileMediaKindForPath } from "../../../core/files/sessionFileMedia";
import {
  brainWorkspaceEntryIconName,
  workspaceFolderEntries,
  workspaceFolderKey,
  type BrainWorkspaceFile,
  type BrainWorkspaceTree,
} from "../../../core/files/workspace";
import { failureText } from "../../../core/more/daemon";
import { daemon } from "../daemon";
import { EmptyState, Row, Section } from "../kit/kit";
import type { PageNav } from "../routes";

// Each folder the workspace showed, per (computer, folder), while the app runs: going back into one reads nothing.
const trees = new Map<string, BrainWorkspaceTree>();
// Each computer's workspace folder, for the media its Brain Session plays.
const roots = new Map<string, string>();

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
  return (
    <NavPage title={name} testID="workspace">
      {props.file ? <WorkspaceFile path={props.file} hostSession={props.hostSession} /> : <WorkspaceFolder trail={props.trail ?? []} hostSession={props.hostSession} nav={props.nav} />}
    </NavPage>
  );
}

function WorkspaceFolder(props: { trail: string[]; hostSession?: SessionFileIdentity; nav: PageNav }) {
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
    <Scroll class="flex-1 bg-background" contentClass="px-4 pb-10 pt-2" refreshing={busy() && !!tree()} onRefresh={() => void read()}>
      <>{content()}</>
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
    if (!entries.length) return <EmptyState testID="workspace-empty" icon="folder-open" title="This folder is empty." />;
    return (
      <Section title={props.trail.length ? undefined : t.workspace?.replace(/^\/(Users|home)\/[^/]+/, "~")}>
        {entries.map((entry) => (
          <Row
            testID={`workspace-${entry.path}`}
            title={entry.name}
            icon={brainWorkspaceEntryIconName(entry.kind, entry.path)}
            chevron={entry.kind === "directory"}
            onPress={() =>
              props.nav.push(
                entry.kind === "directory"
                  ? { name: "files-workspace", trail: [...props.trail, entry.path], hostSession: props.hostSession }
                  : { name: "files-workspace", file: entry.path, hostSession: props.hostSession },
              )
            }
          />
        ))}
      </Section>
    );
  }
}

function WorkspaceFile(props: { path: string; hostSession?: SessionFileIdentity }) {
  const media = sessionFileMediaKindForPath(props.path);
  const file = signal<BrainWorkspaceFile | null>(null);
  const stream = signal("");
  const error = signal("");
  const read = async () => {
    const d = daemon();
    if (!d) return error.set("Connect to this computer to open the file.");
    error.set("");
    try {
      if (media) {
        if (!props.hostSession) throw new Error("Brain isn't running, so this file can't be played yet.");
        const root = roots.get(d.server.id);
        const request = { ...props.hostSession, path: root ? `${root.replace(/\/$/, "")}/${props.path}` : props.path };
        const metadata = await getSessionFileMetadata(d, request);
        if (metadata.kind !== "video" && metadata.kind !== "audio") throw new Error("This file isn't a video or audio file Mewla can play.");
        stream.set((await buildSessionFileBinarySource(d.server, bindSessionFileRequestToGeneration(request, metadata))).uri);
      } else {
        file.set(await getBrainWorkspaceFile(d, props.path));
      }
    } catch (failure) {
      error.set(failureText(failure, "Failed to load Brain workspace file."));
    }
  };
  void read();

  return <>{content()}</>;

  function content(): JSX.Element {
    if (error()) return <EmptyState testID="workspace-file-error" icon="alert-circle" title={error()} action="Try again" onAction={() => void read()} />;
    if (stream()) {
      return (
        <View class="flex-1 items-center justify-center bg-background p-4">
          <Video testID="workspace-media" src={stream()} controls paused class={media === "video" ? "aspect-video w-full" : "h-16 w-full"} />
        </View>
      );
    }
    const f = file();
    if (!f) return <EmptyState testID="workspace-file-loading" busy title="Loading file" />;
    if (f.data_url) {
      return (
        <Scroll class="flex-1 bg-background" contentClass="grow items-center justify-center" maximumZoomScale={6}>
          <Image testID="workspace-image" src={f.data_url} class="h-full w-full" style={{ objectFit: "contain" }} accessibilityLabel={f.name} />
        </Scroll>
      );
    }
    if (!f.content) return <EmptyState testID="workspace-file-empty" icon="document" title="Empty file." />;
    return (
      <Scroll class="flex-1 bg-background" contentClass="pb-10">
        <Scroll horizontal class="w-full" contentClass="px-4 py-3">
          <Text testID="workspace-text" class="font-mono text-sm">{f.content}</Text>
        </Scroll>
      </Scroll>
    );
  }
}
