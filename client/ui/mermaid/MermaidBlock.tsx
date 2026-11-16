import { cleanup, Clipboard, colorScheme, computed, effect, safeArea, Scroll, signal, Text, untracked, View, WebView, type JSX } from "nsh";
import { Button, TextView } from "nsh/base";
import { drawMermaid, mermaidInlineLayout, mermaidSvgDocument, prepareMermaid, type MermaidDrawing, type MermaidMode, type MermaidPalette } from "../../core/mermaid/mermaid";
import { Icon } from "../kit/Icon";
import { GuardedDialog } from "../shell/AppSheet";
import { viewerBaseUrl } from "../viewerBase";
import { MERMAID_PALETTES } from "./palette";

/** The tallest a diagram is shown in a reply or a file; a taller one is cut there and opens whole on a tap. */
const INLINE_MAX_HEIGHT = 420;
/** The card's side padding, either side of the diagram. */
const SIDE = 16;
const ZOOMS = [0.5, 0.75, 1, 1.5, 2, 3];

/**
 * A fenced `mermaid` block (core/mermaid/mermaid.ts), drawn as its diagram on the card a code block has, its header
 * the same ("mermaid", Copy) with a button that opens it full screen. Laid out for the card's width, and shown at a
 * scale its labels stay legible at (wider: it scrolls sideways; taller: it is cut, and opens whole). While the reply
 * it is in still streams it is its code, drawn once when it ends; a diagram Pele cannot read is its code with Pele's
 * words under it. Light and dark are the same drawing, coloured by the view.
 */
export function MermaidBlock(props: { code: string; streaming?: () => boolean; asCode?: (code: string) => JSX.Element }) {
  const mode = (): MermaidMode => (colorScheme() === "dark" ? "dark" : "light");
  const prepared = prepareMermaid(props.code);
  // The card's width less its sides, rounded so a pixel's change draws nothing anew. Drawn only once it is known.
  const room = signal(0);
  const measured = (width: number) => {
    const next = Math.max(160, Math.round((width - 2 * SIDE) / 8) * 8);
    if (width > 0 && next !== untracked(room)) room.set(next);
  };
  const result = computed(() => {
    if (props.streaming?.() || !room()) return null;
    if (!prepared.ok) return { ok: false as const, error: prepared.error };
    return drawMermaid(prepared.source, room());
  });
  const drawn = computed(() => {
    const value = result();
    return value?.ok ? value : null;
  });
  const failure = () => {
    const value = result();
    return value && !value.ok ? value.error : "";
  };
  return (
    <View onLayout={({ bounds }) => measured(bounds.size.width)}>
      <If when={drawn()} else={<View testID="mermaid-code">
          {(props.asCode ?? plainCode)(props.code)}
          <If when={failure()}><View class="px-1 pb-1"><Text testID="mermaid-error" class="font-mono text-[12px] leading-[18px] text-muted-foreground">{failure()}</Text></View></If>
        </View>}>
        <Diagram code={props.code} source={prepared.ok ? prepared.source : ""} drawn={() => drawn()!} room={room} palette={() => MERMAID_PALETTES[mode()]} />
      </If>
    </View>
  );
}

/** A code block as Markdown draws one by default: what a file's preview shows. */
export function plainCode(code: string, language = "mermaid"): JSX.Element {
  return <TextView document={{ blocks: [{ type: "codeBlock", code, language }] }} selectable />;
}

function Diagram(props: { code: string; source: string; drawn: () => MermaidDrawing; room: () => number; palette: () => MermaidPalette }) {
  const open = signal(false);
  const copied = signal(false);
  let timer: ReturnType<typeof setTimeout> | undefined;
  cleanup(() => clearTimeout(timer));
  const copy = () => {
    void Clipboard.writeText(props.code).then(() => {
      copied.set(true);
      clearTimeout(timer);
      timer = setTimeout(() => copied.set(false), 1500);
    }).catch(() => {});
  };
  // Fitted to the card while its labels stay legible; a wider one at the legible scale scrolls sideways, a taller one
  // is cut at INLINE_MAX_HEIGHT (core mermaidInlineLayout).
  const layout = computed(() => mermaidInlineLayout(props.drawn(), props.room(), INLINE_MAX_HEIGHT));
  const page = computed(() => mermaidSvgDocument(props.drawn(), props.palette(), layout().scale, true));
  const Frame = () => (
    <View style={{ width: layout().width, height: layout().frameHeight }}>
      <WebView testID="mermaid-view" class="flex-1" html={page()} baseUrl={viewerBaseUrl} scrollable={false} incognito accessibilityLabel="Diagram" />
      {/* Over the web view: a tap opens it, and a drag scrolls the chat (or the frame) rather than the picture. */}
      <Button testID="mermaid-tap" accessibilityLabel="Open diagram" onPress={() => open.set(true)} class="absolute inset-0" />
    </View>
  );
  return (
    <View testID="mermaid-diagram" class="my-1 overflow-hidden rounded-(--radius-group) bg-card">
      <View class="h-12 flex-row items-center gap-1 pl-4 pr-1">
        <Text class="min-w-0 flex-1 select-none font-mono text-code text-muted-foreground line-clamp-1">mermaid</Text>
        <Button testID="mermaid-copy" accessibilityLabel={copied() ? "Copied" : "Copy"} onPress={copy} class="size-11 items-center justify-center rounded-full active:bg-accent">
          <Icon name={copied() ? "check" : "copy"} size={18} tone="soft" />
        </Button>
        <Button testID="mermaid-open" accessibilityLabel="Open diagram" onPress={() => open.set(true)} class="size-11 items-center justify-center rounded-full active:bg-accent">
          <Icon name="expand" size={18} tone="soft" />
        </Button>
      </View>
      <View testID={layout().cropped ? "mermaid-cropped" : "mermaid-whole"} class="mb-4">
        <If when={layout().scrolls} else={<View testID="mermaid-fitted" class="mx-4 items-center"><Frame /></View>}>
          <Scroll horizontal testID="mermaid-scroll" class="grow-0" contentClass="px-4" keyboardShouldPersistTaps="handled"><Frame /></Scroll>
        </If>
      </View>
      <DiagramViewer open={open} source={props.source} fallback={props.drawn} palette={props.palette} />
    </View>
  );
}

/** A diagram full screen, as written: fitted at first, then − and + zoom it in steps of its drawn size and it pans by scrolling; Back closes it. */
function DiagramViewer(props: { open: ReturnType<typeof signal<boolean>>; source: string; fallback: () => MermaidDrawing; palette: () => MermaidPalette }) {
  const zoom = signal<number | null>(null);
  // Full screen it is laid out as written (a `flowchart LR` stays across), drawn only once it is opened.
  const whole = computed((): MermaidDrawing | null => {
    if (!props.open()) return null;
    const drawn = drawMermaid(props.source, null);
    return drawn.ok ? drawn : props.fallback();
  });
  effect(() => {
    if (!props.open()) zoom.set(null);
  });
  const step = (by: 1 | -1) => {
    const now = untracked(zoom);
    // From fitted: + is the drawn size, − half of it (fitted is smaller than drawn unless the diagram is small).
    const at = now === null ? ZOOMS.indexOf(by > 0 ? 1 : 0.5) : ZOOMS.indexOf(now) + by;
    zoom.set(ZOOMS[Math.max(0, Math.min(ZOOMS.length - 1, at))]);
  };
  const page = computed(() => (whole() ? mermaidSvgDocument(whole()!, props.palette(), zoom()) : ""));
  return (
    <GuardedDialog testID="mermaid-viewer" open={props.open()} onOpenChange={({ open }) => !open && props.open.set(false)}
      presentation="fullScreen" animation="fade" accessibilityLabel="Diagram" class="flex-1 bg-card">
      <View class="flex-1 bg-card" style={{ paddingTop: safeArea().top, paddingBottom: safeArea().bottom }}>
        <View class="h-[52px] flex-row items-center gap-1 px-2">
          <Button testID="mermaid-viewer-close" accessibilityLabel="Close" onPress={() => props.open.set(false)} class="size-11 items-center justify-center rounded-full active:bg-accent">
            <Icon name="close" size={22} tone="ink" />
          </Button>
          <View class="flex-1" />
          <Button testID="mermaid-zoom-out" accessibilityLabel="Zoom out" disabled={zoom() === ZOOMS[0]} onPress={() => step(-1)} class="size-11 items-center justify-center rounded-full active:bg-accent disabled:opacity-30">
            <Icon name="remove" size={22} tone="ink" />
          </Button>
          <Button testID="mermaid-zoom-fit" accessibilityLabel="Fit" disabled={zoom() === null} onPress={() => zoom.set(null)} class="h-11 items-center justify-center rounded-full px-3 active:bg-accent disabled:opacity-30">
            <Text class="text-sub font-semibold">{zoom() === null ? "Fit" : `${Math.round(zoom()! * 100)}%`}</Text>
          </Button>
          <Button testID="mermaid-zoom-in" accessibilityLabel="Zoom in" disabled={zoom() === ZOOMS[ZOOMS.length - 1]} onPress={() => step(1)} class="size-11 items-center justify-center rounded-full active:bg-accent disabled:opacity-30">
            <Icon name="add" size={22} tone="ink" />
          </Button>
        </View>
        <WebView testID="mermaid-viewer-view" class="flex-1" html={page()} baseUrl={viewerBaseUrl} incognito accessibilityLabel="Diagram" />
      </View>
    </GuardedDialog>
  );
}
