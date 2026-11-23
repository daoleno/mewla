import { Text, View } from "nsh";
import { Button } from "nsh/base";
import { ATTACH_SOURCE_LOOK, type AttachSource } from "../../core/files/attachSources";
import { Icon } from "../icons/Icon";
import { composerSize } from "../kit/density";
import { Sheet } from "../kit/Sheet";
import type { AttachmentUploads } from "../more/files/upload";

/**
 * What a composer's "+" attaches into (`Composer`'s `attach`): the composer's chips, whether "+" can be pressed now
 * (connected, the page not busy), and where a file that could not be attached is said (one line, the upload's own
 * words).
 */
export type ComposerAttach = {
  uploads: AttachmentUploads;
  enabled: () => boolean;
  onError: (message: string) => void;
};

/** The composer's "+": 44 × 52 inside the field's pill, at its left. */
export function AttachButton(props: { enabled: () => boolean; onPress: () => void }) {
  const plus = composerSize().plus;
  return (
    <Button testID="composer-plus" accessibilityLabel="Attach" disabled={!props.enabled()} onPress={props.onPress}
      class={`${plus.box} items-center justify-center active:opacity-60 disabled:opacity-40`}>
      <Icon name="add" size={plus.icon} color="muted-foreground" />
    </Button>
  );
}

/**
 * The attach sheet (a phone or a tablet; docs/brain-and-work.md "Attachments"): one row of large tiles, a glyph and
 * one word each, Photos first. A tile closes the sheet and opens its system picker; the sheet closes on the scrim,
 * Back and a swipe. On wide it is the centred dialog every Sheet is there.
 */
export function AttachSheet(props: { open: () => boolean; sources: () => AttachSource[]; onPick: (source: AttachSource) => void; onClose: () => void }) {
  return (
    <Sheet testID="attach-sheet" open={props.open} onClose={props.onClose}>
      <View class="flex-row gap-2.5 px-4 pb-2">
        <Each of={props.sources()} key={(source) => source}>{(source) => (
          <Button testID={`attach-${source()}`} accessibilityLabel={ATTACH_SOURCE_LOOK[source()].label} onPress={() => props.onPick(source())}
            class="h-[96px] min-w-0 flex-1 items-center justify-center gap-2 rounded-2xl bg-sheet-surface active:opacity-70">
            <Icon name={ATTACH_SOURCE_LOOK[source()].icon} size={28} color="foreground" />
            <Text class="text-sub font-medium text-foreground">{ATTACH_SOURCE_LOOK[source()].label}</Text>
          </Button>
        )}</Each>
      </View>
    </Sheet>
  );
}
