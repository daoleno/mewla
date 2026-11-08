import React from 'react';
import { StyleSheet, View } from 'react-native';
import { Typography } from '../../constants/tokens';
import { Button, AppText, IconButton } from '../ui';
import { MobileSingleLineInput } from '../ui/MobileSingleLineInput';

interface NewTerminalAdvancedFormProps {
  cwd: string;
  command: string;
  name: string;
  canSubmit: boolean;
  submitting: boolean;
  canPickDirectory: boolean;
  onCwdChange(value: string): void;
  onCommandChange(value: string): void;
  onNameChange(value: string): void;
  onPickDirectory(): void;
  onSubmit(): void;
}

export function NewTerminalAdvancedForm({
  cwd,
  command,
  name,
  canSubmit,
  submitting,
  canPickDirectory,
  onCwdChange,
  onCommandChange,
  onNameChange,
  onPickDirectory,
  onSubmit,
}: NewTerminalAdvancedFormProps) {
  return (
    <View style={styles.advancedSection}>
      <AppText variant="label" tone="secondary" style={styles.fieldLabel}>
        Working Directory
      </AppText>
      <View style={styles.inputRow}>
        <MobileSingleLineInput
          framed
          containerStyle={styles.inputFlex}
          inputStyle={styles.mono}
          value={cwd}
          onChangeText={onCwdChange}
          placeholder="Shell default"
          accessibilityLabel="Working directory"
          autoCapitalize="none"
          autoCorrect={false}
          autoComplete="off"
        />
        {canPickDirectory ? (
          <IconButton
            icon="folder-open"
            size={38}
            iconSize={20}
            tone="input"
            style={styles.folderBtn}
            onPress={onPickDirectory}
            accessibilityLabel="Choose working directory"
          />
        ) : null}
      </View>

      <AppText variant="label" tone="secondary" style={styles.fieldLabel}>
        Command
      </AppText>
      <MobileSingleLineInput
        framed
        inputStyle={styles.mono}
        accessibilityLabel="Command"
        value={command}
        onChangeText={onCommandChange}
        placeholder="e.g. claude --model opus"
        autoCapitalize="none"
        autoCorrect={false}
        autoComplete="off"
      />

      <AppText variant="label" tone="secondary" style={styles.fieldLabel}>
        Window Title
      </AppText>
      <MobileSingleLineInput
        framed
        inputStyle={styles.mono}
        accessibilityLabel="Window title"
        value={name}
        onChangeText={onNameChange}
        placeholder="Optional"
        autoCapitalize="none"
        autoCorrect={false}
        autoComplete="off"
      />

      <Button
        label={submitting ? 'Starting...' : 'Launch'}
        variant="filled"
        onPress={onSubmit}
        disabled={!canSubmit}
        block
        style={styles.launchBtn}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  advancedSection: { marginTop: 4, gap: 4 },
  fieldLabel: { marginTop: 8, marginBottom: 4 },
  inputRow: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  inputFlex: { flex: 1 },
  mono: { fontSize: 13, fontFamily: Typography.terminalFont },
  folderBtn: { alignSelf: 'stretch' },
  launchBtn: { marginTop: 10 },
});
