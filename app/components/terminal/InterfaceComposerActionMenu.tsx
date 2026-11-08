import React from "react";
import { ScrollView, StyleSheet, View } from "react-native";
import { ContinuousCorners, Radii, useAppColors } from "../../constants/tokens";
import type { CodexSlashCommand } from "../../services/websocket";
import { EmptyState, ListRow, ListSection } from "../ui";
import { slashCommandEmptyText, slashCommandIcon } from "./codexSlashCommandPresentation";

const COMMAND_GROUPS = [
  { key: "session", label: "Session" },
  { key: "navigation", label: "Navigation" },
  { key: "tools", label: "Project Tools" },
  { key: "settings", label: "Settings" },
  { key: "management", label: "Management" },
] as const;

interface CommandGroup {
  key: string;
  label: string;
  commands: CodexSlashCommand[];
}

interface InterfaceComposerActionMenuProps {
  visible: boolean;
  showComposerActions: boolean;
  showCommandList: boolean;
  commandsUnavailable: boolean;
  canAttach: boolean;
  uploading: boolean;
  commands: CodexSlashCommand[];
  commandQuery: string;
  onUploadPress(): void;
  onSelectCommand(command: CodexSlashCommand): void;
}

/** The slash-command and attach menu. Reads the terminal theme from `TerminalThemeScope`. */
export function InterfaceComposerActionMenu({
  visible,
  showComposerActions,
  showCommandList,
  commandsUnavailable,
  canAttach,
  uploading,
  commands,
  commandQuery,
  onUploadPress,
  onSelectCommand,
}: InterfaceComposerActionMenuProps) {
  const colors = useAppColors();
  if (!visible) {
    return null;
  }

  const commandGroups = groupCommands(commands);

  return (
    <View
      style={[
        styles.menu,
        // Same material as the Composer capsule so menu and dock read as
        // one floating layer.
        { backgroundColor: colors.inputBackground, borderColor: colors.border },
      ]}
    >
      <ScrollView
        style={styles.scroller}
        contentContainerStyle={styles.content}
        keyboardShouldPersistTaps="handled"
        showsVerticalScrollIndicator={commands.length > 6}
      >
        {showComposerActions ? (
          <ListSection>
            <ListRow
              icon={uploading ? "cloud-upload" : "attach"}
              iconColor={canAttach ? colors.accent : colors.textTertiary}
              title="Attach File"
              subtitle={uploading ? "Uploading..." : "Add a file to this message"}
              accessibilityLabel="Upload file"
              accessory="chevron"
              loading={uploading}
              disabled={!canAttach}
              onPress={onUploadPress}
            />
          </ListSection>
        ) : null}

        {showCommandList ? (
          commands.length > 0 ? (
            commandGroups.map((group) => (
              <ListSection key={group.key} title={group.label}>
                {group.commands.map((command) => (
                  <ListRow
                    key={command.value}
                    icon={slashCommandIcon(command.name)}
                    iconColor={colors.accent}
                    title={command.value}
                    subtitle={command.description}
                    accessibilityLabel={`${command.value} ${command.description}`}
                    accessory="check"
                    selected={commandQuery === command.value}
                    accessibilityRole="button"
                    onPress={() => onSelectCommand(command)}
                  />
                ))}
              </ListSection>
            ))
          ) : (
            <EmptyState size="inline" icon="search" detail={slashCommandEmptyText(commandsUnavailable)} />
          )
        ) : null}
      </ScrollView>
    </View>
  );
}

function groupCommands(commands: CodexSlashCommand[]): CommandGroup[] {
  const grouped: CommandGroup[] = COMMAND_GROUPS.map((group) => ({
    ...group,
    commands: commands.filter((command) => command.category === group.key),
  }));
  const knownCategories = new Set(COMMAND_GROUPS.map((group) => group.key));
  const otherCommands = commands.filter(
    (command) => !knownCategories.has(command.category as any),
  );
  if (otherCommands.length > 0) {
    grouped.push({
      key: "other",
      label: "Other",
      commands: otherCommands,
    });
  }
  return grouped.filter((group) => group.commands.length > 0);
}

const styles = StyleSheet.create({
  menu: {
    borderRadius: Radii.card,
    ...ContinuousCorners,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  scroller: { maxHeight: 360 },
  content: { padding: 8 },
});
