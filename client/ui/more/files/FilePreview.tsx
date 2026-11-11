import { cleanup, Clipboard, effect, Image, openUrl, Scroll, signal, Text, untracked, View, WebView } from "nsh";
import type { JSX, ScrollRef } from "nsh";
import { Button, TextView } from "nsh/base";
import { NavPage } from "../../shell/NavPage";
import { isSvgImage } from "../../../core/files/imageSource";
import { dirnameOf, resolveMarkdownLink } from "../../../core/files/markdownLinks";
import { loadSvgPreview, svgPreviewHtml, type SvgPreview } from "../../../core/files/svgPreview";
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
import { color } from "../kit/colors";
import { Icon, type IconName } from "../kit/Icon";
import { EmptyState, Spinner } from "../kit/kit";
import type { PageNav } from "../routes";
import { cancelDownload, downloadHost, MewlaPdf } from "./host";
import { MediaPlayer } from "./Media";
import { filePreviewRoute } from "./routes";

/** What a picture's or PDF's stream failing means: the file changed, or the stream lapsed. */
export const SESSION_FILE_STREAM_FAILED = "The file stream changed or could not be loaded. Refresh and try again.";

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
  // A cut text copies the whole file through the stream, so only while it can be read whole.
  const canCopy = () => !!text() && (!text()!.truncated || canDownload());
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
          {canCopy() ? <Action testID="file-copy" label={copied() ? "Copied" : "Copy"} icon={copied() ? "check" : "copy"} onPress={onCopy} /> : null}
          <Action testID="file-close" label="Close file preview" icon="close" onPress={() => props.nav.pop()} />
        </View>
      }
    >
      <View class="flex-1 bg-background">
        <View class="border-b border-border px-4 py-2">
          <Text testID="file-meta" class="font-mono text-xs text-muted-foreground line-clamp-1">{sessionFileMetaLine(metadata(), props.reference)}</Text>
          {status() ? <Text testID="file-download-status" android={{ accessibilityLiveRegion: "polite" }} class={download() === "saved" ? "mt-0.5 text-xs" : "mt-0.5 text-xs text-destructive"}>{status()!}</Text> : null}
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
    if (renderer === "markdown" && read) return <MarkdownBody text={read} onLink={(href) => openLink(href, found)} />;
    if (renderer === "text" && read) return <TextBody text={read} />;
    const signed = source();
    if (renderer === "image" && signed) {
      if (isSvgImage({ kind: "owned", path: found.path, name: found.name, mimeType: found.contentType }, signed)) {
        return <SvgBody source={signed} onFail={(message) => failInPlace(message, false)} />;
      }
      return <ImageBody source={signed} name={found.name} onFail={() => failInPlace(SESSION_FILE_STREAM_FAILED, true)} />;
    }
    if (renderer === "pdf" && signed) return <PdfBody source={signed} expectedBytes={found.size} onFail={(message) => failInPlace(message, /changed|exceeded its inspected/i.test(message))} />;
    if ((renderer === "video" || renderer === "audio") && signed) {
      return <MediaPlayer testID="file-media" kind={renderer} source={signed} resolve={() => sign(found)} onFail={failInPlace} />;
    }
    return <EmptyState testID="file-unsupported" icon="document" title="Preview not supported" detail={sessionFileCanDownload(found) ? "Download it to open it in another app." : undefined} />;
  }

  /** A failure after the file showed: the preview says so and offers Refresh, marked changed when it is. */
  function failInPlace(message: string, isStale: boolean): void {
    stale.set(isStale);
    problem.set({ title: sessionFileProblemTitle({ stale: isStale, tooLarge: false }), detail: message || "The file is unavailable." });
  }

  /** A link in a previewed Markdown file: a file beside it opens in the preview, a web link outside the app. */
  function openLink(href: string, found: SessionFileMetadata): void {
    const target = resolveMarkdownLink(href, dirnameOf(found.path));
    if (target.kind === "file") props.nav.push(filePreviewRoute(target.reference, props.identity));
    else if (target.kind === "external") void openUrl(target.url).catch(() => {});
  }
}

/** Markdown rendered, selectable, its links reported to the preview. */
function MarkdownBody(props: { text: SessionFileTextPreview; onLink: (href: string) => void }) {
  const note = sessionFileTruncationNote(props.text);
  return (
    <Scroll class="flex-1" contentClass="px-4 pb-10 pt-3">
      <TextView testID="file-markdown" markdown={props.text.content} selectable onLinkPress={(event) => props.onLink(event.url)} />
      {note ? <Text testID="file-truncated" class="pt-2 text-xs text-muted-foreground">{note}</Text> : null}
    </Scroll>
  );
}

/** Text as the file has it, in mono, scrolling both ways, selectable. */
function TextBody(props: { text: SessionFileTextPreview }) {
  const note = sessionFileTruncationNote(props.text);
  return (
    <Scroll class="flex-1" contentClass="pb-10">
      <Scroll horizontal class="w-full" contentClass="px-4 py-3">
        <View selectable>
          <Text testID="file-text" class="font-mono text-sm">{props.text.content || " "}</Text>
        </View>
      </Scroll>
      {note ? <Text testID="file-truncated" class="px-4 pt-2 text-xs text-muted-foreground">{note}</Text> : null}
    </Scroll>
  );
}

const DOUBLE_TAP_MS = 300;

/** A picture that zooms: pinch, or double tap for 2× and back; a spinner until it shows. */
export function ZoomImage(props: { src: string; name: string; testID?: string; onFail?: (message: string) => void }) {
  const shown = signal(false);
  let scroll: ScrollRef | null = null;
  let zoomed = false;
  let lastTap = 0;
  const tap = (x: number, y: number, clicks: number) => {
    const now = Date.now();
    if (clicks < 2 && now - lastTap > DOUBLE_TAP_MS) {
      lastTap = now;
      return;
    }
    lastTap = 0;
    zoomed = !zoomed;
    scroll?.zoomTo(zoomed ? { scale: 2, x, y, animated: true } : { scale: 1, animated: true });
  };
  return (
    <View class="flex-1">
      <Scroll ref={(ref) => (scroll = ref)} class="flex-1" contentClass="grow items-center justify-center" maximumZoomScale={4} minimumZoomScale={1} accessibilityLabel="Zoomable image preview">
        <View class="h-full w-full" onPress={(event) => tap(event.position.x, event.position.y, event.clickCount)}>
          <Image
            testID={props.testID}
            src={props.src}
            class="h-full w-full"
            style={{ objectFit: "contain" }}
            accessibilityLabel={props.name}
            onLoad={() => shown.set(true)}
            onError={(e) => {
              shown.set(true);
              props.onFail?.(e.message);
            }}
          />
        </View>
      </Scroll>
      {shown() ? null : <View class="absolute inset-0 items-center justify-center"><Spinner size={24} /></View>}
    </View>
  );
}

function ImageBody(props: { source: SessionFileBinarySource; name: string; onFail: () => void }) {
  return <ZoomImage testID="file-image" src={props.source.uri} name={props.name} onFail={() => props.onFail()} />;
}

/** An SVG read whole (at most 2 MiB), checked, and drawn where nothing runs or loads. */
function SvgBody(props: { source: SessionFileBinarySource; onFail: (message: string) => void }) {
  const preview = signal<SvgPreview | null>(null);
  void loadSvgPreview(props.source, (uri, headers) => fetch(uri, { headers, redirect: "error" })).then(
    (checked) => preview.set(checked),
    (error: unknown) => props.onFail(error instanceof Error && error.message ? error.message : "Unsupported SVG content."),
  );
  // A view, not a fragment, holds what changes: read in a fragment, `preview` would remake the page's body.
  return (
    <View class="flex-1">
      {preview() ? (
        <WebView
          testID="file-svg"
          class="flex-1"
          html={svgPreviewHtml(preview()!, color("paper"))}
          baseUrl="https://viewer.mewla.invalid/"
          javaScriptEnabled={false}
          accessibilityLabel="Zoomable image preview"
          onError={() => props.onFail(SESSION_FILE_STREAM_FAILED)}
        />
      ) : (
        <View class="flex-1 items-center justify-center"><Spinner size={24} /></View>
      )}
    </View>
  );
}

/** The PDF's pages; the host refuses a file whose size is not the one inspected (it changed). */
function PdfBody(props: { source: SessionFileBinarySource; expectedBytes: number; onFail: (message: string) => void }) {
  const shown = signal(false);
  return (
    <View class="flex-1">
      <MewlaPdf
        testID="file-pdf"
        class="flex-1"
        source={props.source.uri}
        headers={props.source.headers}
        expectedBytes={props.expectedBytes}
        accessibilityLabel="PDF preview"
        onLoad={() => shown.set(true)}
        onError={(e) => props.onFail(e.message || "This PDF could not be rendered on this device.")}
      />
      {shown() ? null : <View class="absolute inset-0 items-center justify-center"><Spinner size={24} /></View>}
    </View>
  );
}

function Action(props: { label: string; icon: IconName; onPress: () => void; disabled?: boolean; busy?: boolean; testID?: string }) {
  return (
    <Button testID={props.testID} accessibilityLabel={props.label} disabled={props.disabled} onPress={props.onPress} class="size-9 items-center justify-center rounded-full active:bg-accent disabled:opacity-40">
      {props.busy ? <Spinner size={16} /> : <Icon name={props.icon} size={18} tone="ink" />}
    </Button>
  );
}
