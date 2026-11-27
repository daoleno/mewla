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
 * The Mac hears the drop from NSH (`onFileDrop`): a local file (Finder, the screenshot thumbnail) as its `file:` URL,
 * which `attachIncoming` reads through the Mac's host (`readLocalFiles`: NSH 0.7.1 cannot open the thumbnail's
 * folder), and a promised one (an image dragged out of Safari or Chrome, Mail's attachments) with its contents in
 * `files`, the shape of a pick (NSH 0.7, longbridge/nsh#2964). In a browser
 * the page host catches files dragged over the page and says which place they are over by its `testID`
 * (`files.dragOver`, `files.dropped`, with the files kept as picked ones are), and refuses a drop that misses every
 * place, so the browser never opens the file over the app. NSH 0.7's web build delivers drops too (#2867), but
 * `nsh/test` cannot drop contents to test it with: the web keeps its own, and never sets `onFileDrop`, so a drop is
 * never delivered twice. On a phone (NSH 0.7 drops there are untried) it is only the view around them.
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
      onFileDrop={(e) => receive({ dropped: e.files, uris: e.uris })}>
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
