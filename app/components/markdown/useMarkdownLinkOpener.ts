import { useCallback } from "react";
import { Linking } from "react-native";
import { useSessionFilePreviewContext } from "../terminal/SessionFilePreviewContext";
import { openMarkdownLink } from "./markdownLinks";

/**
 * Activates a Markdown link for the surface that shows it. Web and mail links
 * open outside the app; files open in the Session file preview unless the
 * surface brings its own opener.
 */
export function useMarkdownLinkOpener(
  options: {
    baseDir?: string;
    openFile?(reference: string): boolean;
  } = {},
) {
  const filePreview = useSessionFilePreviewContext();
  const { baseDir, openFile } = options;
  return useCallback(
    (href: string) => {
      void openMarkdownLink(href, {
        openUrl: (url) => Linking.openURL(url),
        openFile: openFile ?? ((reference) => filePreview?.open(reference) ?? false),
        baseDir,
      });
    },
    [baseDir, filePreview, openFile],
  );
}
