import React, { createContext, memo, useContext } from "react";
import { ScrollView, StyleSheet, Text, View } from "react-native";
import { GestureDetector } from "react-native-gesture-handler";
import { Typography } from "../../constants/tokens";
import { useNestedHorizontalScrollGesture } from "../ui/useNestedHorizontalScrollGesture";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { imageReference, type ImageSource } from "../../services/imageSource";
import { AppImage } from "./AppImage";
import {
  messageTableCellText,
  type MessageBlock,
  type MessageListBlock,
  type MessageTableAlignment,
} from "./InterfaceMessageMarkdownModel";
import { InterfaceInlineMessage } from "./InterfaceInlineMessage";
import { MermaidDiagram } from "../markdown/MermaidDiagram";
import { isMermaidFenceLanguage } from "../markdown/mermaidFences";
import { InterfaceMessageCodeBlock } from "./InterfaceMessageCodeBlock";
import {
  messageTableColumnWidths,
  messageTableRowTone,
  messageTableSemanticColors,
} from "./InterfaceMessageTableModel";
import { useTimelineSelectableTextProps } from "./TimelineTextSelectableContext";

interface InterfaceMessageBlockProps {
  block: MessageBlock;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  compact: boolean;
  dense?: boolean;
  isLast: boolean;
  streaming?: boolean;
  /** How many lists this block sits inside; picks the bullet and keeps item text close. */
  listDepth?: number;
}

// An empty cell still holds one line, so its row keeps its height.
const EMPTY_CELL = [{ text: " " }];

/** Every image of the message, so a tap on one opens the same gallery. */
export const MessageImageGalleryContext = createContext<ImageSource[]>([]);

export const InterfaceMessageBlock = memo(function InterfaceMessageBlock({
  block,
  chrome,
  theme,
  compact,
  dense = false,
  isLast,
  streaming = false,
  listDepth = 0,
}: InterfaceMessageBlockProps) {
  const selectableTextProps = useTimelineSelectableTextProps();
  const tableColors = messageTableSemanticColors(chrome);
  const tableHorizontalGesture = useNestedHorizontalScrollGesture();
  const inList = listDepth > 0;

  switch (block.type) {
    case "heading":
      return (
        <View
          style={[
            styles.messageHeadingWrap,
            isLast ? styles.messageBlockLast : null,
          ]}
        >
          <Text
            {...selectableTextProps}
            style={[
              styles.messageHeading,
              block.level <= 2 ? styles.messageHeadingLarge : null,
              { color: chrome.text },
            ]}
          >
            <InterfaceInlineMessage
              parts={block.inline}
              chrome={chrome}
              theme={theme}
              compact={compact}
            />
          </Text>
        </View>
      );
    case "list":
      return (
        <View
          style={[
            styles.messageList,
            inList ? styles.messageListNested : null,
            isLast ? styles.messageBlockLast : null,
          ]}
        >
          {block.items.map((item, itemIndex) => (
            <View key={itemIndex} style={styles.messageListItem}>
              <View
                style={[
                  styles.messageListMarkerWrap,
                  block.ordered ? styles.messageListMarkerWrapOrdered : null,
                ]}
              >
                {item.taskState ? (
                  <View
                    style={[
                      styles.messageTaskCheckbox,
                      {
                        borderColor: chrome.textSubtle,
                        backgroundColor:
                          item.taskState === "checked"
                            ? theme.green
                            : "transparent",
                      },
                    ]}
                  >
                    {item.taskState === "checked" ? (
                      <View
                        style={[
                          styles.messageTaskCheckboxDot,
                          {
                            backgroundColor:
                              theme.background === "transparent"
                                ? theme.cursorAccent
                                : theme.background,
                          },
                        ]}
                      />
                    ) : null}
                  </View>
                ) : (
                  <Text
                    {...selectableTextProps}
                    style={[
                      styles.messageListMarker,
                      { color: chrome.textSubtle },
                    ]}
                  >
                    {listMarkerText(block, itemIndex, listDepth)}
                  </Text>
                )}
              </View>
              <View style={styles.messageListBody}>
                {item.blocks.map((child, childIndex) => (
                  <InterfaceMessageBlock
                    key={childIndex}
                    block={child}
                    chrome={chrome}
                    theme={theme}
                    compact={compact}
                    dense={dense}
                    isLast={childIndex === item.blocks.length - 1}
                    streaming={streaming}
                    listDepth={listDepth + 1}
                  />
                ))}
              </View>
            </View>
          ))}
        </View>
      );
    case "table": {
      const headerText = block.headers.map(messageTableCellText);
      const rowText = block.rows.map((row) => row.map(messageTableCellText));
      const columnWidths = messageTableColumnWidths(headerText, rowText, compact);
      return (
        <View
          style={[
            styles.messageTableBlock,
            isLast ? styles.messageBlockLast : null,
          ]}
        >
          <GestureDetector gesture={tableHorizontalGesture}>
            <ScrollView
              horizontal
              nestedScrollEnabled
              showsHorizontalScrollIndicator={false}
              contentContainerStyle={styles.messageTableScrollContent}
            >
              <View
                style={[
                  styles.messageTable,
                  compact ? styles.messageTableCompact : null,
                  {
                    backgroundColor: chrome.surface,
                    borderColor: chrome.border,
                  },
                ]}
              >
                <View
                  style={[
                    styles.messageTableRow,
                    styles.messageTableHeaderRow,
                    {
                      backgroundColor: tableColors.header,
                      borderBottomColor: chrome.border,
                    },
                  ]}
                >
                  {block.headers.map((cell, columnIndex) => (
                    <View
                      key={`header:${columnIndex}`}
                      style={[
                        styles.messageTableCell,
                        compact ? styles.messageTableCellCompact : null,
                        columnIndex === block.headers.length - 1
                          ? styles.messageTableCellLast
                          : null,
                        { borderRightColor: chrome.border, width: columnWidths[columnIndex] },
                      ]}
                    >
                      <Text
                        {...selectableTextProps}
                        style={[
                          styles.messageTableHeaderText,
                          compact ? styles.messageTableTextCompact : null,
                          {
                            color: chrome.text,
                            textAlign: tableCellTextAlign(
                              block.alignments[columnIndex],
                            ),
                          },
                        ]}
                      >
                        <InterfaceInlineMessage
                          parts={cell.length > 0 ? cell : EMPTY_CELL}
                          chrome={chrome}
                          theme={theme}
                          compact={compact}
                        />
                      </Text>
                    </View>
                  ))}
                </View>
                {block.rows.map((row, rowIndex) => {
                  const rowTone = messageTableRowTone(rowText[rowIndex], rowIndex);
                  return (
                    <View
                      key={`row:${rowIndex}`}
                      style={[
                        styles.messageTableRow,
                        rowIndex < block.rows.length - 1
                          ? styles.messageTableBodyRow
                          : null,
                        rowTone === "section"
                          ? styles.messageTableSectionRow
                          : null,
                        {
                          backgroundColor: tableColors[rowTone],
                          borderBottomColor: chrome.border,
                          borderTopColor: chrome.borderStrong,
                        },
                      ]}
                    >
                      {row.map((cell, columnIndex) => (
                        <View
                          key={`cell:${rowIndex}:${columnIndex}`}
                          style={[
                            styles.messageTableCell,
                            compact ? styles.messageTableCellCompact : null,
                            columnIndex === row.length - 1
                              ? styles.messageTableCellLast
                              : null,
                            { borderRightColor: chrome.border, width: columnWidths[columnIndex] },
                          ]}
                        >
                          <Text
                            {...selectableTextProps}
                            style={[
                              rowTone === "section"
                                ? styles.messageTableSectionText
                                : styles.messageTableText,
                              compact ? styles.messageTableTextCompact : null,
                              {
                                color: chrome.text,
                                textAlign: tableCellTextAlign(
                                  block.alignments[columnIndex],
                                ),
                              },
                            ]}
                          >
                            <InterfaceInlineMessage
                              parts={cell.length > 0 ? cell : EMPTY_CELL}
                              chrome={chrome}
                              theme={theme}
                              compact={compact}
                            />
                          </Text>
                        </View>
                      ))}
                    </View>
                  );
                })}
              </View>
            </ScrollView>
          </GestureDetector>
        </View>
      );
    }
    case "code": {
      const codeBlock = (
        <InterfaceMessageCodeBlock
          text={block.text}
          language={block.language}
          chrome={chrome}
          theme={theme}
          compact={compact}
          isLast={isLast}
        />
      );
      if (isMermaidFenceLanguage(block.language)) {
        return (
          <MermaidDiagram
            source={block.text}
            chrome={chrome}
            theme={theme}
            compact={compact}
            isLast={isLast}
            streaming={streaming}
            closed={block.closed !== false}
            fallback={codeBlock}
          />
        );
      }
      return codeBlock;
    }
    case "image":
      return (
        <InterfaceMessageImage
          block={block}
          chrome={chrome}
          compact={compact}
          isLast={isLast}
        />
      );
    case "rule":
      return (
        <View
          style={[styles.messageRule, { backgroundColor: chrome.borderStrong }]}
        />
      );
    case "quote": {
      const quoteChrome = { ...chrome, text: chrome.textMuted };
      return (
        <View
          style={[
            styles.messageQuote,
            { borderLeftColor: chrome.borderStrong },
            isLast ? styles.messageBlockLast : null,
          ]}
        >
          {block.blocks.map((child, index) => (
            <InterfaceMessageBlock
              key={index}
              block={child}
              chrome={quoteChrome}
              theme={theme}
              compact={compact}
              dense={dense}
              isLast={index === block.blocks.length - 1}
              streaming={streaming}
              listDepth={listDepth}
            />
          ))}
        </View>
      );
    }
    case "paragraph":
    default:
      return (
        <Text
          {...selectableTextProps}
          style={[
            styles.messageText,
            compact ? styles.messageTextCompact : null,
            dense ? styles.messageTextDense : null,
            { color: chrome.text },
            inList ? styles.messageTextInList : null,
            isLast ? styles.messageBlockLast : null,
          ]}
        >
          <InterfaceInlineMessage
            parts={block.inline}
            chrome={chrome}
            theme={theme}
            compact={compact}
          />
        </Text>
      );
  }
});

/** The only block that reads the gallery, so a change in it re-renders images and nothing else. */
function InterfaceMessageImage({
  block,
  chrome,
  compact,
  isLast,
}: {
  block: Extract<MessageBlock, { type: "image" }>;
  chrome: TerminalThemeChrome;
  compact: boolean;
  isLast: boolean;
}) {
  const gallery = useContext(MessageImageGalleryContext);
  const selectableTextProps = useTimelineSelectableTextProps();
  return (
    <View style={isLast ? null : styles.messageImage}>
      <AppImage
        source={imageReference(block.src, block.alt || "Image")}
        gallery={gallery}
        chrome={chrome}
        compact={compact}
      />
      {block.alt || block.title ? (
        <Text
          {...selectableTextProps}
          style={[styles.messageImageCaption, { color: chrome.textMuted }]}
        >
          {block.title || block.alt}
        </Text>
      ) : null}
    </View>
  );
}

function listMarkerText(list: MessageListBlock, index: number, depth: number) {
  if (list.ordered) {
    return `${list.start + index}.`;
  }
  switch (depth % 3) {
    case 1:
      return "\u25E6";
    case 2:
      return "\u25AA";
    default:
      return "\u2022";
  }
}

function tableCellTextAlign(alignment: MessageTableAlignment | undefined) {
  switch (alignment) {
    case "center":
      return "center";
    case "right":
      return "right";
    case "left":
    default:
      return "left";
  }
}

const styles = StyleSheet.create({
  messageText: {
    alignSelf: "stretch",
    minWidth: 0,
    marginBottom: 10,
    fontSize: 15,
    lineHeight: 23,
    fontFamily: Typography.chatFont,
    letterSpacing: 0,
  },
  messageTextCompact: {
    marginBottom: 8,
    fontSize: 14,
    lineHeight: 21,
  },
  messageTextInList: {
    marginBottom: 4,
  },
  messageTextDense: {
    marginBottom: 6,
    fontSize: 15,
    lineHeight: 23,
  },
  messageHeadingWrap: {
    alignSelf: "stretch",
    width: "100%",
    minWidth: 0,
    marginBottom: 8,
  },
  messageHeading: {
    fontSize: 15,
    lineHeight: 23,
    fontFamily: Typography.chatFontMedium,
    letterSpacing: 0,
  },
  messageHeadingLarge: {
    fontSize: 16,
    lineHeight: 25,
  },
  messageList: {
    alignSelf: "stretch",
    width: "100%",
    minWidth: 0,
    marginBottom: 10,
    gap: 6,
  },
  messageListNested: {
    marginBottom: 4,
  },
  messageListItem: {
    width: "100%",
    flexDirection: "row",
    alignItems: "flex-start",
    minWidth: 0,
  },
  messageListBody: {
    flex: 1,
    minWidth: 0,
  },
  messageListMarkerWrap: {
    width: 22,
    paddingRight: 7,
    alignItems: "flex-end",
  },
  messageListMarkerWrapOrdered: {
    width: 32,
  },
  messageListMarker: {
    textAlign: "right",
    fontSize: 13,
    lineHeight: 23,
    fontFamily: Typography.chatFontMedium,
    letterSpacing: 0,
  },
  messageTaskCheckbox: {
    width: 14,
    height: 14,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: 4,
    alignItems: "center",
    justifyContent: "center",
    marginTop: 5,
  },
  messageTaskCheckboxDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
  },
  messageTableBlock: {
    alignSelf: "stretch",
    width: "100%",
    minWidth: 0,
    marginBottom: 10,
  },
  messageTableScrollContent: {
    paddingRight: 8,
  },
  messageTable: {
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: 8,
    overflow: "hidden",
  },
  messageTableCompact: {
    borderRadius: 7,
  },
  messageTableRow: {
    flexDirection: "row",
    minWidth: 0,
  },
  messageTableHeaderRow: {
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  messageTableBodyRow: {
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  messageTableSectionRow: {
    borderTopWidth: StyleSheet.hairlineWidth,
  },
  // Widths come per column from messageTableColumnWidths.
  messageTableCell: {
    borderRightWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 10,
    paddingVertical: 7,
  },
  messageTableCellCompact: {
    paddingHorizontal: 8,
    paddingVertical: 6,
  },
  messageTableCellLast: {
    borderRightWidth: 0,
  },
  messageTableText: {
    fontSize: 13,
    lineHeight: 19,
    fontFamily: Typography.chatFont,
    letterSpacing: 0,
  },
  messageTableSectionText: {
    fontSize: 13,
    lineHeight: 19,
    fontFamily: Typography.chatFontMedium,
    letterSpacing: 0,
  },
  messageTableHeaderText: {
    fontSize: 13,
    lineHeight: 19,
    fontFamily: Typography.chatFontMedium,
    letterSpacing: 0,
  },
  messageTableTextCompact: {
    fontSize: 12,
    lineHeight: 18,
  },
  messageRule: {
    alignSelf: "stretch",
    height: StyleSheet.hairlineWidth,
    marginVertical: 10,
  },
  messageQuote: {
    alignSelf: "stretch",
    width: "100%",
    minWidth: 0,
    marginBottom: 10,
    borderLeftWidth: 2,
    paddingLeft: 10,
    paddingVertical: 1,
  },
  messageImage: {
    marginBottom: 10,
  },
  messageImageCaption: {
    marginTop: 4,
    fontSize: 13,
    lineHeight: 19,
    fontFamily: Typography.chatFont,
  },
  messageBlockLast: {
    marginBottom: 0,
  },
});
