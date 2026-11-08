import React from "react";
import { RisingSheet } from "../ui/RisingSheet";
import { TerminalRenameCard } from "./TerminalRenameCard";

interface TerminalRenameModalProps {
  visible: boolean;
  draft: string;
  placeholder: string;
  onDraftChange(value: string): void;
  onClose(): void;
  onSave(): void;
}

export function TerminalRenameModal({
  visible,
  draft,
  placeholder,
  onDraftChange,
  onClose,
  onSave,
}: TerminalRenameModalProps) {
  return (
    <RisingSheet visible={visible} onClose={onClose} avoidKeyboard>
      <TerminalRenameCard
        draft={draft}
        placeholder={placeholder}
        onDraftChange={onDraftChange}
        onClose={onClose}
        onSave={onSave}
      />
    </RisingSheet>
  );
}
