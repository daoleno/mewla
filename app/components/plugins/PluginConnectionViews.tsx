import React, { useState } from "react";
import { StyleSheet, Switch, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { ContinuousCorners, Radii, useAppTheme } from "../../constants/tokens";
import { AppText, Button, EmptyState, InlineNotice, ListRow, ListSection, StatusPill } from "../ui";
import type { StatusTone } from "../ui/StatusPill";
import { accountStatus, type PluginAccount, type PluginIntegration } from "../../services/connections";
import type { ConnectPhase, PendingConnection } from "../../services/pluginOnboarding";
import {
  accountRecovery,
  accountTone,
  allowedToolCount,
  capabilityLabel,
  capabilityState,
  connectSteps,
  type AccountRecoveryAction,
  type PluginCatalogSections,
} from "../../services/pluginConnectionsModel";

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];
type Job = { read: string; write: string; example: string } | undefined;

const SERVICE_ICONS: Record<string, IoniconName> = {
  github: "logo-github",
  slack: "logo-slack",
  google: "logo-google",
  notion: "document-text-outline",
  linear: "git-branch-outline",
  mcp: "server-outline",
  openapi: "code-slash-outline",
};

export function ServiceGlyph({ id, size = 30 }: { id: string; size?: number }) {
  const { colors, theme } = useAppTheme();
  return (
    <View
      accessible={false}
      importantForAccessibility="no-hide-descendants"
      style={[styles.glyph, { width: size, height: size, borderRadius: size * 0.3, backgroundColor: theme.materials.tint }]}
    >
      <Ionicons name={SERVICE_ICONS[id] ?? "extension-puzzle-outline"} size={Math.round(size * 0.6)} color={colors.accentStrong} />
    </View>
  );
}

export function NoServerState({ onOpenSettings }: { onOpenSettings(): void }) {
  return (
    <EmptyState
      icon="server-outline"
      title="No current server"
      detail="Choose one in Settings."
      action={{ label: "Open Settings", onPress: onOpenSettings }}
    />
  );
}

export function CatalogView({
  sections,
  loading,
  error,
  onRetry,
  onOpenAccount,
  onOpenService,
  onOpenCustom,
}: {
  sections: PluginCatalogSections;
  loading: boolean;
  error: string;
  onRetry(): void;
  onOpenAccount(plugin: PluginIntegration, account: PluginAccount): void;
  onOpenService(plugin: PluginIntegration): void;
  onOpenCustom(): void;
}) {
  if (!sections.services.length && !sections.custom.length) {
    return error ? (
      <EmptyState icon="cloud-offline-outline" tone="danger" title="Services unavailable" detail={error} action={{ label: "Try again", icon: "refresh-outline", onPress: onRetry, loading }} />
    ) : loading ? (
      <EmptyState busy title="Loading services" />
    ) : (
      <EmptyState icon="extension-puzzle-outline" title="No services" detail="This server has none to connect." action={{ label: "Try again", icon: "refresh-outline", onPress: onRetry }} />
    );
  }
  return (
    <>
      {sections.connected.length ? (
        <ListSection title="Connected">
          {sections.connected.map(({ account, plugin }) => (
            <ListRow
              key={account.id}
              leading={<ServiceGlyph id={plugin.id} />}
              title={account.name}
              subtitle={plugin.name}
              trailing={<StatusPill label={accountStatus(account)} tone={accountTone(account)} />}
              accessory="chevron"
              accessibilityLabel={`${account.name}, ${plugin.name}, ${accountStatus(account)}`}
              disabled={loading}
              onPress={() => onOpenAccount(plugin, account)}
            />
          ))}
        </ListSection>
      ) : null}
      <ListSection title={sections.connected.length ? "Add a service" : "Services"}>
        {sections.services.map(({ plugin, accountCount }) => (
          <ListRow
            key={plugin.id}
            leading={<ServiceGlyph id={plugin.id} />}
            title={plugin.name}
            subtitle={plugin.available ? plugin.description : plugin.unavailable_reason || "Sign-in is not set up yet."}
            value={plugin.available && accountCount ? `${accountCount} connected` : undefined}
            trailing={!plugin.available ? <StatusPill label="Not yet available" tone="neutral" /> : undefined}
            accessory={plugin.available ? "chevron" : undefined}
            numberOfLines={2}
            disabled={loading || !plugin.available}
            accessibilityLabel={plugin.available ? plugin.name : `${plugin.name}, Not yet available. ${plugin.unavailable_reason || "Sign-in is not set up yet."}`}
            onPress={() => onOpenService(plugin)}
          />
        ))}
      </ListSection>
      {sections.custom.length ? (
        <ListSection>
          <ListRow
            icon="construct-outline"
            title="Custom services"
            subtitle={sections.custom.map((entry) => entry.plugin.name).join(" · ")}
            accessory="chevron"
            onPress={onOpenCustom}
          />
        </ListSection>
      ) : null}
    </>
  );
}

export function CustomCatalogView({ sections, onOpenService }: { sections: PluginCatalogSections; onOpenService(plugin: PluginIntegration): void }) {
  return (
    <ListSection>
      {sections.custom.map(({ plugin, accountCount }) => (
        <ListRow
          key={plugin.id}
          leading={<ServiceGlyph id={plugin.id} />}
          title={plugin.name}
          subtitle={plugin.description}
          value={accountCount ? `${accountCount} connected` : undefined}
          accessory="chevron"
          onPress={() => onOpenService(plugin)}
        />
      ))}
    </ListSection>
  );
}

/** Service identity and its single most important state. */
export function ServiceHeader({ plugin, account }: { plugin: PluginIntegration; account: PluginAccount | null }) {
  return (
    <View style={styles.serviceHeader}>
      <ServiceGlyph id={plugin.id} size={52} />
      <View style={styles.serviceCopy}>
        <View style={styles.serviceTitleRow}>
          <AppText variant="title" accessibilityRole="header" numberOfLines={1} style={styles.flexShrink}>
            {plugin.name}
          </AppText>
          {account ? <StatusPill label={accountStatus(account)} tone={accountTone(account)} /> : null}
        </View>
        {plugin.description ? (
          <AppText variant="compact" tone="secondary" numberOfLines={3}>
            {plugin.description}
          </AppText>
        ) : null}
      </View>
    </View>
  );
}

export function ConnectProgressCard({
  serviceName,
  phase,
  flow,
  onOpen,
  onCancel,
}: {
  serviceName: string;
  phase: ConnectPhase;
  flow: PendingConnection | null;
  onOpen(): void;
  onCancel(): void;
}) {
  const { colors } = useAppTheme();
  const steps = connectSteps(phase, flow);
  const code = flow?.flow.user_code;
  return (
    <ListSection
      title={phase === "verifying" ? "Verifying your account" : `Connecting ${serviceName}`}
      footer="You can leave and come back."
    >
      {steps.map((step) => (
        <ListRow
          key={step.key}
          leading={<StepGlyph state={step.state} />}
          title={step.label}
          accessibilityLabel={`${step.label}, ${step.state === "done" ? "done" : step.state === "active" ? "in progress" : step.state === "failed" ? "failed" : "not started"}`}
        />
      ))}
      {code ? (
        <View style={styles.codeBlock} accessible accessibilityLabel={`Code ${code.split("").join(" ")}`}>
          <AppText variant="caption" tone="tertiary">
            Code
          </AppText>
          <AppText variant="monoStrong" selectable style={[styles.code, { color: colors.textPrimary }]}>
            {code}
          </AppText>
        </View>
      ) : null}
      <View style={styles.cardActions}>
        {phase === "waiting" && flow ? (
          <Button
            label={code ? `Copy code and open ${serviceName}` : "Open authorization"}
            icon="open-outline"
            variant="filled"
            block
            onPress={onOpen}
          />
        ) : null}
        <Button label="Cancel connection" variant="plain" block onPress={onCancel} />
      </View>
    </ListSection>
  );
}

function StepGlyph({ state }: { state: "done" | "active" | "pending" | "failed" }) {
  const { colors } = useAppTheme();
  const glyph: { name: IoniconName; color: string } =
    state === "done"
      ? { name: "checkmark-circle", color: colors.success }
      : state === "active"
        ? { name: "ellipse", color: colors.accentStrong }
        : state === "failed"
          ? { name: "close-circle", color: colors.dangerText }
          : { name: "ellipse-outline", color: colors.textTertiary };
  return (
    <View style={styles.stepGlyph}>
      <Ionicons name={glyph.name} size={state === "active" ? 12 : 20} color={glyph.color} />
    </View>
  );
}

export function LinkedAccountView({
  plugin,
  account,
  job,
  custom,
  busy,
  accountCount,
  onRecover,
  onOpenPermissions,
  onOpenAccounts,
  onOpenTools,
}: {
  plugin: PluginIntegration;
  account: PluginAccount;
  job: Job;
  custom: boolean;
  busy: boolean;
  accountCount: number;
  onRecover(action: AccountRecoveryAction): void;
  onOpenPermissions(): void;
  onOpenAccounts(): void;
  onOpenTools(): void;
}) {
  const recovery = accountRecovery(account, plugin.name);
  const read = capabilityState(account, "read");
  const write = capabilityState(account, "write");
  const tools = allowedToolCount(account);
  const nothingAllowed = tools.allowed === 0;
  return (
    <>
      {recovery ? (
        <InlineNotice
          tone={recovery.tone}
          title={recovery.title}
          detail={recovery.detail}
          busy={busy && recovery.action !== "reconnect"}
          action={{ label: recovery.actionLabel, onPress: () => onRecover(recovery.action), disabled: busy }}
          style={styles.notice}
        />
      ) : null}
      <ListSection title="Account">
        <ListRow
          icon="person-circle-outline"
          title={account.name}
          subtitle={account.identity === account.name ? undefined : account.identity || undefined}
          numberOfLines={2}
        />
      </ListSection>
      <ListSection title="What Brain can do">
        {custom ? (
          <ListRow
            icon="key-outline"
            title={`${tools.allowed} of ${tools.total} tools allowed`}
            subtitle={nothingAllowed ? "Choose tools in Tools & activity." : undefined}
            accessory="chevron"
            onPress={onOpenTools}
          />
        ) : (
          <>
            <ListRow
              icon="eye-outline"
              title="Read and search"
              subtitle={job?.read}
              value={capabilityLabel(read)}
              numberOfLines={1}
              accessory="chevron"
              accessibilityLabel={`Read and search, ${capabilityLabel(read)}`}
              onPress={onOpenPermissions}
            />
            <ListRow
              icon="create-outline"
              title="Make changes when asked"
              subtitle={job?.write}
              value={capabilityLabel(write)}
              numberOfLines={1}
              accessory="chevron"
              accessibilityLabel={`Make changes when asked, ${capabilityLabel(write)}`}
              onPress={onOpenPermissions}
            />
          </>
        )}
      </ListSection>
      <ListSection>
        <ListRow icon="people-outline" title="Connected accounts" value={String(accountCount)} accessory="chevron" onPress={onOpenAccounts} />
        <ListRow icon="list-outline" title="Tools & activity" value={tools.total ? `${tools.allowed}/${tools.total}` : undefined} accessory="chevron" onPress={onOpenTools} />
      </ListSection>
    </>
  );
}

/** Capability consent shown before the official authorization screen. */
export function ConnectOfferView({
  plugin,
  job,
  writes,
  busy,
  serverName,
  retry,
  confirmIdentity,
  onWritesChange,
  onConnect,
  onUseServerAccount,
  onImport,
  onChooseAnother,
  onRetryVerification,
}: {
  plugin: PluginIntegration;
  job: Job;
  writes: boolean;
  busy: boolean;
  serverName: string;
  retry: boolean;
  /** Identity found on the server, awaiting explicit import. */
  confirmIdentity: string | null;
  onWritesChange(value: boolean): void;
  onConnect(): void;
  onUseServerAccount?(): void;
  onImport(): void;
  onChooseAnother(): void;
  onRetryVerification?(): void;
}) {
  if (!plugin.available) {
    return <InlineNotice title="Not yet available" detail={plugin.unavailable_reason || "Sign-in is not set up yet."} />;
  }
  return (
    <>
      <ListSection title="Access you're granting" footer={`Credentials stay on ${serverName}.`}>
        <ListRow icon="eye-outline" title="Read and search" subtitle={job?.read} numberOfLines={3} value="Included" />
        <ListRow
          icon="create-outline"
          title="Make changes when asked"
          subtitle={job?.write}
          numberOfLines={3}
          trailing={<Switch accessibilityLabel="Allow changes when asked" value={writes} onValueChange={onWritesChange} disabled={busy} />}
        />
      </ListSection>
      {confirmIdentity !== null ? (
        <ListSection title="Use the account signed in on this server?" footer="Zen keeps its own copy. Later sign-ins on the server don't change it.">
          <ListRow icon="person-circle-outline" title={confirmIdentity || `${plugin.name} account`} subtitle={`${plugin.name} · identity verified`} />
          <View style={styles.cardActions}>
            <Button label={`Connect ${confirmIdentity || "this account"}`} variant="filled" block loading={busy} onPress={onImport} />
            <Button label="Choose another account" variant="plain" block onPress={onChooseAnother} />
          </View>
        </ListSection>
      ) : (
        <View style={styles.actions}>
          <Button label={`${retry ? "Try again with" : "Connect"} ${plugin.name}`} variant="filled" size="lg" block loading={busy} onPress={onConnect} />
          {onUseServerAccount ? <Button label="Use account signed in on server" variant="plain" block disabled={busy} onPress={onUseServerAccount} /> : null}
        </View>
      )}
      {onRetryVerification ? <Button label="Retry verification" icon="refresh-outline" block onPress={onRetryVerification} /> : null}
    </>
  );
}

export function AccountsView({
  plugin,
  accounts,
  selectedId,
  busy,
  onOpen,
  onConnectAnother,
  onDisconnect,
}: {
  plugin: PluginIntegration;
  accounts: PluginAccount[];
  selectedId: string | null;
  busy: boolean;
  onOpen(account: PluginAccount): void;
  onConnectAnother(): void;
  onDisconnect(account: PluginAccount): void;
}) {
  const selected = accounts.find((item) => item.id === selectedId) ?? null;
  return (
    <>
      <ListSection title={`${plugin.name} accounts`}>
        {accounts.map((item) => (
          <ListRow
            key={item.id}
            leading={<ServiceGlyph id={plugin.id} />}
            title={item.name}
            subtitle={item.identity === item.name ? undefined : item.identity}
            trailing={<StatusPill label={accountStatus(item)} tone={accountTone(item)} />}
            accessory="check"
            selected={item.id === selectedId}
            accessibilityRole="button"
            accessibilityLabel={`${item.name}, ${accountStatus(item)}${item.id === selectedId ? ", selected" : ""}`}
            disabled={busy}
            onPress={() => onOpen(item)}
          />
        ))}
      </ListSection>
      <ListSection>
        <ListRow icon="add" title="Connect another account" accessory="chevron" onPress={onConnectAnother} />
      </ListSection>
      {selected ? (
        <ListSection>
          <ListRow
            icon={selected.credential_removal_pending ? "refresh-outline" : "remove-circle-outline"}
            title={selected.credential_removal_pending ? "Retry credential removal" : `Disconnect ${selected.name}`}
            destructive
            disabled={busy}
            onPress={() => onDisconnect(selected)}
          />
        </ListSection>
      ) : null}
    </>
  );
}

export function PermissionsView({
  account,
  job,
  busy,
  onChange,
  onReconnect,
}: {
  account: PluginAccount;
  job: Job;
  busy: boolean;
  onChange(group: "read" | "write", allowed: boolean): void;
  onReconnect(): void;
}) {
  return (
    <>
      <ListSection title={account.name}>
        {(["read", "write"] as const).map((group) => {
          const state = capabilityState(account, group);
          const label = group === "read" ? "Read and search" : "Make changes when asked";
          return (
            <ListRow
              key={group}
              icon={group === "read" ? "eye-outline" : "create-outline"}
              title={label}
              subtitle={state === "unavailable" ? "Not granted. Reconnect to request it." : group === "read" ? job?.read : job?.write}
              numberOfLines={3}
              trailing={<Switch accessibilityLabel={label} disabled={busy || state === "unavailable"} value={state === "allowed"} onValueChange={(allowed) => onChange(group, allowed)} />}
            />
          );
        })}
      </ListSection>
      <Button label="Reconnect to change access" variant="plain" block onPress={onReconnect} />
    </>
  );
}

export function ToolsView({
  account,
  busy,
  onToggleEnabled,
  onRefresh,
  onToggleTool,
}: {
  account: PluginAccount;
  busy: boolean;
  onToggleEnabled(enabled: boolean): void;
  onRefresh(): void;
  onToggleTool(tool: NonNullable<PluginAccount["tools"]>[number], allowed: boolean): void;
}) {
  const tools = account.tools ?? [];
  return (
    <>
      <ListSection title="Account controls">
        <ListRow icon="power-outline" title="Enabled" trailing={<Switch accessibilityLabel="Enable account" value={account.enabled} disabled={busy} onValueChange={onToggleEnabled} />} />
        <ListRow
          icon="pulse-outline"
          title="Check service and tools"
          subtitle={account.verified_at ? `Last verified ${new Date(account.verified_at).toLocaleString()}` : undefined}
          loading={busy}
          onPress={onRefresh}
        />
      </ListSection>
      <ListSection title={`Tools (${tools.length})`} footer="A tool that changes needs permission again.">
        {tools.length ? (
          tools.map((tool) => <ToolRow key={tool.name} tool={tool} busy={busy} onToggle={(allowed) => onToggleTool(tool, allowed)} />)
        ) : (
          <ListRow title="No tools reported" />
        )}
      </ListSection>
      <ListSection title="Recent calls">
        {account.history.length ? (
          account.history.map((event, index) => (
            <ListRow
              key={`${event.at}-${index}`}
              title={event.tool}
              subtitle={event.message || new Date(event.at).toLocaleString()}
              trailing={<StatusPill label={event.status} tone={callTone(event.status)} />}
              numberOfLines={2}
            />
          ))
        ) : (
          <ListRow title="No calls yet" />
        )}
      </ListSection>
    </>
  );
}

function callTone(status: string): StatusTone {
  const value = status.toLowerCase();
  if (value === "ok" || value === "success" || value === "succeeded") return "success";
  if (value.includes("fail") || value.includes("error") || value.includes("denied")) return "danger";
  return "neutral";
}

function ToolRow({ tool, busy, onToggle }: { tool: NonNullable<PluginAccount["tools"]>[number]; busy: boolean; onToggle(allowed: boolean): void }) {
  const { colors } = useAppTheme();
  const [expanded, setExpanded] = useState(false);
  return (
    <View>
      <ListRow
        title={tool.name}
        subtitle={tool.description}
        numberOfLines={2}
        trailing={<Switch accessibilityLabel={`Allow ${tool.name}`} value={tool.allowed} disabled={busy} onValueChange={onToggle} />}
      />
      <View style={styles.schema}>
        <Button
          label={expanded ? "Hide inputs" : "Inspect inputs"}
          accessibilityLabel={`${expanded ? "Hide" : "Inspect"} inputs for ${tool.name}`}
          accessibilityState={{ expanded }}
          size="sm"
          variant="plain"
          icon={expanded ? "chevron-up" : "chevron-down"}
          onPress={() => setExpanded(!expanded)}
        />
        {expanded ? (
          <View style={[styles.schemaBody, { backgroundColor: colors.surfaceSubtle }]}>
            <AppText variant="mono" selectable>
              {JSON.stringify(tool.input_schema ?? {}, null, 2)}
            </AppText>
          </View>
        ) : null}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  glyph: { alignItems: "center", justifyContent: "center", ...ContinuousCorners },
  serviceHeader: { flexDirection: "row", alignItems: "center", gap: 14, paddingHorizontal: 4 },
  serviceCopy: { flex: 1, minWidth: 0, gap: 4 },
  serviceTitleRow: { flexDirection: "row", alignItems: "center", gap: 10, flexWrap: "wrap" },
  flexShrink: { flexShrink: 1 },
  stepGlyph: { width: 30, height: 30, alignItems: "center", justifyContent: "center" },
  codeBlock: { paddingHorizontal: 16, paddingVertical: 12, gap: 4 },
  code: { fontSize: 26, lineHeight: 34, letterSpacing: 2 },
  cardActions: { paddingHorizontal: 16, paddingVertical: 12, gap: 4 },
  actions: { gap: 6, marginBottom: 26 },
  notice: { marginBottom: 20 },
  schema: { paddingHorizontal: 8, paddingBottom: 8 },
  schemaBody: { marginHorizontal: 8, padding: 12, borderRadius: Radii.xs, ...ContinuousCorners },
});
