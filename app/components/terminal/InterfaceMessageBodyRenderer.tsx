import React, { useMemo } from "react";
import { StyleSheet, View } from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { imageReference } from "../../services/imageSource";
import {
  InterfaceMessageBlock,
  MessageImageGalleryContext,
} from "./InterfaceMessageBlock";
import {
  messageImages,
  parseMessageMarkdown,
} from "./InterfaceMessageMarkdownModel";
import { prepareInterfaceMarkdown } from "./InterfaceNativeMarkdownBodyPrepare";
import { useStreamingMarkdownPresentation } from "./useStreamingMarkdownPresentation";

interface MessageBodyProps {
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
}: MessageBodyProps) {
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
  const gallery = useMemo(
    () =>
      messageImages(blocks).map((image) =>
        imageReference(image.src, image.alt || "Image"),
      ),
    [blocks],
  );
  if (blocks.length === 0) {
    return null;
  }
  return (
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
  );
}

const styles = StyleSheet.create({
  messageBody: {
    alignSelf: "stretch",
    width: "100%",
    minWidth: 0,
  },
});
