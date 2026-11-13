import { Image, signal, Text, View } from "nsh";
import { Button } from "nsh/base";
import { GuardedSheet as Sheet } from "../../../shell/AppSheet";
import { failureText } from "../../../../core/more/daemon";
import { Icon } from "../../../kit/Icon";
import { Spinner } from "../../../kit/kit";

/**
 * The pictures of one Skill copy, read from that copy only. `read` answers a
 * file's data URL; each picture is read once per copy and kept.
 */
export type ImageReader = (path: string) => Promise<string>;

export function cachedReader(read: ImageReader): ImageReader {
  const cache = new Map<string, Promise<string>>();
  return (path) => {
    let pending = cache.get(path);
    if (!pending) {
      pending = read(path);
      pending.catch(() => cache.delete(path));
      cache.set(path, pending);
    }
    return pending;
  };
}

/** A picture a Skill's Markdown shows, from the copy itself; a tap opens it larger. */
export function SkillImage(props: { path: string | null; alt?: string; problem?: string; read: ImageReader; onOpen: (path: string) => void; width?: () => number }) {
  const src = signal("");
  const error = signal(props.problem ?? "");
  if (props.path) {
    props.read(props.path)
      .then((url) => src.set(url))
      .catch((reason: unknown) => error.set(failureText(reason, "This Skill image is unavailable.")));
  }
  return (
    // A text view sizes what renderImage gives it to its content, so `w-full` alone came out 0 wide (K9):
    // the picture takes the text's width, measured by the page.
    <View class="my-1" style={{ width: props.width?.() || undefined }}>
      <If when={src()} else={<If when={error()} else={<Spinner size={16} />}><Text class="text-caption text-muted-foreground">{`${props.alt ? `${props.alt}: ` : ""}${error()}`}</Text></If>}><View role="button" accessibilityLabel={props.alt || props.path || "Image"} accessibilityHint="Opens the image" onPress={() => props.path && props.onOpen(props.path)} class="active:opacity-80">
          <Image src={src()} class="h-48 w-full rounded-[14px]" style={{ objectFit: "contain" }} accessibilityLabel={props.alt || props.path || "Image"} />
        </View></If>
    </View>
  );
}

/** One picture of the copy at a time, full width, with the copy's other pictures before and after it. */
export function SkillImageViewer(props: { images: readonly string[]; start: string | null; read: ImageReader; onClose: () => void }) {
  return (
    <Sheet open={props.start !== null} onOpenChange={({ open }) => !open && props.onClose()} closeOnSwipe popupClass="h-[94%] rounded-t-(--radius-sheet) bg-sheet pb-8 pt-3 shadow-float" sidePadding={16} backdropClass="bg-scrim">
      <If when={props.start !== null}><ViewerBody images={props.images} start={props.start!} read={props.read} onClose={props.onClose} /></If>
    </Sheet>
  );
}

function ViewerBody(props: { images: readonly string[]; start: string; read: ImageReader; onClose: () => void }) {
  const list = props.images.includes(props.start) ? props.images : [props.start, ...props.images];
  const index = signal(Math.max(0, list.indexOf(props.start)));
  const src = signal("");
  const error = signal("");
  let shown = -1;
  const load = (at: number) => {
    shown = at;
    src.set("");
    error.set("");
    props.read(list[at]!)
      .then((url) => shown === at && src.set(url))
      .catch((reason: unknown) => shown === at && error.set(failureText(reason, "This Skill image is unavailable.")));
  };
  load(index());
  const go = (step: number) => {
    const next = index() + step;
    if (next < 0 || next >= list.length) return;
    index.set(next);
    load(next);
  };
  return (
    <View testID="skill-image-viewer" class="flex-1 gap-3">
      <View class="flex-row items-center gap-3">
        <View class="min-w-0 flex-1">
          <Text class="font-mono text-code line-clamp-1">{list[index()]!}</Text>
          <If when={list.length > 1}><Text class="text-caption text-muted-foreground">{`${index() + 1} of ${list.length}`}</Text></If>
        </View>
        <Button accessibilityLabel="Close image" onPress={props.onClose} class="size-11 items-center justify-center rounded-full active:bg-accent">
          <Icon name="close" size={20} />
        </Button>
      </View>
      <View class="flex-1 items-center justify-center" panActivation={{ activeOffsetX: 24, failOffsetY: 14 }} onPanEnd={(event) => !event.cancelled && Math.abs(event.translation.x) > 60 && go(event.translation.x < 0 ? 1 : -1)}>
        <If when={src()} else={<If when={error()} else={<Spinner />}><Text class="text-sub text-muted-foreground">{error()}</Text></If>}><Image src={src()} class="h-full w-full" style={{ objectFit: "contain" }} accessibilityLabel={list[index()]!} /></If>
      </View>
      <If when={list.length > 1}><View class="flex-row justify-between">
          <Button testID="image-previous" accessibilityLabel="Previous image" disabled={index() === 0} onPress={() => go(-1)} class="size-11 items-center justify-center rounded-full bg-secondary active:opacity-70 disabled:opacity-40">
            <Icon name="chevron-left" size={20} />
          </Button>
          <Button testID="image-next" accessibilityLabel="Next image" disabled={index() === list.length - 1} onPress={() => go(1)} class="size-11 items-center justify-center rounded-full bg-secondary active:opacity-70 disabled:opacity-40">
            <Icon name="chevron-right" size={20} />
          </Button>
        </View></If>
    </View>
  );
}
