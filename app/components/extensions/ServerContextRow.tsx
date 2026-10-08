import React from "react";
import { StyleSheet, View } from "react-native";
import { useAppTheme } from "../../constants/tokens";
import { AppText } from "../ui/AppText";
import { StatusPill, type StatusTone } from "../ui/StatusPill";
import { Icon } from "../icons/Icon";

export type ServerConnection = "offline" | "connecting" | "connected";

/**
 * Names the canonical current server a management page reads from and, when
 * the inventory is project-aware, the project it was read for. An empty
 * project means only global locations were read.
 */
export function ServerContextRow({
  name,
  connection,
  project,
}: {
  name: string;
  connection: ServerConnection;
  /** Project directory; omit on pages without a project scope. */
  project?: string;
}) {
  const { colors } = useAppTheme();
  const tone: StatusTone = connection === "connected" ? "success" : connection === "connecting" ? "accent" : "warning";
  const label = connection === "connected" ? "Connected" : connection === "connecting" ? "Connecting" : "Offline";
  const projectName = project === undefined ? undefined : project ? basename(project) : "Global only";
  return (
    <View
      style={styles.row}
      accessible
      accessibilityLabel={[`Current server ${name}`, label, projectName === undefined ? null : project ? `Project ${projectName}` : "No project, global Skills only"].filter(Boolean).join(", ")}
    >
      <Icon name="server" size={14} color={colors.textTertiary} />
      <AppText variant="label" tone="secondary" numberOfLines={1} style={styles.shrink}>
        {name}
      </AppText>
      {projectName !== undefined ? (
        <>
          <Icon name={project ? "folder" : "browser"} size={14} color={colors.textTertiary} />
          <AppText variant="label" tone="tertiary" numberOfLines={1} style={styles.shrink}>
            {projectName}
          </AppText>
        </>
      ) : null}
      <View style={styles.spacer} />
      <StatusPill label={label} tone={tone} live={connection === "connecting"} />
    </View>
  );
}

function basename(path: string): string {
  const parts = path.replace(/\/+$/, "").split("/");
  return parts[parts.length - 1] || path;
}

const styles = StyleSheet.create({
  row: { flexDirection: "row", alignItems: "center", gap: 6, minHeight: 32 },
  shrink: { flexShrink: 1 },
  spacer: { flex: 1, minWidth: 4 },
});
