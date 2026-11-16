import { cleanup, Clipboard, colorScheme, computed, effect, safeArea, signal, Text, untracked, View, WebView, type JSX } from "nsh";
import { Button, TextView } from "nsh/base";
import { mermaidSvgDocument, prepareMermaid, type MermaidMode, type MermaidResult } from "../../core/mermaid/mermaid";
import { Icon } from "../kit/Icon";
import { GuardedDialog } from "../shell/AppSheet";
import { viewerBaseUrl } from "../viewerBase";
import { MERMAID_PALETTES, mermaidRenderer } from "./engine";

/** The tallest a diagram is drawn in a reply or a file; a taller one is fitted inside and opens whole on a tap. */
const INLINE_MAX_HEIGHT = 420;
const INLINE_MIN_HEIGHT = 96;
const ZOOMS = [0.5, 0.75, 1, 1.5, 2, 3];

/**
 * A fenced `mermaid` block (core/mermaid/mermaid.ts), drawn as its diagram on the card a code block has, its header
 * the same ("mermaid", Copy) with a button that opens it full screen. Until it is drawn, and while the reply it
 * is in still streams, it is its code; a diagram mermaid cannot read is its code with mermaid's words under it.
 */
export function MermaidBlock(props: { code: string; streaming?: () => boolean; asCode?: (code: string) => JSX.Element }) {
  const mode = (): MermaidMode => (colorScheme() === "dark" ? "dark" : "light");
  const prepared = prepareMermaid(props.code);
  const result = signal<MermaidResult | null>(null);
  effect(() => {
    if (props.streaming?.()) return;
    if (!prepared.ok) return result.set({ ok: false, error: prepared.error });
    const shown = mode();
    const renderer = mermaidRenderer();
    const kept = renderer.cached(prepared.source, shown);
    if (kept) return result.set(kept);
    let live = true;
    cleanup(() => {
      live = false;
    });
    void renderer.render(prepared.source, shown).then((drawn) => {
      if (live) result.set(drawn);
    });
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
    <If when={drawn()} else={<View testID="mermaid-code">
        {(props.asCode ?? plainCode)(props.code)}
        <If when={failure()}><Text testID="mermaid-error" class="px-1 pb-1 font-mono text-[12px] leading-[18px] text-muted-foreground">{failure()}</Text></If>
      </View>}>
      <Diagram code={props.code} drawn={() => drawn()!} mode={mode} />
    </If>
  );
}

/** A code block as Markdown draws one by default: what a file's preview shows. */
export function plainCode(code: string, language = "mermaid"): JSX.Element {
  return <TextView document={{ blocks: [{ type: "codeBlock", code, language }] }} selectable />;
}

function Diagram(props: { code: string; drawn: () => Extract<MermaidResult, { ok: true }>; mode: () => MermaidMode }) {
  const width = signal(0);
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
  const background = () => MERMAID_PALETTES[props.mode()].surface;
  // Fitted to the card's width, no taller than INLINE_MAX_HEIGHT and never drawn larger than mermaid drew it.
  const height = () => {
    const { width: w, height: h } = props.drawn();
    const room = Math.max(0, width() - 32);
    const fitted = room ? Math.min(h, (room / w) * h) : Math.min(h, INLINE_MAX_HEIGHT);
    return Math.round(Math.max(INLINE_MIN_HEIGHT, Math.min(INLINE_MAX_HEIGHT, fitted)));
  };
  const page = computed(() => mermaidSvgDocument(props.drawn(), background()));
  return (
    <View testID="mermaid-diagram" class="my-1 overflow-hidden rounded-(--radius-group) bg-card" onLayout={({ bounds }) => width.set(bounds.size.width)}>
      <View class="h-12 flex-row items-center gap-1 pl-4 pr-1">
        <Text class="min-w-0 flex-1 select-none font-mono text-code text-muted-foreground line-clamp-1">mermaid</Text>
        <Button testID="mermaid-copy" accessibilityLabel={copied() ? "Copied" : "Copy"} onPress={copy} class="size-11 items-center justify-center rounded-full active:bg-accent">
          <Icon name={copied() ? "check" : "copy"} size={18} tone="soft" />
        </Button>
        <Button testID="mermaid-open" accessibilityLabel="Open diagram" onPress={() => open.set(true)} class="size-11 items-center justify-center rounded-full active:bg-accent">
          <Icon name="expand" size={18} tone="soft" />
        </Button>
      </View>
      <View class="mx-4 mb-4" style={{ height: height() }}>
        <WebView testID="mermaid-view" class="flex-1" html={page()} baseUrl={viewerBaseUrl} scrollable={false} incognito accessibilityLabel="Diagram" />
        {/* Over the web view: a tap opens it, and a drag scrolls the chat rather than the picture. */}
        <Button testID="mermaid-tap" accessibilityLabel="Open diagram" onPress={() => open.set(true)} class="absolute inset-0" />
      </View>
      <DiagramViewer open={open} drawn={props.drawn} background={background} />
    </View>
  );
}

/** A diagram full screen: fitted at first, then − and + zoom it in steps of its drawn size and it pans by scrolling; Back closes it. */
function DiagramViewer(props: { open: ReturnType<typeof signal<boolean>>; drawn: () => Extract<MermaidResult, { ok: true }>; background: () => string }) {
  const zoom = signal<number | null>(null);
  effect(() => {
    if (!props.open()) zoom.set(null);
  });
  const step = (by: 1 | -1) => {
    const now = untracked(zoom);
    // From fitted: + is the drawn size, − half of it (fitted is smaller than drawn unless the diagram is small).
    const at = now === null ? ZOOMS.indexOf(by > 0 ? 1 : 0.5) : ZOOMS.indexOf(now) + by;
    zoom.set(ZOOMS[Math.max(0, Math.min(ZOOMS.length - 1, at))]);
  };
  const page = computed(() => mermaidSvgDocument(props.drawn(), props.background(), zoom()));
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
