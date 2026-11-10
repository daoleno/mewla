import { Text, View } from "nsh";
import { Button } from "nsh/base";
import type { AttachmentQueueItem } from "../../../core/files/attachmentUploadQueue";
import { buildAttachmentUploadPresentation } from "../../../core/files/attachmentUploadPresentation";
import { Icon } from "../kit/Icon";
import { Spinner } from "../kit/kit";
import { attachmentLabel, type AttachmentUploads } from "./upload";

/** The chips above a composer: one per picked file, with its progress, Cancel, Retry or Remove. */
export function AttachmentTray(props: { uploads: AttachmentUploads }) {
  return (
    <View class="flex-row flex-wrap gap-2 px-3 py-2">
      {props.uploads.items().map((item) => <Chip item={item} uploads={props.uploads} />)}
    </View>
  );
}

function Chip(props: { item: AttachmentQueueItem; uploads: AttachmentUploads }) {
  const item = props.item;
  const presentation = buildAttachmentUploadPresentation(item.asset.name, item.progress);
  const uploading = item.status === "uploading";
  return (
    <View testID={`attachment-${item.id}`} class="max-w-[260px] flex-row items-center gap-2 rounded-xl border border-border bg-card px-3 py-2" accessibilityLabel={uploading ? presentation.accessibilityLabel : `${item.asset.name}, ${attachmentLabel(item)}`}>
      {uploading ? <Spinner size={14} /> : <Icon name={item.status === "failed" ? "alert-circle" : "attach"} size={16} tone={item.status === "failed" ? "seal" : "soft"} />}
      <View class="min-w-0 flex-1">
        <Text class="text-sm line-clamp-1">{item.asset.name}</Text>
        <Text testID={`attachment-${item.id}-label`} class={item.status === "failed" ? "text-xs text-destructive line-clamp-2" : "text-xs text-muted-foreground line-clamp-1"}>{attachmentLabel(item)}</Text>
      </View>
      {item.status === "failed" && item.asset.selectionRetryable !== false ? (
        <Button testID={`attachment-${item.id}-retry`} onPress={() => props.uploads.retry(item.id)} class="active:opacity-60"><Text class="text-sm font-semibold">Retry</Text></Button>
      ) : null}
      <Button
        testID={`attachment-${item.id}-remove`}
        accessibilityLabel={uploading ? presentation.cancelAccessibilityLabel : `Remove ${item.asset.name}`}
        onPress={() => props.uploads.remove(item.id)}
        class="active:opacity-60"
      >
        {uploading ? <Text class="text-sm font-semibold">{presentation.cancelLabel}</Text> : <Icon name="close" size={16} tone="soft" />}
      </Button>
    </View>
  );
}
