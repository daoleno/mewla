import React, { useState } from "react";
import { ScrollView, StyleSheet, View } from "react-native";
import { useAppColors } from "../../constants/tokens";
import type { AgentUseStatus } from "../../services/agentUse";
import { ComputerPanel } from "../settings/ComputerPanel";
import { ComputersSection } from "../settings/ComputersSection";
import { computerProblems } from "../settings/computerModel";
import { AppText } from "../ui/AppText";
import { ListRow, ListSection } from "../ui/ListSection";

const NOOP = () => undefined;

const MAC: AgentUseStatus = {
  enabled: true,
  browser: { state: "no_extension", summary: "", fix: "install_extension" },
  computer: { state: "ready", summary: "" },
};

const LINUX: AgentUseStatus = {
  enabled: true,
  browser: { state: "no_desktop", summary: "" },
  computer: { state: "unsupported", summary: "" },
};

/** Settings → Computers with two computers: a Mac and a headless Linux server. */
export function ComputersScreenshotDemo({ fixture, header }: { fixture?: string; header: (title: string) => React.ReactNode }) {
  const colors = useAppColors();
  const page = fixture === "mac" || fixture === "linux" ? fixture : "list";
  const [name, setName] = useState(page === "mac" ? "daoleno's MacBook Pro" : "manjaro");
  return (
    <ScrollView style={{ flex: 1, backgroundColor: colors.bgPrimary }} contentContainerStyle={styles.content}>
      {header(page === "list" ? "Settings" : name)}
      <View style={styles.inner}>
        {page === "list" ? (
          <>
            <Header>Computers</Header>
            <ComputersSection
              computers={[
                { id: "mac", name: "daoleno's MacBook Pro", status: "Online" },
                { id: "linux", name: "manjaro", status: "In use" },
              ]}
              onOpen={NOOP}
              onPair={NOOP}
            />
            <Header>Channels</Header>
            <ListSection>
              <ListRow title="Telegram" subtitle="Talk to Brain from Telegram" icon="paper-plane" accessory="chevron" />
            </ListSection>
            <Header>Agents</Header>
            <ListSection>
              <ListRow title="Model Providers" subtitle="Official login or your own API keys" icon="key" accessory="chevron" />
            </ListSection>
          </>
        ) : (
          <ComputerPanel
            reachable
            agentsEnabled
            problems={computerProblems(page === "mac" ? MAC : LINUX)}
            busy={null}
            name={name}
            onName={setName}
            address={page === "linux" ? "https://zen.daoleno.com" : null}
            onAddress={NOOP}
            onSaveDetails={NOOP}
            onToggle={NOOP}
            onAction={NOOP}
            onUse={page === "mac" ? NOOP : undefined}
            onRemove={NOOP}
          />
        )}
      </View>
    </ScrollView>
  );
}

function Header({ children }: { children: string }) {
  return (
    <AppText variant="label" tone="tertiary" accessibilityRole="header" style={styles.header}>
      {children}
    </AppText>
  );
}

const styles = StyleSheet.create({
  content: { width: "100%", paddingHorizontal: 16, paddingTop: 20 },
  inner: { width: "100%", maxWidth: 760, alignSelf: "center" },
  header: { paddingHorizontal: 16, paddingTop: 4, paddingBottom: 6 },
});
