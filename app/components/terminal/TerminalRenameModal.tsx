import React from "react";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { RisingSheet } from "../ui/RisingSheet";
import { TerminalRenameCard } from "./TerminalRenameCard";

interface TerminalRenameModalProps {
  visible: boolean;
  draft: string;
  placeholder: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onDraftChange(value: string): void;
  onClose(): void;
  onSave(): void;
}

export function TerminalRenameModal({
  visible,
  draft,
  placeholder,
  chrome,
  theme,
  onDraftChange,
  onClose,
  onSave,
}: TerminalRenameModalProps) {
  return (
    <RisingSheet visible={visible} onClose={onClose} avoidKeyboard backdropColor={chrome.overlay}>
      <TerminalRenameCard
        draft={draft}
        placeholder={placeholder}
        chrome={chrome}
        theme={theme}
        onDraftChange={onDraftChange}
        onClose={onClose}
        onSave={onSave}
      />
    </RisingSheet>
  );
}
