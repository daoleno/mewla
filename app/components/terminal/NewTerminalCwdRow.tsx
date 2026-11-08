import React, { useMemo } from "react";
import { StyleSheet } from "react-native";
import { compactPathLabel } from "../../services/pathDisplay";
import { ListRow, ListSection } from "../ui";

interface NewTerminalCwdRowProps {
  cwd: string;
  canPickDirectory: boolean;
  onPickDirectory(): void;
}

export function NewTerminalCwdRow({
  cwd,
  canPickDirectory,
  onPickDirectory,
}: NewTerminalCwdRowProps) {
  const label = useMemo(() => formatCwdLabel(cwd), [cwd]);

  return (
    <ListSection style={styles.section}>
      <ListRow
        icon="folder"
        title={label}
        accessibilityLabel="Choose working directory"
        accessory={canPickDirectory ? "chevron" : "none"}
        disabled={!canPickDirectory}
        onPress={onPickDirectory}
      />
    </ListSection>
  );
}

function formatCwdLabel(value: string): string {
  const trimmed = value.trim().replace(/\/+$/, "");
  if (!trimmed) {
    return "Default directory";
  }
  return compactPathLabel(trimmed);
}

const styles = StyleSheet.create({
  section: { marginBottom: 6 },
});
