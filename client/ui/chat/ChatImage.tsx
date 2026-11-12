import { cleanup, effect, Image, signal, untracked, View } from "nsh";
import { Button } from "nsh/base";
import { Icon } from "../icons/Icon";

/** A web or data URL, which shows as it is; anything else is a path on the computer. */
export const isDirectPicture = (src: string) => /^(https?:|data:image\/)/i.test(src);

/** Reads a path on the computer as a data URL (`codex_asset` through the chat's Session); null when it is no picture. */
export type PictureLoader = (path: string) => Promise<string | null>;

/**
 * A picture's source made showable, following `src` (a recycled row's next picture loads its own):
 * a web or data URL as it is, a path read through `load`. `retry` asks again after a failure.
 */
export function createPicture(src: () => string, load?: PictureLoader) {
  const url = signal<string | null>(null);
  const failed = signal(false);
  const attempt = signal(0);
  effect(() => {
    const source = src();
    attempt();
    let live = true;
    failed.set(false);
    url.set(null);
    if (isDirectPicture(source)) url.set(source);
    else if (!load) failed.set(true);
    else {
      void load(source.replace(/^file:\/\//i, ""))
        .then((found) => live && (found ? url.set(found) : failed.set(true)))
        .catch(() => live && failed.set(true));
    }
    cleanup(() => {
      live = false;
    });
  });
  return {
    url: () => url(),
    failed: () => failed(),
    /** A picture that loaded but would not draw. */
    fail: () => failed.set(true),
    retry: () => attempt.set(untracked(attempt) + 1),
  };
}

/** Where a picture sits outside a reply, which sets its frame (SPEC §1.7, §11). */
export type PictureFrame = "sent" | "tool";

/**
 * A picture in a chat (SPEC §1.7): a reply's (the default: the text's width, 4:3), a sent one (168 × 126), or a
 * tool call's thumbnail (96 × 170 tall, 200 × 125 wide, by the picture). A tap opens the viewer
 * (`onOpen`); one that failed to load keeps its frame with a refresh glyph, and a tap loads it again.
 */
export function ChatImage(props: { src: string; alt?: string; load?: PictureLoader; frame?: PictureFrame; width?: () => number; onOpen?: () => void }) {
  const picture = createPicture(() => props.src, props.load);
  const tall = signal(false);
  const box = () => {
    if (props.frame === "sent") return { width: 168, height: 126 };
    if (props.frame === "tool") return tall() ? { width: 96, height: 170 } : { width: 200, height: 125 };
    // A text view sizes what renderImage gives it to its content, so the picture's 100% alone came out
    // 0 wide (as K9's SKILL.md pictures): it takes the text's width, measured by the page.
    return { width: props.width?.() || undefined, aspectRatio: 4 / 3 };
  };
  const label = () => props.alt || "Picture";
  return (
    // A sent or tool picture is a radius-16 thumbnail on the canvas (FIDELITY-AUDIT item 18): no fill behind
    // it and no padding round it; only a picture still loading or failed shows a quiet tint in its frame.
    <View testID="chat-image" class={`overflow-hidden ${props.frame ? "rounded-[16px]" : "my-1 rounded-xl"}`} style={box()}>
      {picture.failed() ? (
        <Button testID="chat-image-retry" accessibilityLabel={`Load ${label()} again`} onPress={picture.retry} class="h-full w-full items-center justify-center bg-accent p-0 active:opacity-70">
          <Icon name="refresh" size={22} />
        </Button>
      ) : picture.url() ? (
        <Button testID="chat-image-open" accessibilityLabel={label()} disabled={!props.onOpen} onPress={() => props.onOpen?.()} class="h-full w-full p-0 active:opacity-80">
          <Image src={picture.url()!} accessibilityLabel={label()} onError={picture.fail}
            onLoad={({ size }) => tall.set(size.height > size.width)}
            style={{ width: "100%", height: "100%", objectFit: props.frame ? "cover" : "contain" }} />
        </Button>
      ) : (
        <View testID="chat-image-loading" class="h-full w-full bg-accent" />
      )}
    </View>
  );
}
