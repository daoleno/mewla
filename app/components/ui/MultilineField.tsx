import React, { forwardRef } from "react";
import {
  StyleSheet,
  TextInput,
  type StyleProp,
  type TextInputInstance,
  type TextInputProps,
  type TextStyle,
} from "react-native";
import { Radii, TypeScale, UiTextMetrics, useAppColors } from "../../constants/tokens";

export interface MultilineFieldProps extends Omit<TextInputProps, "multiline" | "style"> {
  /** Height of the empty field; it grows with its text. */
  minHeight?: number;
  style?: StyleProp<TextStyle>;
}

/**
 * The framed field for text that runs over several lines: a note, a pasted
 * code, a link. It shares the box of `MobileSingleLineInput framed`, so a form
 * mixing both reads as one family. Text starts at the top.
 */
export const MultilineField = forwardRef<TextInputInstance, MultilineFieldProps>(
  function MultilineField({ minHeight = 92, style, placeholderTextColor, ...props }, ref) {
    const colors = useAppColors();
    return (
      <TextInput
        {...props}
        ref={ref}
        multiline
        placeholderTextColor={placeholderTextColor ?? colors.textTertiary}
        selectionColor={colors.selectionBackground}
        cursorColor={colors.accentStrong}
        style={[
          styles.field,
          {
            minHeight,
            color: colors.textPrimary,
            backgroundColor: colors.inputBackground,
            borderColor: colors.borderSubtle,
          },
          style,
        ]}
      />
    );
  },
);

const styles = StyleSheet.create({
  field: {
    ...TypeScale.body,
    ...UiTextMetrics,
    borderRadius: Radii.xs,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 14,
    paddingVertical: 12,
    textAlignVertical: "top",
  },
});
