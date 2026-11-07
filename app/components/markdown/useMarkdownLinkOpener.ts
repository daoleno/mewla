import { useCallback, useContext } from "react";
import { Linking } from "react-native";
import { useSessionFilePreviewContext } from "../terminal/SessionFilePreviewContext";
import { MarkdownLinkSurfaceContext, type MarkdownLinkSurface } from "./MarkdownLinkContext";
import { isMarkdownLinkOpenable, openMarkdownLink } from "./markdownLinks";

/**
 * Activates a Markdown link for the surface that shows it. Web and mail links
 * open outside the app; files open in the Session file preview unless the
 * surface brings its own opener. `openable` says whether a link would do
 * anything, so the surface can show the rest as plain text.
 */
export function useMarkdownLinks(options: MarkdownLinkSurface = {}) {
  const filePreview = useSessionFilePreviewContext();
  const target = useContext(MarkdownLinkSurfaceContext);
  const baseDir = options.baseDir ?? target.baseDir;
  const openFile = options.openFile ?? target.openFile;
  const canOpenFile = options.canOpenFile ?? target.canOpenFile;
  const hasFileOpener = Boolean(openFile || filePreview);
  const open = useCallback(
    (href: string) => {
      void openMarkdownLink(href, {
        openUrl: (url) => Linking.openURL(url),
        openFile: openFile ?? ((reference) => filePreview?.open(reference) ?? false),
        baseDir,
      });
    },
    [baseDir, filePreview, openFile],
  );
  const openable = useCallback(
    (href: string) =>
      isMarkdownLinkOpenable(href, {
        baseDir,
        canOpenFile: canOpenFile ?? (() => hasFileOpener),
      }),
    [baseDir, canOpenFile, hasFileOpener],
  );
  return { open, openable };
}
