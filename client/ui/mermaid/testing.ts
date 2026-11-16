import { host } from "nsh";
import { createMermaidRenderer, mermaidThemeVariables, type MermaidRenderer } from "../../core/mermaid/mermaid";
import { MERMAID_PALETTES, replaceMermaidRenderer } from "./engine";

/**
 * Tests only: mermaid's engine is a web view, which a headless test does not run. With `host.params.mermaid`,
 * the test apps (FakeDaemonApp, More's Harness) draw with this one instead: a source with `BROKEN` in it fails
 * with mermaid's kind of words, any other is a 640×320 SVG naming its theme. Each drawing asked of it is posted
 * as `app.mermaidRender` ({source, mode}).
 */
export function installTestMermaid(params: unknown): void {
  if (!params || typeof params !== "object" || !("mermaid" in params)) return;
  let renderer: MermaidRenderer | null = null;
  const link = {
    send: (raw: string) => {
      const message = JSON.parse(raw) as { id: string; source: string; mode: string };
      host.post("app.mermaidRender", { source: message.source, mode: message.mode });
      const answer = message.source.includes("BROKEN")
        ? { type: "result", id: message.id, ok: false, error: "Parse error on line 2:\nA --> BROKEN[\n---------------^\nExpecting 'SQE', got 'EOF'" }
        : { type: "result", id: message.id, ok: true, svg: `<svg viewBox="0 0 640 320" width="640" height="320"><text>${message.mode}</text></svg>` };
      setTimeout(() => renderer?.receive(answer), 0);
    },
  };
  renderer = createMermaidRenderer({
    themeVariables: (mode) => mermaidThemeVariables(mode, MERMAID_PALETTES[mode]),
    onWanted: () => {
      renderer!.attach(link);
      renderer!.receive({ type: "ready", ok: true });
    },
  });
  replaceMermaidRenderer(renderer);
}
