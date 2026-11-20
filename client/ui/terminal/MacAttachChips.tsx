import { Image, signal, Text, View } from "nsh";
import { Button } from "nsh/base";
import type { AttachmentQueueItem } from "../../core/files/attachmentUploadQueue";
import { buildAttachmentUploadPresentation } from "../../core/files/attachmentUploadPresentation";
import { isImageAttachment } from "../../core/files/imageSource";
import { FileName } from "../kit/FileName";
import { Icon } from "../kit/Icon";
import { Spinner } from "../kit/wait";
import { thumbnail } from "../more/files/host";
import { attachmentLabel } from "../more/files/upload";
import type { TerminalAttach } from "./attach";

/** A chip at the Mac's density (DESIGN-MAC.md §2.3.1): 44 at least, 320 at most, radius 10, a surface with its edge. */
const CHIP = "min-h-[44px] max-w-[320px] flex-row items-center gap-2.5 rounded-[10px] border-[0.5px] border-border bg-card px-3 py-1.5 shadow-[0_6px_18px_rgba(0,0,0,0.12)]";

/**
 * Mewla for Mac's terminal attachments: chips floating over the grid at its top right, 12 from the top and 16 from
 * the right, 8 apart, the newest at the bottom; the grid never resizes. A file that is ready leaves (its path is at
 * the prompt); a failed one stays with Retry and ×; a pick that could not start is a chip of its own.
 */
export function MacAttachChips(props: { attach: TerminalAttach }) {
  const uploads = props.attach.uploads;
  return (
    <View testID="terminal-attach-chips" class="absolute right-4 top-3 items-end gap-2" style={{ pointerEvents: "box-none" }}>
      {/* Keyed by the file: a chip is made once, its state read in the JSX. */}
      <Each of={uploads.items().filter((item) => item.status !== "ready")} key={(item: AttachmentQueueItem) => item.id}>{(item: () => AttachmentQueueItem) => <Chip item={item} attach={props.attach} />}</Each>
      <If when={props.attach.problem()}>
        <View testID="terminal-attach-problem" class={CHIP}>
          <Icon name="alert-circle" size={16} tone="seal" />
          <Text class="min-w-0 shrink text-[12.5px] leading-[17px] text-seal-ink line-clamp-2">{props.attach.problem()}</Text>
          <Button testID="terminal-attach-problem-close" accessibilityLabel="Dismiss" onPress={props.attach.dismissProblem} class="size-6 items-center justify-center rounded-md hover:bg-accent">
            <Icon name="close" size={14} tone="soft" />
          </Button>
        </View>
      </If>
    </View>
  );
}

function Chip(props: { item: () => AttachmentQueueItem; attach: TerminalAttach }) {
  const uploads = props.attach.uploads;
  const item = () => props.item();
  const failed = () => item().status === "failed";
  const uploading = () => item().status === "uploading";
  const name = item().asset.name;
  const picture = isImageAttachment({ mimeType: item().asset.mimeType ?? undefined, name });
  return (
    <View testID={`attachment-${item().id}`} class={CHIP}
      accessibilityLabel={uploading() ? buildAttachmentUploadPresentation(name, item().progress).accessibilityLabel : `${name}, ${attachmentLabel(item())}`}>
      <If when={failed()} else={<If when={picture} else={<If when={uploading()} else={<Icon name="attach" size={16} tone="soft" />}><Spinner size={14} /></If>}>
          <Thumbnail uri={item().asset.uri} name={name} testID={`attachment-${item().id}-thumbnail`} />
        </If>}><Icon name="alert-circle" size={16} tone="seal" /></If>
      <View class="min-w-0 shrink">
        <FileName name={name} class="text-[13px] leading-[17px] font-semibold text-foreground" />
        <Text testID={`attachment-${item().id}-label`} class={`text-[11.5px] leading-[15px] ${failed() ? "text-seal-ink line-clamp-2" : "text-muted-foreground line-clamp-1"}`}>{attachmentLabel(item())}</Text>
      </View>
      <If when={failed() && item().asset.selectionRetryable !== false}>
        <Button testID={`attachment-${item().id}-retry`} accessibilityLabel={`Retry upload of ${name}`} onPress={() => uploads.retry(item().id)} class="hover:opacity-70 active:opacity-60">
          <Text class="text-[12.5px] font-semibold text-foreground">Retry</Text>
        </Button>
      </If>
      <If when={failed()} else={<Button testID={`attachment-${item().id}-remove`} accessibilityLabel={buildAttachmentUploadPresentation(name, item().progress).cancelAccessibilityLabel}
          onPress={() => uploads.remove(item().id)} class="hover:opacity-70 active:opacity-60">
          <Text class="text-[12.5px] font-semibold text-muted-foreground">{buildAttachmentUploadPresentation(name, item().progress).cancelLabel}</Text>
        </Button>}>
        <Button testID={`attachment-${item().id}-remove`} accessibilityLabel={`Remove ${name}`} onPress={() => uploads.remove(item().id)} class="size-6 items-center justify-center rounded-md hover:bg-accent">
          <Icon name="close" size={14} tone="soft" />
        </Button>
      </If>
    </View>
  );
}

/** A picture's own, 28 with radius 6, in place of the spinner. */
function Thumbnail(props: { uri: string; name: string; testID: string }) {
  const src = signal<string | null>(null);
  thumbnail(props.uri, 84).then((url) => src.set(url), () => {});
  return (
    <View class="size-7 items-center justify-center overflow-hidden rounded-md bg-accent">
      <If when={src()} else={<Icon name="image" size={14} tone="soft" />}><Image testID={props.testID} src={src()!} class="size-7" style={{ objectFit: "cover" }} accessibilityLabel={props.name} /></If>
    </View>
  );
}
