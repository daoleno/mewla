import React, { useMemo } from "react";
import { StyleSheet, View } from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import {
  MarkdownLinkSurfaceContext,
  type MarkdownLinkSurface,
} from "../markdown/MarkdownLinkContext";
import { imageReference, type ImageSource } from "../../services/imageSource";
import {
  InterfaceMessageBlock,
  MessageImageGalleryContext,
} from "./InterfaceMessageBlock";
import {
  messageImages,
  parseMessageMarkdown,
} from "./InterfaceMessageMarkdownModel";
import { prepareInterfaceMarkdown } from "./InterfaceMessageMarkdownPrepare";
import { useStreamingMarkdownPresentation } from "./useStreamingMarkdownPresentation";

const NO_IMAGES: ImageSource[] = [];

interface MessageBodyProps extends MarkdownLinkSurface {
  value: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  compact?: boolean;
  dense?: boolean;
  streaming?: boolean;
  /** Keep the line breaks a person typed (their own message). */
  keepLineBreaks?: boolean;
}

export function MessageBody({
  value,
  chrome,
  theme,
  compact = false,
  dense = false,
  streaming = false,
  keepLineBreaks = false,
  baseDir,
  openFile,
  canOpenFile,
}: MessageBodyProps) {
  const linkSurface = useMemo<MarkdownLinkSurface>(
    () => ({ baseDir, openFile, canOpenFile }),
    [baseDir, canOpenFile, openFile],
  );
  // Provider value stays lossless in props; only presentation work coalesces.
  const presentedValue = useStreamingMarkdownPresentation(value, streaming);
  const blocks = useMemo(
    () =>
      parseMessageMarkdown(
        streaming ? prepareInterfaceMarkdown(presentedValue, true) : presentedValue,
        { keepLineBreaks },
      ),
    [keepLineBreaks, presentedValue, streaming],
  );
  // Keyed on what the images are, not on the blocks: blocks are a new array
  // every streaming frame, and a new gallery would re-render every image.
  const galleryKey = messageImages(blocks)
    .map((image) => `${image.src}\n${image.alt}`)
    .join("\n\n");
  const gallery = useMemo(
    () =>
      galleryKey
        ? messageImages(blocks).map((image) =>
            imageReference(image.src, image.alt || "Image"),
          )
        : NO_IMAGES,
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [galleryKey],
  );
  if (blocks.length === 0) {
    return null;
  }
  return (
    <MarkdownLinkSurfaceContext.Provider value={linkSurface}>
    <MessageImageGalleryContext.Provider value={gallery}>
      <View style={styles.messageBody}>
        {blocks.map((block, index) => (
          <InterfaceMessageBlock
            key={index}
            block={block}
            chrome={chrome}
            theme={theme}
            compact={compact}
            dense={dense}
            isLast={index === blocks.length - 1}
            streaming={streaming}
          />
        ))}
      </View>
    </MessageImageGalleryContext.Provider>
    </MarkdownLinkSurfaceContext.Provider>
  );
}

const styles = StyleSheet.create({
  messageBody: {
    alignSelf: "stretch",
    width: "100%",
    minWidth: 0,
  },
});
