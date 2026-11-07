import { createContext } from "react";

/**
 * Where a document's relative links point and how its files open, for a
 * surface that shows a file rather than a chat message (a Brain workspace
 * file, a Skill file). Chat messages leave it empty and open files in the
 * Session file preview.
 */
export interface MarkdownLinkSurface {
  baseDir?: string;
  openFile?(reference: string): boolean;
  canOpenFile?(reference: string): boolean;
}

export const MarkdownLinkSurfaceContext = createContext<MarkdownLinkSurface>({});
