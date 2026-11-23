import { host, platform, signal, Text, View, type JSX } from "nsh";
import { Icon } from "../icons/Icon";
import type { UploadDocumentAsset } from "../../core/files/uploads";
import type { IncomingFiles } from "../more/files/incoming";

/** Where files can be dropped: a desktop (NSH's `onFileDrop`) or a browser (the page host's drop, web/host.js). */
const DROPS = platform.os === "web" || platform.desktop;

/**
 * A conversation and its composer as one place to drop files on (owner issue 3, 2026-11-23): files dragged over it
 * show "Drop to attach" over it, and dropped they become the composer's chips as picked ones do (`onFiles`, the
 * composer's attach). Text dragged in is the field's, as before. `testID` names the place (one per page).
 *
 * The desktops hear the drop from NSH (`onFileDrop`: the files' `file:` URLs). NSH 0.6.1's web build delivers no
 * drops (NSH #2867 adds them later), so in a browser the page host catches files dragged over the page and says which
 * place they are over by its `testID` (`files.dragOver`, `files.dropped`, with the files kept as picked ones are).
 * Only one of the two ever listens here: the web never sets `onFileDrop`, so a later NSH cannot deliver a drop twice.
 * On a phone (no drops in NSH 0.6.1) it is only the view around them.
 */
export function FileDrop(props: { testID: string; onFiles: () => ((incoming: IncomingFiles) => void) | null; class?: string; children: JSX.Element }) {
  if (!DROPS) return <View testID={props.testID} class={props.class ?? "flex-1"}>{props.children}</View>;
  const over = signal(false);
  const receive = (incoming: IncomingFiles) => {
    over.set(false);
    props.onFiles()?.(incoming);
  };
  if (platform.os === "web") {
    host.on("files.dragOver", (payload) => over.set((payload as { target?: string } | null)?.target === props.testID && props.onFiles() !== null));
    host.on("files.dropped", (payload) => {
      const drop = payload as { target?: string; files?: UploadDocumentAsset[] } | null;
      if (drop?.target === props.testID && drop.files?.length) receive({ assets: drop.files });
    });
    return <View testID={props.testID} class={props.class ?? "flex-1"}>{props.children}<DropTarget shown={over} /></View>;
  }
  return (
    <View testID={props.testID} class={props.class ?? "flex-1"}
      onFileDragEnter={() => over.set(props.onFiles() !== null)} onFileDragLeave={() => over.set(false)}
      onFileDrop={({ uris }) => receive({ uris })}>
      {props.children}
      <DropTarget shown={over} />
    </View>
  );
}

/** Over the place while files are dragged over it: the composer's surface, inked at its edge, with the attach glyph. */
function DropTarget(props: { shown: () => boolean }) {
  return (
    <If when={props.shown()}>
      <View testID="drop-target" class="absolute inset-2 items-center justify-center gap-2 rounded-2xl border-2 border-primary bg-card/90" style={{ pointerEvents: "none" }}>
        <Icon name="attach" size={28} color="foreground" />
        <Text class="text-sub font-semibold text-foreground">Drop to attach</Text>
      </View>
    </If>
  );
}
