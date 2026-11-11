import { Image, signal, Text, View } from "nsh";

/**
 * An image in a reply. A web or data URL shows as it is; a path is a file on
 * the computer, read through the Session (`load`, `codex_asset`). One that
 * can't load shows its alt text.
 */
export function ChatImage(props: { src: string; alt?: string; load?: (path: string) => Promise<string | null> }) {
  const direct = /^(https?:|data:image\/)/i.test(props.src);
  const src = signal<string | null>(direct ? props.src : null);
  const failed = signal(false);
  if (!direct && props.load) {
    void props.load(props.src.replace(/^file:\/\//i, ""))
      .then((url) => (url ? src.set(url) : failed.set(true)))
      .catch(() => failed.set(true));
  } else if (!direct) failed.set(true);
  return (
    <View testID="chat-image" class="my-1 overflow-hidden rounded-xl">
      {src() && !failed() ? (
        <Image src={src()!} accessibilityLabel={props.alt || "Image"} onError={() => failed.set(true)} style={{ width: "100%", aspectRatio: 4 / 3, objectFit: "contain" }} />
      ) : failed() ? (
        <Text class="rounded-xl border border-border px-3 py-2 text-sm text-muted-foreground">{props.alt || props.src}</Text>
      ) : (
        <View class="h-40 rounded-xl bg-accent" />
      )}
    </View>
  );
}
