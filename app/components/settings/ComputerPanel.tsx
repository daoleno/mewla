import React from "react";
import { StyleSheet, TextInput, View } from "react-native";
import { Radii, TypeScale, UiTextMetrics, useAppColors } from "../../constants/tokens";
import { InkSwitch } from "../plugins/PluginConnectionViews";
import { Button } from "../ui/Button";
import { ListRow, ListSection } from "../ui/ListSection";
import type { ComputerAction, ComputerProblem } from "./computerModel";

export interface ComputerPanelProps {
  /** Whether this phone can talk to the computer now. */
  reachable: boolean;
  /** The switch; null until the computer has answered. */
  agentsEnabled: boolean | null;
  problems: ComputerProblem[];
  /** What to do about the connection itself, with its one button. */
  connectionProblem?: { text: string; detail?: string | null; label: string; onPress: () => void } | null;
  /** A line after an action, or what went wrong. */
  note?: { key: string; text: string } | null;
  busy: string | null;
  name: string;
  onName: (value: string) => void;
  /** The address, for a computer this phone reaches directly. */
  address?: string | null;
  onAddress?: (value: string) => void;
  onSaveDetails: () => void;
  onToggle: (enabled: boolean) => void;
  onAction: (problem: ComputerProblem, action: ComputerAction) => void;
  /** Switch the app to this computer; absent when it already is in use. */
  onUse?: () => void;
  onRemove: () => void;
}

/**
 * One computer: the switch that lets agents use it, the problems someone can
 * fix (one line, one button), its name, and removing it.
 */
export function ComputerPanel(props: ComputerPanelProps) {
  const colors = useAppColors();
  const disabled = !props.reachable || props.agentsEnabled === null || props.busy !== null;
  const inputStyle = [
    styles.input,
    { backgroundColor: colors.inputBackground, borderColor: colors.borderStrong, color: colors.textPrimary },
  ];

  const action = (label: string, key: string, onPress: () => void, destructive?: boolean) => (
    <Button
      label={label}
      size="sm"
      variant={destructive ? "destructive" : "tinted"}
      loading={props.busy === key}
      disabled={props.busy !== null}
      onPress={onPress}
    />
  );
  const strayNote = props.note && !props.problems.some((problem) => problem.key === props.note!.key) ? props.note : null;

  return (
    <View>
      {props.connectionProblem ? (
        <ListSection>
          <ListRow
            title={props.connectionProblem.text}
            subtitle={props.connectionProblem.detail}
            numberOfLines={3}
            trailing={action(props.connectionProblem.label, "connection", props.connectionProblem.onPress)}
          />
        </ListSection>
      ) : null}

      <ListSection footer={strayNote?.text ?? null}>
        <ListRow
          title="Agents can use this computer"
          disabled={disabled}
          accessibilityRole="switch"
          trailing={
            <InkSwitch
              value={props.agentsEnabled ?? false}
              disabled={disabled}
              accessibilityLabel="Agents can use this computer"
              onValueChange={props.onToggle}
            />
          }
        />
        {props.problems.map((problem) => (
          <ListRow
            key={problem.key}
            title={problem.text}
            subtitle={props.note?.key === problem.key ? props.note.text : null}
            numberOfLines={3}
            trailing={
              problem.action && props.reachable
                ? action(problem.action.label, problem.key, () => props.onAction(problem, problem.action!.does), problem.action.destructive)
                : undefined
            }
          />
        ))}
      </ListSection>

      <ListSection title="Name">
        <View style={styles.field}>
          <TextInput
            style={inputStyle}
            value={props.name}
            onChangeText={props.onName}
            onEndEditing={props.onSaveDetails}
            returnKeyType="done"
            accessibilityLabel="Computer name"
            placeholderTextColor={colors.textSecondary}
            selectionColor={colors.selectionBackground}
            cursorColor={colors.accentStrong}
            autoCorrect={false}
          />
        </View>
      </ListSection>

      {props.address != null && props.onAddress ? (
        <ListSection title="Address">
          <View style={styles.field}>
            <TextInput
              style={[inputStyle, styles.mono]}
              value={props.address}
              onChangeText={props.onAddress}
              onEndEditing={props.onSaveDetails}
                returnKeyType="done"
              accessibilityLabel="Address"
              placeholder="https://mewla.example.com"
              placeholderTextColor={colors.textSecondary}
              selectionColor={colors.selectionBackground}
              cursorColor={colors.accentStrong}
              autoCapitalize="none"
              autoCorrect={false}
              keyboardType="url"
            />
          </View>
        </ListSection>
      ) : null}

      <ListSection>
        {props.onUse ? (
          <ListRow
            title="Use this computer"
            icon="swap-horizontal"
            disabled={props.busy !== null}
            onPress={props.onUse}
          />
        ) : null}
        <ListRow
          title="Remove computer"
          icon="trash"
          destructive
          loading={props.busy === "remove"}
          disabled={props.busy !== null}
          onPress={props.onRemove}
        />
      </ListSection>
    </View>
  );
}

const styles = StyleSheet.create({
  field: {
    padding: 12,
  },
  input: {
    ...UiTextMetrics,
    ...TypeScale.body,
    minHeight: 44,
    borderRadius: Radii.sm,
    paddingHorizontal: 14,
    paddingVertical: 10,
    borderWidth: 1,
  },
  mono: {
    ...TypeScale.mono,
  },
});
