import React from "react";
import { StyleSheet, View } from "react-native";
import { AppText } from "../ui/AppText";
import { Button } from "../ui/Button";
import { MobileSingleLineInput } from "../ui/MobileSingleLineInput";
import { useAppColors, Radii } from "../../constants/tokens";

interface TerminalRenameCardProps {
  draft: string;
  placeholder: string;
  onDraftChange(value: string): void;
  onClose(): void;
  onSave(): void;
}

/** Reads the terminal theme from the surrounding `TerminalThemeScope`. */
export function TerminalRenameCard({
  draft,
  placeholder,
  onDraftChange,
  onClose,
  onSave,
}: TerminalRenameCardProps) {
  const colors = useAppColors();
  return (
    <View
      style={[
        styles.card,
        { backgroundColor: colors.bgSurface, borderColor: colors.border },
      ]}
    >
      <AppText variant="heading">Rename Terminal</AppText>
      <AppText variant="caption" tone="secondary" style={styles.hint}>
        Only changes the local display name on this device.
      </AppText>
      <MobileSingleLineInput
        framed
        value={draft}
        onChangeText={onDraftChange}
        placeholder={placeholder}
        placeholderTextColor={colors.textTertiary}
        accessibilityLabel="Terminal name"
        autoFocus
        autoCorrect={false}
        autoCapitalize="none"
        returnKeyType="done"
        onSubmitEditing={onSave}
        containerStyle={styles.input}
      />
      <View style={styles.actions}>
        <Button label="Cancel" variant="tinted" onPress={onClose} />
        <Button label="Save" variant="filled" onPress={onSave} />
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  card: { borderRadius: Radii.card, padding: 16, borderWidth: StyleSheet.hairlineWidth },
  hint: { marginTop: 4 },
  input: { marginTop: 14 },
  actions: { flexDirection: "row", justifyContent: "flex-end", marginTop: 14, gap: 10 },
});
