import type { TextProps } from "react-native";

/**
 * Line breaking for message text. Android's default "highQuality" strategy
 * balances a paragraph for the width it is measured at. A sent bubble then
 * shrinks to its widest line and the text is laid out again at that narrower
 * width, where the balanced result can need more lines than were measured:
 * the extra lines are drawn below the bubble, over the next rows. Greedy
 * breaking gives the same lines at any width that fits the widest one, so
 * what is measured is what is drawn. iOS and web ignore the prop.
 */
export const MESSAGE_TEXT_LAYOUT: Pick<TextProps, "textBreakStrategy"> = {
  textBreakStrategy: "simple",
};
