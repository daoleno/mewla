import { cleanup, Clipboard, effect, Image, Scroll, signal, Text, untracked, Video, View } from "nsh";
import type { JSX } from "nsh";
import { Button } from "nsh/base";
import { NavPage } from "../../shell/NavPage";
import {
  sessionFileDownloadLabel,
  sessionFileDownloadStatus,
  sessionFileMetaLine,
  sessionFileProblemTitle,
  sessionFileTitle,
  sessionFileTruncationNote,
  SESSION_FILE_NO_IDENTITY,
} from "../../../core/files/presentation";
import { getSessionFileMetadata, getSessionFileText } from "../../../core/files/requests";
import {
  bindSessionFileRequestToGeneration,
  buildSessionFileBinarySource,
  classifySessionFileRenderer,
  isStaleSessionFileError,
  sessionFileTooLargeMessage,
  type SessionFileBinarySource,
  type SessionFileIdentity,
  type SessionFileMetadata,
  type SessionFileTextPreview,
} from "../../../core/files/sessionFilePreview";
import { createSessionFileCopyLifecycleOwner, resolveSessionFileCopyContent } from "../../../core/files/sessionFilePreviewCopy";
import {
  createSessionFileDownloadLifecycleOwner,
  downloadSessionFile,
  isSessionFileDownloadCancelError,
  sessionFileCanDownload,
  sessionFileDownloadErrorMessage,
  type SessionFileDownloadFeedback,
} from "../../../core/files/sessionFilePreviewDownload";
import { failureText } from "../../../core/more/daemon";
import { daemon } from "../daemon";
import { Icon, type IconName } from "../kit/Icon";
import { EmptyState, Spinner } from "../kit/kit";
import type { PageNav } from "../routes";
import { cancelDownload, downloadHost, MewlaPdf } from "./host";

type Problem = { title: string; detail: string };

/**
 * A file a Session showed, read from the current computer for that live Session:
 * text and Markdown as text, pictures, PDFs and media streamed through a short-lived
 * read capability. Download saves it where the person chooses; Copy copies its text.
 * A computer switch closes it.
 */
export function FilePreviewPage(props: { reference: string; identity: SessionFileIdentity; nav: PageNav }) {
  const metadata = signal<SessionFileMetadata | null>(null);
  const text = signal<SessionFileTextPreview | null>(null);
  const source = signal<SessionFileBinarySource | null>(null);
  const loading = signal(true);
  const problem = signal<Problem | null>(null);
  const stale = signal(false);
  const copied = signal(false);
  const download = signal<SessionFileDownloadFeedback>("idle");
  const downloadError = signal("");
  let epoch = 0;
  let downloading = "";
  const serverId = daemon()?.server.id ?? "";

  const request = () => ({ ...props.identity, path: props.reference });

  const load = async () => {
    const d = daemon();
    const mine = ++epoch;
    metadata.set(null);
    text.set(null);
    source.set(null);
    problem.set(null);
    stale.set(false);
    loading.set(true);
    copy.replaceController();
    downloads.reset();
    const fail = (error: unknown, tooLarge = false) => {
      if (mine !== epoch) return;
      stale.set(isStaleSessionFileError(error));
      problem.set({ title: sessionFileProblemTitle({ stale: stale(), tooLarge }), detail: failureText(error, "The file is unavailable.") });
      loading.set(false);
    };
    if (!d) return fail(new Error("Connect to this computer to open the file."));
    if (!props.identity.workerId || !props.identity.processId || !props.identity.startedAt) return fail(new Error(SESSION_FILE_NO_IDENTITY));
    try {
      const found = await getSessionFileMetadata(d, request());
      if (mine !== epoch) return;
      metadata.set(found);
      if (found.tooLarge) return fail(new Error(sessionFileTooLargeMessage(found)), true);
      const renderer = classifySessionFileRenderer(found.kind);
      if (renderer === "markdown" || renderer === "text") {
        const read = await getSessionFileText(d, { ...request(), path: found.path, generation: found.generation });
        if (mine !== epoch) return;
        text.set(read);
      } else if (renderer !== "unsupported") {
        const signed = await buildSessionFileBinarySource(d.server, bindSessionFileRequestToGeneration(request(), found));
        if (mine !== epoch) return;
        source.set(signed);
      }
      loading.set(false);
    } catch (error) {
      fail(error);
    }
  };

  const copy = createSessionFileCopyLifecycleOwner<ReturnType<typeof setTimeout>>({
    copyText: (value) => Clipboard.writeText(value),
    onCopiedChange: (value) => copied.set(value),
    scheduleReset: (callback, ms) => setTimeout(callback, ms),
    cancelReset: (timer) => clearTimeout(timer),
  });
  const downloads = createSessionFileDownloadLifecycleOwner({
    onFeedbackChange(feedback, error) {
      if (feedback === "failed" && isSessionFileDownloadCancelError(error)) feedback = "idle";
      downloadError.set(feedback === "failed" ? sessionFileDownloadErrorMessage(error) : "");
      download.set(feedback);
    },
  });

  const sign = (found: SessionFileMetadata) => {
    const d = daemon();
    if (!d) throw new Error("Connect to this computer to download the file.");
    return buildSessionFileBinarySource(d.server, bindSessionFileRequestToGeneration(request(), found));
  };

  const onCopy = () => {
    const found = metadata();
    void resolveSessionFileCopyContent(text(), async () => {
      const signed = await sign(found!);
      const response = await fetch(signed.uri, { headers: signed.headers });
      if (!response.ok) throw new Error(`Session file download failed (HTTP ${response.status}).`);
      return response.text();
    })
      .then((value) => (value === null ? undefined : copy.copy(value)))
      .catch(() => {});
  };

  const onDownload = () => {
    const found = metadata();
    if (!found || !sessionFileCanDownload(found)) return;
    void downloads
      .start(() => downloadSessionFile({ host: downloadHost, metadata: found, sign: () => sign(found), onStart: (id) => (downloading = id) }))
      .catch(() => {})
      .finally(() => (downloading = ""));
  };

  // A switch to another computer closes the file: it was that computer's.
  effect(() => {
    const id = daemon()?.server.id ?? "";
    untracked(() => {
      if (id !== serverId) props.nav.pop();
    });
  });
  cleanup(() => {
    epoch += 1;
    copy.dispose();
    downloads.dispose();
    if (downloading) cancelDownload(downloading);
  });
  void load();

  const title = () => sessionFileTitle(metadata(), props.reference);
  const canDownload = () => sessionFileCanDownload(metadata());
  const status = () => sessionFileDownloadStatus(download(), downloadError());

  return (
    <NavPage
      title={title()}
      testID="file-preview"
      headerRight={
        <View class="flex-row items-center gap-1">
          {stale() ? <Action testID="file-refresh" label="Refresh changed file" icon="refresh" onPress={() => void load()} /> : null}
          <Action
            testID="file-download"
            label={sessionFileDownloadLabel({ busy: download() === "busy", can: canDownload() })}
            icon="download"
            busy={download() === "busy"}
            disabled={!canDownload() || download() === "busy"}
            onPress={onDownload}
          />
          {text() ? <Action testID="file-copy" label={copied() ? "Copied" : "Copy"} icon={copied() ? "check" : "copy"} onPress={onCopy} /> : null}
          <Action testID="file-close" label="Close file preview" icon="close" onPress={() => props.nav.pop()} />
        </View>
      }
    >
      <View class="flex-1 bg-background">
        <View class="border-b border-border px-4 py-2">
          <Text testID="file-meta" class="font-mono text-xs text-muted-foreground line-clamp-1">{sessionFileMetaLine(metadata(), props.reference)}</Text>
          {status() ? <Text testID="file-download-status" class={download() === "saved" ? "mt-0.5 text-xs" : "mt-0.5 text-xs text-destructive"}>{status()!}</Text> : null}
        </View>
        <>{body()}</>
      </View>
    </NavPage>
  );

  function body(): JSX.Element {
    if (loading()) return <EmptyState testID="file-opening" busy title="Opening file" />;
    const failed = problem();
    if (failed) return <EmptyState testID="file-problem" icon="alert-circle" title={failed.title} detail={failed.detail} action="Refresh" onAction={() => void load()} />;
    const found = metadata();
    if (!found) return <EmptyState title="Could not open file" detail="The file is unavailable." />;
    const renderer = classifySessionFileRenderer(found.kind);
    const read = text();
    if ((renderer === "markdown" || renderer === "text") && read) return <TextBody text={read} />;
    const signed = source();
    if (renderer === "image" && signed) return <ImageBody source={signed} name={found.name} />;
    if (renderer === "pdf" && signed) return <PdfBody source={signed} />;
    if ((renderer === "video" || renderer === "audio") && signed) {
      return (
        <View class="flex-1 items-center justify-center bg-background p-4">
          <Video testID="file-media" src={signed.uri} controls paused class={renderer === "video" ? "aspect-video w-full" : "h-16 w-full"} accessibilityLabel={renderer === "video" ? "Video preview" : "Audio preview"} />
        </View>
      );
    }
    return <EmptyState testID="file-unsupported" icon="document" title="Preview not supported" detail={sessionFileCanDownload(found) ? "Download it to open it in another app." : undefined} />;
  }
}

/** Text and Markdown as the file has them, in mono, scrolling both ways. */
function TextBody(props: { text: SessionFileTextPreview }) {
  const note = sessionFileTruncationNote(props.text);
  return (
    <Scroll class="flex-1" contentClass="pb-10">
      <Scroll horizontal class="w-full" contentClass="px-4 py-3">
        <Text testID="file-text" class="font-mono text-sm">{props.text.content || " "}</Text>
      </Scroll>
      {note ? <Text testID="file-truncated" class="px-4 pt-2 text-xs text-muted-foreground">{note}</Text> : null}
    </Scroll>
  );
}

function ImageBody(props: { source: SessionFileBinarySource; name: string }) {
  const failed = signal("");
  return (
    <>
      {failed() ? (
        <EmptyState icon="image" title="Could not open file" detail={failed()} />
      ) : (
        <Scroll class="flex-1" contentClass="grow items-center justify-center" maximumZoomScale={6} accessibilityLabel="Zoomable image preview">
          <Image testID="file-image" src={props.source.uri} class="h-full w-full" style={{ objectFit: "contain" }} accessibilityLabel={props.name} onError={(e) => failed.set(e.message || "The image could not be shown.")} />
        </Scroll>
      )}
    </>
  );
}

function PdfBody(props: { source: SessionFileBinarySource }) {
  const failed = signal("");
  const shown = signal(false);
  return (
    <>
      {failed() ? (
        <EmptyState icon="document" title="Could not open file" detail={failed()} />
      ) : (
        <View class="flex-1">
          <MewlaPdf testID="file-pdf" class="flex-1" source={props.source.uri} headers={props.source.headers} accessibilityLabel="PDF preview" onLoad={() => shown.set(true)} onError={(e) => failed.set(e.message || "This PDF could not be rendered on this device.")} />
          {shown() ? null : <View class="absolute inset-0 items-center justify-center"><Spinner size={24} /></View>}
        </View>
      )}
    </>
  );
}

function Action(props: { label: string; icon: IconName; onPress: () => void; disabled?: boolean; busy?: boolean; testID?: string }) {
  return (
    <Button testID={props.testID} accessibilityLabel={props.label} disabled={props.disabled} onPress={props.onPress} class="size-9 items-center justify-center rounded-full active:bg-accent disabled:opacity-40">
      {props.busy ? <Spinner size={16} /> : <Icon name={props.icon} size={18} tone="ink" />}
    </Button>
  );
}
