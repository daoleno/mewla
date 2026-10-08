import React, { useCallback, useEffect, useState } from "react";
import {
  Platform,
  StyleSheet,
  Text,
  TextInput,
  View,
  type NativeSyntheticEvent,
  type TextInput as TextInputInstance,
  type TextInputContentSizeChangeEventData,
} from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import { COMPOSER_MAX_FONT_SCALE } from "./composerExpansionMetrics";
import {
  COMPOSER_SUBMIT_BEHAVIOR,
  composerReturnKeyType,
} from "./composerInputBehavior";
import { composerKeyIntent } from "../navigation/desktopShortcuts";

/**
 * No touchscreen as the primary pointer: there Enter sends (desktop web).
 * Phones and tablets report a coarse pointer and keep Enter as a new line.
 */
function hasDesktopPointer(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    !window.matchMedia("(pointer: coarse)").matches
  );
}

interface InterfaceComposerInputProps {
  inputRef: React.RefObject<TextInputInstance | null>;
  draft: string;
  placeholder: string;
  editable: boolean;
  chrome: TerminalThemeChrome;
  onDraftChange(value: string): void;
  onInputFocus(): void;
  onInputBlur(): void;
  /** Web keyboard send; absent while sending is not possible. */
  onKeyboardSend?(): void;
  lastSentRef?: React.RefObject<string>;
}

export function InterfaceComposerInput({
  inputRef,
  draft,
  placeholder,
  editable,
  chrome,
  onDraftChange,
  onInputFocus,
  onInputBlur,
  onKeyboardSend,
  lastSentRef,
}: InterfaceComposerInputProps) {
  const [inputHeight, setInputHeight] = useState(MIN_INPUT_HEIGHT);
  const draftEmpty = draft.length === 0;
  const multilineDraft = draft.includes("\n");
  const centerInputText =
    draftEmpty || (!multilineDraft && inputHeight <= MIN_INPUT_HEIGHT);
  const handleContentSizeChange = useCallback(
    (event: NativeSyntheticEvent<TextInputContentSizeChangeEventData>) => {
      const measuredHeight = Math.ceil(event.nativeEvent.contentSize.height);
      setInputHeight(
        Math.max(MIN_INPUT_HEIGHT, Math.min(MAX_INPUT_HEIGHT, measuredHeight)),
      );
    },
    [],
  );

  useEffect(() => {
    if (draftEmpty) {
      setInputHeight(MIN_INPUT_HEIGHT);
    }
  }, [draftEmpty]);

  // Web keys: Enter sends with a mouse and keyboard, Shift+Enter is a new
  // line, Ctrl/⌘+Enter always sends, Esc leaves the box, ↑ in an empty box
  // brings back the last message. Nothing acts while an IME is composing.
  // Native keeps its keyboard's own Return.
  const handleKeyPress = useCallback(
    (event: { nativeEvent: unknown; preventDefault(): void }) => {
      const key = event.nativeEvent as KeyboardEvent;
      const intent = composerKeyIntent(key, {
        desktop: hasDesktopPointer(),
        draftEmpty,
      });
      if (intent === "send") {
        event.preventDefault();
        onKeyboardSend?.();
      } else if (intent === "blur") {
        inputRef.current?.blur();
      } else if (intent === "recall" && lastSentRef?.current) {
        event.preventDefault();
        onDraftChange(lastSentRef.current);
      }
    },
    [draftEmpty, inputRef, lastSentRef, onDraftChange, onKeyboardSend],
  );

  return (
    <View
      collapsable={false}
      style={[styles.inputWrap, { height: inputHeight }]}
    >
      <TextInput
        ref={inputRef}
        style={[
          styles.input,
          centerInputText ? styles.inputCentered : null,
          { color: chrome.text, height: inputHeight },
        ]}
        value={draft}
        onChangeText={onDraftChange}
        onContentSizeChange={handleContentSizeChange}
        placeholder=""
        accessibilityLabel={placeholder}
        selectionColor={chrome.accent}
        multiline
        editable={editable}
        textAlignVertical={centerInputText ? "center" : "top"}
        autoCorrect={false}
        autoCapitalize="none"
        autoComplete="off"
        spellCheck={false}
        keyboardType="default"
        maxFontSizeMultiplier={COMPOSER_MAX_FONT_SCALE}
        disableFullscreenUI
        importantForAutofill="no"
        selectTextOnFocus={false}
        underlineColorAndroid="transparent"
        showSoftInputOnFocus
        returnKeyType={composerReturnKeyType(Platform.OS)}
        enterKeyHint="enter"
        submitBehavior={COMPOSER_SUBMIT_BEHAVIOR}
        blurOnSubmit={false}
        onFocus={onInputFocus}
        onBlur={onInputBlur}
        onKeyPress={Platform.OS === "web" ? handleKeyPress : undefined}
        nativeID="mewla-composer"
      />
      {draftEmpty && placeholder ? (
        <View pointerEvents="none" style={styles.placeholderOverlay}>
          <Text
            numberOfLines={1}
            maxFontSizeMultiplier={COMPOSER_MAX_FONT_SCALE}
            style={[styles.placeholderText, { color: chrome.textMuted }]}
          >
            {placeholder}
          </Text>
        </View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  inputWrap: {
    flex: 1,
    minWidth: 0,
    minHeight: 42,
    maxHeight: 124,
    justifyContent: "center",
    position: "relative",
  },
  input: {
    width: "100%",
    minHeight: 44,
    maxHeight: 124,
    paddingLeft: 6,
    paddingRight: 8,
    paddingTop: Platform.OS === "android" ? 10 : 9,
    paddingBottom: Platform.OS === "android" ? 7 : 8,
    fontSize: 15,
    lineHeight: 23,
    fontFamily: Typography.chatFont,
    includeFontPadding: false,
    // Web: no browser outline inside the pill; the capsule's own border
    // carries keyboard focus (InterfaceComposerExpandingDock).
    ...(Platform.OS === "web" ? { outlineStyle: "none" as const } : null),
  },
  inputCentered: {
    paddingTop: Platform.OS === "android" ? 0 : 8,
    paddingBottom: Platform.OS === "android" ? 0 : 8,
  },
  placeholderOverlay: {
    position: "absolute",
    top: 0,
    right: 8,
    bottom: 0,
    left: 6,
    justifyContent: "center",
  },
  placeholderText: {
    fontSize: 15,
    lineHeight: 23,
    fontFamily: Typography.chatFont,
    includeFontPadding: false,
  },
});

const MIN_INPUT_HEIGHT = 44;
const MAX_INPUT_HEIGHT = 124;
