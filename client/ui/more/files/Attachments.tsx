import { Image, signal, Text, View } from "nsh";
import { Button, Dialog } from "nsh/base";
import type { AttachmentQueueItem } from "../../../core/files/attachmentUploadQueue";
import { buildAttachmentUploadPresentation } from "../../../core/files/attachmentUploadPresentation";
import { isImageAttachment } from "../../../core/files/imageSource";
import { Icon } from "../../kit/Icon";
import { Spinner } from "../../kit/kit";
import { ZoomImage } from "./FilePreview";
import { thumbnail } from "./host";
import { attachmentLabel, type AttachmentUploads } from "./upload";

/** How big a chip's picture is read (points × a dense screen), and the viewer's. */
const CHIP_PIXELS = 120;
const VIEWER_PIXELS = 2048;

/**
 * The chips above a composer: one per picked file, with its progress, Cancel, Retry or
 * Remove; a picture shows itself, and a tap on it opens it whole.
 */
export function AttachmentTray(props: { uploads: AttachmentUploads }) {
  const viewing = signal<{ uri: string; name: string } | null>(null);
  return (
    <View class="flex-row flex-wrap gap-2 px-3 py-2">
      {props.uploads.items().map((item) => <Chip item={item} uploads={props.uploads} onOpen={() => viewing.set({ uri: item.asset.uri, name: item.asset.name })} />)}
      <Dialog testID="attachment-viewer" open={!!viewing()} animation="fade" onOpenChange={({ open }) => !open && viewing.set(null)} class="flex-1 bg-background">
        {viewing() ? <Viewer uri={viewing()!.uri} name={viewing()!.name} onClose={() => viewing.set(null)} /> : null}
      </Dialog>
    </View>
  );
}

/** A picked picture at its size, zoomable, with Close. */
function Viewer(props: { uri: string; name: string; onClose: () => void }) {
  const src = signal<string | null>(null);
  const failed = signal(false);
  thumbnail(props.uri, VIEWER_PIXELS).then((url) => (url ? src.set(url) : failed.set(true)), () => failed.set(true));
  return (
    <View class="flex-1 bg-background pt-10">
      <View class="flex-row items-center px-3">
        <Text class="min-w-0 flex-1 text-body font-semibold line-clamp-1">{props.name}</Text>
        <Button testID="attachment-viewer-close" accessibilityLabel="Close" onPress={props.onClose} class="size-10 items-center justify-center rounded-full active:bg-accent">
          <Icon name="close" size={20} tone="ink" />
        </Button>
      </View>
      <View class="flex-1">
        {src() ? (
          <ZoomImage testID="attachment-viewer-image" src={src()!} name={props.name} />
        ) : failed() ? (
          <View class="flex-1 items-center justify-center px-6"><Text class="text-center text-sub text-muted-foreground">This picture can't be shown here.</Text></View>
        ) : (
          <View class="flex-1 items-center justify-center"><Spinner size={24} /></View>
        )}
      </View>
    </View>
  );
}

/** The picked picture, small, read by the host from its own handle of the file. */
function Thumbnail(props: { uri: string; name: string; testID: string }) {
  const src = signal<string | null>(null);
  thumbnail(props.uri, CHIP_PIXELS).then((url) => src.set(url), () => {});
  return (
    <View class="size-9 overflow-hidden rounded-lg bg-secondary items-center justify-center">
      {src() ? <Image testID={props.testID} src={src()!} class="size-9" style={{ objectFit: "cover" }} accessibilityLabel={props.name} /> : <Icon name="image" size={16} tone="soft" />}
    </View>
  );
}

function Chip(props: { item: AttachmentQueueItem; uploads: AttachmentUploads; onOpen: () => void }) {
  const item = props.item;
  const presentation = buildAttachmentUploadPresentation(item.asset.name, item.progress);
  const uploading = item.status === "uploading";
  const picture = item.status !== "failed" && isImageAttachment({ mimeType: item.asset.mimeType ?? undefined, name: item.asset.name });
  return (
    <View testID={`attachment-${item.id}`} class="max-w-[260px] flex-row items-center gap-2 rounded-(--radius-control) bg-card px-3 py-2" accessibilityLabel={uploading ? presentation.accessibilityLabel : `${item.asset.name}, ${attachmentLabel(item)}`}>
      {picture ? (
        <Button testID={`attachment-${item.id}-open`} accessibilityLabel={`Open ${item.asset.name}`} onPress={props.onOpen} class="active:opacity-70">
          <Thumbnail uri={item.asset.uri} name={item.asset.name} testID={`attachment-${item.id}-thumbnail`} />
        </Button>
      ) : null}
      {uploading && !picture ? <Spinner size={14} /> : !picture ? <Icon name={item.status === "failed" ? "alert-circle" : "attach"} size={16} tone={item.status === "failed" ? "seal" : "soft"} /> : null}
      <View class="min-w-0 flex-1">
        <Text class="text-sub line-clamp-1">{item.asset.name}</Text>
        <Text testID={`attachment-${item.id}-label`} class={item.status === "failed" ? "text-caption text-destructive line-clamp-2" : "text-caption text-muted-foreground line-clamp-1"}>{attachmentLabel(item)}</Text>
      </View>
      {item.status === "failed" && item.asset.selectionRetryable !== false ? (
        <Button testID={`attachment-${item.id}-retry`} accessibilityLabel={`Retry upload of ${item.asset.name}`} onPress={() => props.uploads.retry(item.id)} class="active:opacity-60"><Text class="text-sub font-semibold">Retry</Text></Button>
      ) : null}
      <Button
        testID={`attachment-${item.id}-remove`}
        accessibilityLabel={uploading ? presentation.cancelAccessibilityLabel : `Remove ${item.asset.name}`}
        onPress={() => props.uploads.remove(item.id)}
        class="active:opacity-60"
      >
        {uploading ? <Text class="text-sub font-semibold">{presentation.cancelLabel}</Text> : <Icon name="close" size={16} tone="soft" />}
      </Button>
    </View>
  );
}
