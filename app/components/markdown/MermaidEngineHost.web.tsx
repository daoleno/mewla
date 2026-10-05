import type { MermaidEngineResult } from "./mermaidRenderQueue";

export type { MermaidEngineResult } from "./mermaidRenderQueue";

/**
 * The Mermaid engine runs only inside an isolated native WebView document.
 * Web has no equivalent sandbox here, so diagrams keep their source fallback.
 */
export function requestMermaidEngineRender(input: {
  onResult: (result: MermaidEngineResult) => void;
}) {
  input.onResult({ ok: false, error: "engine" });
  return () => undefined;
}

export function MermaidEngineHost() {
  return null;
}
