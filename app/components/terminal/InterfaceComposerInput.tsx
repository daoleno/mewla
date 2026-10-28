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
  hasDesktopPointer,
} from "./composerInputBehavior";
import { composerKeyIntent } from "../navigation/desktopShortcuts";
import { useComposerKeyActions } from "./composerKeyActions";

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
  /** A command or mention list is open above the box. */
  pickerOpen?: boolean;
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
  pickerOpen = false,
}: InterfaceComposerInputProps) {
  const keyActions = useComposerKeyActions();
  // A desktop window has the room: the box grows to about eleven lines.
  const maxInputHeight = Platform.OS === "web" && hasDesktopPointer() ? DESKTOP_MAX_INPUT_HEIGHT : MAX_INPUT_HEIGHT;
  const [inputHeight, setInputHeight] = useState(MIN_INPUT_HEIGHT);
  const draftEmpty = draft.length === 0;
  const multilineDraft = draft.includes("\n");
  const centerInputText =
    draftEmpty || (!multilineDraft && inputHeight <= MIN_INPUT_HEIGHT);
  const handleContentSizeChange = useCallback(
    (event: NativeSyntheticEvent<TextInputContentSizeChangeEventData>) => {
      const measuredHeight = Math.ceil(event.nativeEvent.contentSize.height);
      setInputHeight(
        Math.max(MIN_INPUT_HEIGHT, Math.min(maxInputHeight, measuredHeight)),
      );
    },
    [maxInputHeight],
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
        pickerOpen,
      });
      if (intent === "hold") {
        event.preventDefault();
      } else if (intent === "send") {
        event.preventDefault();
        onKeyboardSend?.();
      } else if (intent === "blur") {
        // Esc leaves an edit first, then the box.
        if (keyActions.cancelEdit?.()) {
          event.preventDefault();
        } else {
          inputRef.current?.blur();
        }
      } else if (intent === "recall") {
        // ↑ edits the last message where it can be edited, else brings back
        // the text this box last sent.
        if (keyActions.editLast?.()) {
          event.preventDefault();
        } else if (lastSentRef?.current) {
          event.preventDefault();
          onDraftChange(lastSentRef.current);
        }
      }
    },
    [draftEmpty, inputRef, keyActions, lastSentRef, onDraftChange, onKeyboardSend, pickerOpen],
  );

  return (
    <View
      collapsable={false}
      style={[styles.inputWrap, { height: inputHeight, maxHeight: maxInputHeight }]}
    >
      <TextInput
        ref={inputRef}
        style={[
          styles.input,
          centerInputText ? styles.inputCentered : null,
          { color: chrome.text, height: inputHeight, maxHeight: maxInputHeight },
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
const DESKTOP_MAX_INPUT_HEIGHT = 268;
