import { cleanup, computed, effect, keymap, safeArea, signal, Text, untracked, View, type Signal } from "nsh";
import { Button, Carousel, CarouselContent, CarouselItem } from "nsh/base";
import type { ThreadPicture } from "../../core/timeline/threadPictures";
import { ZoomImage } from "../more/files/FilePreview";
import { GuardedDialog } from "../shell/AppSheet";
import { Icon } from "../icons/Icon";
import { Spinner } from "../kit/kit";
import { createPicture, type PictureLoader } from "./ChatImage";
import { askPictureSupport, pictureCan, PictureNote, picturesSupported, runPictureAction, saveLabel } from "./pictureActions";
import type { PictureAction } from "../../core/files/pictureSave";

// The close side matches the actions' side, so "2 of 3" stays in the middle.
const actionsWidth = 44 * (1 + Number(pictureCan.share) + Number(pictureCan.copy));

/** One picture of the viewer, read once it is the one shown or beside it; pinch and double tap zoom it. */
function Slide(props: { picture: ThreadPicture; near: () => boolean; load?: PictureLoader }) {
  // Asked for once it comes near, and kept after.
  let wanted = false;
  const src = computed(() => (wanted ||= props.near()) ? props.picture.src : "");
  // The viewer zooms: it reads the original, where a thread's row reads it phone-sized.
  const picture = createPicture(src, (path) => (path ? props.load?.(path, true) ?? Promise.resolve(null) : Promise.resolve(null)));
  return (
    <View testID="picture-viewer-slide" class="flex-1">
      <If when={picture.url() && !picture.failed()} else={<View class="flex-1 items-center justify-center">
          <If when={picture.failed() && src()} else={<Spinner size={24} />}><Button testID="picture-viewer-retry" accessibilityLabel={`Load ${props.picture.alt || "the picture"} again`} onPress={picture.retry}
              class="size-14 items-center justify-center rounded-full bg-white/15 active:opacity-70">
              <Icon name="refresh" size={24} color="seal-foreground" />
            </Button></If>
        </View>}><ZoomImage testID="picture-viewer-image" src={picture.url()!} name={props.picture.alt || "Picture"} onFail={picture.fail} /></If>
    </View>
  );
}

/**
 * The picture viewer (SPEC §1.7, §11): black in both themes, × and "2 of 3" on top, previous and next
 * at the sides (also the arrow keys and a swipe), double tap and pinch to zoom. It steps through every
 * picture of the thread in order; `open` names the one shown (its key), null while closed. Back closes it.
 * Save and Share (Copy where the clipboard holds pictures) on the right of the top bar act on the original
 * of the picture shown (pictureActions.ts); what they end with shows at the foot for a moment.
 */
export function PictureViewer(props: { pictures: () => ThreadPicture[]; open: Signal<string | null>; load?: PictureLoader }) {
  const index = computed(() => Math.max(0, props.pictures().findIndex((picture) => picture.key === props.open())));
  const count = () => props.pictures().length;
  const go = (step: number) => {
    const all = untracked(props.pictures);
    const next = all[untracked(index) + step];
    if (next) props.open.set(next.key);
  };
  // The picture it shows went (a new chat, another thread): it closes rather than show another.
  effect(() => {
    const key = props.open();
    if (key !== null && !props.pictures().some((picture) => picture.key === key)) props.open.set(null);
  });
  effect(() => {
    if (props.open() === null) return;
    keymap.bind("left", () => go(-1), { when: "always" });
    keymap.bind("right", () => go(1), { when: "always" });
  });
  // Whether this app build can save and share (an older one shows the viewer as it was).
  effect(() => {
    if (props.open() !== null) void askPictureSupport();
  });
  // The action running and the one that just worked (its icon a check for a moment).
  const busy = signal<PictureAction | null>(null);
  const done = signal<PictureAction | null>(null);
  let doneTimer: ReturnType<typeof setTimeout> | undefined;
  cleanup(() => clearTimeout(doneTimer));
  const act = async (action: PictureAction) => {
    const shown = untracked(props.pictures)[untracked(index)];
    if (!shown || untracked(busy)) return;
    busy.set(action);
    const worked = await runPictureAction(action, shown, props.load);
    busy.set(null);
    // Share's sheet is its own answer; Save and Copy show a check.
    if (!worked || action === "share") return;
    done.set(action);
    clearTimeout(doneTimer);
    doneTimer = setTimeout(() => done.set(null), 1500);
  };
  const ACT = {
    save: { icon: "download", label: saveLabel },
    share: { icon: "share", label: "Share" },
    copy: { icon: "copy", label: "Copy" },
  } as const;
  const Act = (a: { action: PictureAction }) => (
    <Button testID={`picture-viewer-${a.action}`} accessibilityLabel={ACT[a.action].label} disabled={count() === 0 || busy() !== null} onPress={() => void act(a.action)}
      class="size-11 items-center justify-center rounded-full disabled:opacity-50 active:opacity-70">
      <If when={busy() === a.action} else={<Icon name={done() === a.action ? "check" : ACT[a.action].icon} size={22} color="seal-foreground" />}><Spinner size={18} tone="onBlack" /></If>
    </Button>
  );
  const Side = (side: { step: -1 | 1 }) => (
    <Button testID={side.step < 0 ? "picture-viewer-previous" : "picture-viewer-next"} accessibilityLabel={side.step < 0 ? "Previous picture" : "Next picture"}
      disabled={!props.pictures()[index() + side.step]} onPress={() => go(side.step)}
      class={`absolute top-1/2 -mt-[22px] size-11 items-center justify-center rounded-full bg-white/15 disabled:opacity-30 active:opacity-70 ${side.step < 0 ? "left-3" : "right-3"}`}>
      {/* White in both themes: the viewer is black in both. */}
      <Icon name={side.step < 0 ? "chevron-left" : "chevron-right"} size={22} color="seal-foreground" />
    </Button>
  );
  return (
    <GuardedDialog testID="picture-viewer" open={props.open() !== null} onOpenChange={({ open }) => !open && props.open.set(null)}
      presentation="fullScreen" animation="fade" accessibilityLabel="Picture" class="flex-1 bg-black" statusBar={{ foreground: "light" }}>
      <View class="flex-1 bg-black" style={{ paddingTop: safeArea().top, paddingBottom: safeArea().bottom + 48 }}>
        <View class="h-[52px] flex-row items-center px-2">
          <View class="flex-row items-center" style={{ width: actionsWidth }}>
            <Button testID="picture-viewer-close" accessibilityLabel="Close" onPress={() => props.open.set(null)} class="size-11 items-center justify-center rounded-full active:opacity-70">
              <Icon name="close" size={22} color="seal-foreground" />
            </Button>
          </View>
          <Text testID="picture-viewer-count" class="flex-1 text-center text-sub font-semibold text-white">{count() > 0 ? `${index() + 1} of ${count()}` : ""}</Text>
          <View class="flex-row items-center" style={{ width: actionsWidth }}>
            <If when={picturesSupported()}><Act action="save" /><If when={pictureCan.share}><Act action="share" /></If><If when={pictureCan.copy}><Act action="copy" /></If></If>
          </View>
        </View>
        <View class="flex-1">
          <Carousel class="flex-1" index={index()} onChange={({ index: shown }) => { const picture = untracked(props.pictures)[shown]; if (picture) props.open.set(picture.key); }}
            accessibilityLabel="Pictures in this chat">
            <CarouselContent class="flex-1">
              {props.pictures().map((picture, at) => (
                <CarouselItem class="h-full w-full" accessibilityLabel={`${at + 1} of ${props.pictures().length}${picture.alt ? `: ${picture.alt}` : ""}`}>
                  <Slide picture={picture} near={() => Math.abs(index() - at) <= 1} load={props.load} />
                </CarouselItem>
              ))}
            </CarouselContent>
          </Carousel>
          <Side step={-1} />
          <Side step={1} />
          <PictureNote testID="picture-viewer-note" class="absolute bottom-4 left-4 right-4" />
        </View>
      </View>
    </GuardedDialog>
  );
}
