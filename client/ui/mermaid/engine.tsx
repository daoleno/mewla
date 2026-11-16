import { signal, View, WebView, type WebViewRef } from "nsh";
import { createMermaidRenderer, mermaidEngineDocument, mermaidThemeVariables, type MermaidMode, type MermaidPalette, type MermaidRenderer } from "../../core/mermaid/mermaid";
import { viewerBaseUrl } from "../viewerBase";
import theme from "../theme.json";

/**
 * The app's one mermaid engine (core/mermaid/mermaid.ts): a web view that runs mermaid's own script, bundled
 * (`mermaidSource.ts`, no network; a chunk of its own, read the first time a diagram is shown, which a browser
 * loads only then), made then and kept for the app's life. It is out of sight; the diagrams it draws are shown
 * by `MermaidBlock`.
 */

const tokens = theme.cssVars;

/** The app's colours as mermaid draws in them: nodes on the code block's card surface, groups a step apart. */
export const MERMAID_PALETTES: Record<MermaidMode, MermaidPalette> = {
  light: { surface: tokens.light.card, ink: tokens.light.foreground, soft: tokens.light["muted-foreground"], node: tokens.light.accent, border: tokens.light.faint, cluster: "#F7F7F8" },
  dark: { surface: tokens.dark.card, ink: tokens.dark.foreground, soft: tokens.dark["muted-foreground"], node: tokens.dark.accent, border: tokens.dark.faint, cluster: "#242528" },
};

/** The engine's document, once a diagram asked for it and mermaid's script was read. */
const engineDocument = signal<string | null>(null);
let loading = false;

function want(): void {
  if (loading) return;
  loading = true;
  import("./mermaidSource").then(
    ({ MERMAID_SOURCE }) => engineDocument.set(mermaidEngineDocument(MERMAID_SOURCE)),
    (error: unknown) => renderer.receive({ type: "ready", ok: false, error: `The diagram engine did not load: ${error instanceof Error ? error.message : String(error)}` }),
  );
}

let renderer: MermaidRenderer = createMermaidRenderer({
  themeVariables: (mode) => mermaidThemeVariables(mode, MERMAID_PALETTES[mode]),
  onWanted: want,
});

/** The renderer the diagrams ask. */
export function mermaidRenderer(): MermaidRenderer {
  return renderer;
}

/** Tests only (FakeDaemonApp): a renderer that answers without a web view, which a headless test cannot run. */
export function replaceMermaidRenderer(next: MermaidRenderer): void {
  renderer = next;
}

/** Mounted once at the app's root: the engine's web view, from the first diagram on. */
export function MermaidEngine() {
  return (
    <If when={engineDocument()}>
      <View testID="mermaid-engine" class="absolute left-0 top-0 h-[320px] w-[480px] opacity-0" style={{ pointerEvents: "none" }}>
        <WebView
          ref={(node: WebViewRef | null) => renderer.attach(node ? { send: (message) => node.postMessage(message) } : null)}
          class="flex-1"
          html={engineDocument()!}
          baseUrl={viewerBaseUrl}
          scrollable={false}
          incognito
          accessibilityLabel="Diagram engine"
          onMessage={(event) => renderer.receive(event.data)}
        />
      </View>
    </If>
  );
}
