/** The part of Pele (vendored `pele.js`, pele 1.13.0) the app uses; the package's own types are in node_modules/pele/dist. */
export interface RenderOptions {
  fontFamily?: string;
  fontSize?: number;
  idPrefix?: string;
  /** false: the SVG at its own pixel size (no `max-width:100%`). */
  responsive?: boolean;
  /** The width the layout should fit: a wide diagram lays out again (a flowchart may turn vertical). */
  maxWidth?: number;
  autoDirection?: boolean;
  padding?: number;
  limit?: number;
  outputLimit?: number;
  maxEdges?: number;
  links?: boolean;
  images?: boolean;
  now?: number | Date;
}
export interface RenderResult {
  svg: string;
  width: number;
  height: number;
  type: string;
  mermaidType: string;
}
export class PeleError extends Error {}
export function render(source: string, options?: RenderOptions): RenderResult;
export function supports(type: string): boolean;
